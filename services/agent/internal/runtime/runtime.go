package runtime

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"shortlink.local/agent/internal/eventqueue"
	"shortlink.local/geoip"
	"shortlink.local/policy"
	"shortlink.local/risk"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxSnapshotBytes = 8 << 20

var slugPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

type Link struct {
	ID          string `json:"id"`
	DomainID    string `json:"domainId"`
	Slug        string `json:"slug"`
	Destination string `json:"destination"`
	State       string `json:"state"`
}
type Snapshot struct {
	Revision       int64     `json:"revision"`
	LeaseExpiresAt time.Time `json:"leaseExpiresAt"`
	Domain         struct {
		ID       string `json:"id"`
		Hostname string `json:"hostname"`
		Mode     string `json:"mode"`
	} `json:"domain"`
	Links       []Link              `json:"links"`
	Rules       []policy.ScopedRule `json:"rules"`
	GeoRelease  *string             `json:"geoRelease"`
	FeedVersion *string             `json:"feedVersion"`
	Protection  policy.Protection   `json:"protection"`
}
type Envelope struct {
	SignedPayload string `json:"signedPayload"`
	Signature     string `json:"signature"`
}
type active struct {
	snapshot Snapshot
	routes   map[string]string
	linkIDs  map[string]string
	engine   *policy.Engine
	payload  []byte
	envelope Envelope
	etag     string
	limiter  *risk.Limiter
}
type Agent struct {
	cfg       Config
	key       ed25519.PublicKey
	proxies   []netip.Prefix
	client    *http.Client
	current   atomic.Pointer[active]
	syncMu    sync.Mutex
	blocked   atomic.Bool
	cachePath string
	geo       *geoip.Manager
	queue     *eventqueue.Queue
	feeds     *risk.Manager
}

func New(c Config) (*Agent, error) {
	if err := policy.Init(); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	key, _ := base64.RawURLEncoding.DecodeString(c.VerificationKey)
	a := &Agent{cfg: c, key: key, cachePath: filepath.Join(c.DataDir, "snapshot.json"), client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	for _, raw := range c.TrustedProxies {
		p, _ := netip.ParsePrefix(raw)
		a.proxies = append(a.proxies, p)
	}
	if err := os.MkdirAll(c.DataDir, 0700); err != nil {
		return nil, err
	}
	if info, err := os.Stat(c.DataDir); err != nil || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("data directory must be private (0700)")
	}
	var geoErr error
	a.geo, geoErr = geoip.NewManager(filepath.Join(c.DataDir, "geoip"), key, c.ControlURL, c.Credential)
	if geoErr != nil {
		return nil, fmt.Errorf("cached GeoLite rejected: %w", geoErr)
	}
	a.feeds, geoErr = risk.NewManager(filepath.Join(c.DataDir, "risk"), key, c.ControlURL, c.Credential)
	if geoErr != nil {
		return nil, fmt.Errorf("cached risk feed rejected: %w", geoErr)
	}
	// A revoked credential remains blocked across restart until operator provisions a replacement.
	if _, err := os.Stat(filepath.Join(c.DataDir, "revoked")); err == nil {
		a.blocked.Store(true)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	raw, err := os.ReadFile(a.cachePath)
	if os.IsNotExist(err) {
		return a.openQueue()
	}
	if err != nil {
		return nil, err
	}
	next, err := a.verify(raw, time.Now(), false)
	if err != nil {
		return nil, fmt.Errorf("cached snapshot rejected: %w", err)
	}
	// Expired cache preserves anti-rollback revision but never enables routing.
	a.current.Store(next)
	return a.openQueue()
}
func (a *Agent) verify(raw []byte, now time.Time, requireFresh bool) (*active, error) {
	if len(raw) > maxSnapshotBytes {
		return nil, errors.New("snapshot exceeds limit")
	}
	var e Envelope
	if json.Unmarshal(raw, &e) != nil {
		return nil, errors.New("invalid envelope")
	}
	payload, err := base64.RawURLEncoding.DecodeString(e.SignedPayload)
	if err != nil {
		return nil, errors.New("invalid signed payload")
	}
	sig, err := base64.RawURLEncoding.DecodeString(e.Signature)
	if err != nil || !ed25519.Verify(a.key, payload, sig) {
		return nil, errors.New("invalid signature")
	}
	var s Snapshot
	if json.Unmarshal(payload, &s) != nil || s.Revision < 1 || s.LeaseExpiresAt.IsZero() || (requireFresh && !now.Before(s.LeaseExpiresAt)) {
		return nil, errors.New("invalid revision or expired lease")
	}
	if s.Domain.ID != a.cfg.DomainID || s.Domain.Hostname != a.cfg.Hostname || s.Domain.Mode != "local" {
		return nil, errors.New("snapshot binding mismatch")
	}
	s.Protection = policy.NormalizeProtection(s.Protection)
	if err := policy.ValidateProtection(s.Protection); err != nil {
		return nil, err
	}
	// Validate feed versions and signed policy before activation.
	if (s.GeoRelease != nil && !geoip.ValidRelease(*s.GeoRelease)) || (s.FeedVersion != nil && !risk.ValidVersion(*s.FeedVersion)) {
		return nil, errors.New("snapshot requires unsupported capabilities")
	}
	routes := make(map[string]string, len(s.Links))
	linkIDs := map[string]string{}
	ids := map[string]bool{}

	for _, l := range s.Links {
		if l.ID == "" || ids[l.ID] || l.DomainID != a.cfg.DomainID || l.State != "published" || !slugPattern.MatchString(l.Slug) {
			return nil, errors.New("invalid published link")
		}
		u, err := url.Parse(l.Destination)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(l.Destination, "\r\n") || normalizeHost(u.Hostname()) == a.cfg.Hostname {
			return nil, errors.New("invalid or looping destination")
		}
		if _, exists := routes[l.Slug]; exists {
			return nil, errors.New("duplicate slug")
		}
		routes[l.Slug] = l.Destination
		linkIDs[l.Slug] = l.ID
		ids[l.ID] = true
	}
	engine, err := policy.Compile(s.Rules, s.Domain.ID, s.Domain.Hostname, ids)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(payload)
	return &active{snapshot: s, routes: routes, linkIDs: linkIDs, engine: engine, payload: payload, envelope: e, etag: fmt.Sprintf(`"%x"`, h), limiter: risk.NewLimiter(s.Protection.RatePerSecond, s.Protection.Burst, 16384)}, nil
}
func atomicFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (a *Agent) request(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.cfg.ControlURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Credential)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if path == "/v1/agents/config" {
		if old := a.current.Load(); old != nil {
			req.Header.Set("If-None-Match", old.etag)
		}
	}
	return a.client.Do(req)
}
func (a *Agent) rejectCredential() error {
	a.blocked.Store(true)
	if err := atomicFile(filepath.Join(a.cfg.DataDir, "revoked"), []byte("credential rejected\n")); err != nil {
		return fmt.Errorf("credential rejected; revoke marker persistence failed: %w", err)
	}
	return errors.New("credential rejected; reprovision agent with a new private data directory")
}
func (a *Agent) Sync(ctx context.Context) error {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	if a.blocked.Load() {
		return errors.New("credential blocked")
	}
	resp, err := a.request(ctx, http.MethodGet, "/v1/agents/config", nil)
	if err != nil {
		return errors.New("control config unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return a.rejectCredential()
	}
	if resp.StatusCode == 304 {
		old := a.current.Load()
		if old == nil || !time.Now().Before(old.snapshot.LeaseExpiresAt) {
			return errors.New("304 does not renew an expired lease")
		}
		return a.heartbeat(ctx, old.snapshot.Revision)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("control config HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSnapshotBytes+1))
	if err != nil {
		return errors.New("snapshot read failed")
	}
	next, err := a.verify(raw, time.Now(), true)
	if err != nil {
		return err
	}
	if old := a.current.Load(); old != nil {
		if next.snapshot.Revision < old.snapshot.Revision {
			return errors.New("snapshot rollback rejected")
		}
		if next.snapshot.Revision == old.snapshot.Revision {
			if next.snapshot.LeaseExpiresAt.Before(old.snapshot.LeaseExpiresAt) {
				return errors.New("lease rollback rejected")
			}
			// A same-revision renewal may change only lease expiry, not routing configuration.
			prev, newer := old.snapshot, next.snapshot
			prev.LeaseExpiresAt = time.Time{}
			newer.LeaseExpiresAt = time.Time{}
			x, _ := json.Marshal(prev)
			y, _ := json.Marshal(newer)
			if !bytes.Equal(x, y) {
				return errors.New("same revision changed configuration")
			}
		}
	}
	persisted, _ := json.Marshal(next.envelope)
	if err := atomicFile(a.cachePath, persisted); err != nil {
		return fmt.Errorf("snapshot persistence failed: %w", err)
	}
	if old := a.current.Load(); old != nil && old.snapshot.Protection == next.snapshot.Protection {
		next.limiter = old.limiter
	}
	a.current.Store(next)
	return a.heartbeat(ctx, next.snapshot.Revision)
}
func (a *Agent) heartbeat(ctx context.Context, revision int64) error {
	stats, _ := a.queue.Stats(ctx)
	body, _ := json.Marshal(map[string]any{"version": Version, "appliedRevision": revision, "geoRelease": a.geo.ActiveRelease(), "feedVersion": a.feeds.Version(), "protectionVersion": 1, "queueDepth": stats.Depth, "queueBytes": stats.Bytes, "droppedEvents": stats.Dropped, "bufferedEvents": stats.Buffered, "queueWriteErrors": stats.WriteErrors})
	resp, err := a.request(ctx, http.MethodPost, "/v1/agents/heartbeat", bytes.NewReader(body))
	if err != nil {
		return errors.New("heartbeat unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return a.rejectCredential()
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("heartbeat HTTP %d", resp.StatusCode)
	}
	return nil
}
func (a *Agent) Run(ctx context.Context) {
	for {
		if err := a.Sync(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("agent sync", "error", err.Error())
		}
		var jitter [1]byte
		_, _ = rand.Read(jitter[:])
		delay := 15*time.Second + time.Duration(jitter[0])*time.Second/17
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (a *Agent) trusted(ip netip.Addr) bool {
	for _, p := range a.proxies {
		if p.Contains(ip.Unmap()) {
			return true
		}
	}
	return false
}

// Only socket-peer trust permits X-Forwarded-For. Walk right to left until first untrusted hop.
func (a *Agent) ClientIP(r *http.Request) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, errors.New("invalid peer IP")
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, errors.New("invalid peer IP")
	}
	peer = peer.Unmap()
	if !a.trusted(peer) {
		return peer, nil
	}
	values := r.Header.Values("X-Forwarded-For")
	if len(values) == 0 {
		return peer, nil
	}
	raw := strings.Join(values, ",")
	if strings.TrimSpace(raw) == "" {
		return peer, nil
	}
	if len(raw) > 4096 {
		return netip.Addr{}, errors.New("forwarded chain too large")
	}
	hops := strings.Split(raw, ",")
	if len(hops) > 32 {
		return netip.Addr{}, errors.New("too many proxy hops")
	}
	parsed := make([]netip.Addr, len(hops))
	for i, h := range hops {
		parsed[i], err = netip.ParseAddr(strings.TrimSpace(h))
		if err != nil {
			return netip.Addr{}, errors.New("invalid forwarded IP")
		}
		parsed[i] = parsed[i].Unmap()
	}
	for i := len(parsed) - 1; i >= 0 && a.trusted(peer); i-- {
		peer = parsed[i]
	}
	return peer, nil
}
func (a *Agent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Path == "/.well-known/shortlink-ready" {
		a.readinessProbe(w, r)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", 405)
		return
	}
	if r.URL.Path == "/health/live" {
		w.WriteHeader(200)
		return
	}
	if r.URL.Path == "/health/ready" {
		state := a.current.Load()
		if a.blocked.Load() || state == nil || !time.Now().Before(state.snapshot.LeaseExpiresAt) || !a.GeoReady(state) {
			http.Error(w, "not ready", 503)
			return
		}
		w.WriteHeader(200)
		return
	}
	host := r.Host
	if strings.Contains(host, ":") {
		var err error
		host, _, err = net.SplitHostPort(host)
		if err != nil {
			http.Error(w, "invalid host", 400)
			return
		}
	}
	if normalizeHost(host) != a.cfg.Hostname {
		http.Error(w, "unknown host", 421)
		return
	}
	ip, err := a.ClientIP(r)
	if err != nil {
		http.Error(w, "invalid proxy chain", 400)
		return
	}
	state := a.current.Load()
	if a.blocked.Load() || state == nil || !time.Now().Before(state.snapshot.LeaseExpiresAt) || !a.GeoReady(state) {
		http.Error(w, "configuration unavailable", 503)
		return
	}
	// One exact, unescaped segment; incoming query parameters are not forwarded in B06.
	slug := strings.TrimPrefix(r.URL.Path, "/")
	if r.URL.RawPath != "" || !slugPattern.MatchString(slug) {
		http.NotFound(w, r)
		return
	}
	destination, ok := state.routes[slug]
	if !ok {
		http.NotFound(w, r)
		return
	}
	signals := policy.Detect(r.UserAgent())
	if result, e := a.geo.Lookup(ip); e == nil {
		if policy.ValidCountry(result.Country) {
			signals.Country = result.Country
		}
		if result.ASN > 0 {
			asn := result.ASN
			signals.ASN = &asn
		}
	}
	feed := a.feeds.Lookup(ip, time.Now())
	signals.TorExit = feed.Tor
	signals.ReputationFlag = feed.Reputation
	signals.FeedStatus = feed.Status
	signals.TorStatus = feed.TorStatus
	signals.ReputationStatus = feed.ReputationStatus
	if state.snapshot.Protection.Mode != "off" {
		signals.RateLimited = !state.limiter.Allow(ip, time.Now())
	}
	decision := state.engine.EvaluateProtection(state.linkIDs[slug], destination, signals, state.snapshot.Protection)
	if r.Method == http.MethodGet {
		var id [32]byte
		if _, e := rand.Read(id[:]); e == nil {
			decision.EventID = base64.RawURLEncoding.EncodeToString(id[:])
			decision.ConfigRevision = state.snapshot.Revision
			decision.GeoRelease = a.geo.ActiveRelease()
			decision.FeedVersion = feed.Version
			a.queue.Enqueue(policy.TrafficEvent{Decision: decision, OccurredAt: time.Now().UTC(), DomainID: a.cfg.DomainID, LinkID: state.linkIDs[slug], Country: signals.Country, Device: signals.Device, OS: signals.OS, Browser: signals.Browser, Source: "go", OSVersion: signals.OSVersion, BrowserVersion: signals.BrowserVersion, DeviceBrand: signals.DeviceBrand, DeviceModel: signals.DeviceModel})
		}
	}
	if decision.Action == "block" {
		http.Error(w, "access denied", 403)
		return
	}
	w.Header().Set("Location", *decision.Destination)
	w.WriteHeader(http.StatusFound)
}

// RunGeo downloads on startup and every six hours; failure retries use capped backoff.
func (a *Agent) RunGeo(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := a.geo.Cleanup(); err != nil {
					slog.Warn("geo cleanup failed")
				}
			}
		}
	}()
	delay := time.Duration(0)
	failures := 0
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if a.blocked.Load() {
			return
		}
		if err := a.geo.Cleanup(); err != nil {
			slog.Warn("geo cleanup failed")
		}
		if err := a.geo.Update(ctx); err != nil {
			if errors.Is(err, geoip.ErrCredential) {
				_ = a.rejectCredential()
				return
			}
			if ctx.Err() != nil {
				return
			}
			slog.Warn("geo update", "error", err.Error())
			failures++
			if failures > 5 {
				failures = 5
			}
			delay = time.Duration(1<<failures) * 15 * time.Second
		} else {
			failures = 0
			delay = 6 * time.Hour
			// Report the newly activated database without waiting for the next config poll.
			a.syncMu.Lock()
			if state := a.current.Load(); state != nil && !a.blocked.Load() {
				if err := a.heartbeat(ctx, state.snapshot.Revision); err != nil && ctx.Err() == nil {
					slog.Warn("geo heartbeat", "error", err.Error())
				}
			}
			a.syncMu.Unlock()
		}
		var jitter [1]byte
		_, _ = rand.Read(jitter[:])
		delay += time.Duration(jitter[0]) * time.Second
	}
}
func (a *Agent) GeoReady(state *active) bool {
	id := a.geo.ActiveRelease()
	if a.cfg.GeoIPRequired && id == nil {
		return false
	}
	if state.snapshot.GeoRelease != nil && (id == nil || *id != *state.snapshot.GeoRelease) {
		return false
	}
	return true
}
func (a *Agent) LookupGeo(ip netip.Addr) (geoip.Result, error) { return a.geo.Lookup(ip) }
