package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	lru "github.com/hashicorp/golang-lru"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/netip"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"shortlink.local/geoip"
	"shortlink.local/policy"
	"shortlink.local/risk"
	"sync"
	"time"
)

type decisionInput struct {
	EventID    string    `json:"eventId"`
	OccurredAt time.Time `json:"occurredAt"`
	DomainID   string    `json:"domainId"`
	Slug       string    `json:"slug"`
	Signals    struct {
		IP        string  `json:"ip"`
		UserAgent *string `json:"userAgent"`
	} `json:"signals"`
}
type limiterEntry struct {
	settings policy.Protection
	limiter  *risk.Limiter
}
type decisionRuntime struct {
	mu                      sync.Mutex
	seed                    []byte
	key                     ed25519.PublicKey
	geo                     *geoip.Manager
	feed                    *risk.Manager
	geoVersion, feedVersion string
	checked                 time.Time
	limiters                *lru.Cache
}

func (s *Server) WarmDecisions(ctx context.Context) error {
	_, _, _, err := s.classify(ctx, "", policy.Detect(""), netip.MustParseAddr("1.1.1.1"), policy.DefaultProtection(), false)
	return err
}
func (s *Server) Close() {
	s.decisions.mu.Lock()
	defer s.decisions.mu.Unlock()
	if s.decisions.geo != nil {
		s.decisions.geo.Close()
		s.decisions.geo = nil
	}
}
func (s *Server) requestFingerprint(ctx context.Context, input decisionInput) (string, error) {
	d := &s.decisions
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seed == nil {
		seed, err := s.store.Seed(ctx)
		if err != nil {
			return "", err
		}
		d.seed = seed
		d.key = ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	h := hmac.New(sha256.New, d.seed)
	h.Write([]byte("shortlink-api-decision-v1:"))
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (s *Server) classify(ctx context.Context, domain string, signals policy.Signals, ip netip.Addr, p policy.Protection, consume bool) (policy.Signals, *string, *string, error) {
	d := &s.decisions
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	if d.key == nil {
		seed, err := s.store.Seed(ctx)
		if err != nil {
			return policy.Signals{}, nil, nil, err
		}
		d.seed = seed
		d.key = ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	}
	if d.checked.IsZero() || now.Sub(d.checked) > time.Minute {
		d.checked = now
		if s.cfg.GeoIPRoot != "" {
			env, _, err := geoip.ReadManifest(s.cfg.GeoIPRoot, d.key, now, true)
			if err == nil && env.ReleaseID != d.geoVersion {
				m, e := geoip.NewManager(s.cfg.GeoIPRoot, d.key, "", "")
				if e == nil {
					old := d.geo
					d.geo = m
					d.geoVersion = env.ReleaseID
					if old != nil {
						old.Close()
					}
				}
			}
		}
		if s.cfg.RiskFeedRoot != "" {
			env, _, _, err := risk.Read(s.cfg.RiskFeedRoot, d.key, now, false)
			if err == nil && env.Version != d.feedVersion {
				m, e := risk.NewManager(s.cfg.RiskFeedRoot, d.key, "", "")
				if e == nil {
					d.feed = m
					d.feedVersion = env.Version
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return policy.Signals{}, nil, nil, err
	}
	if d.geo == nil {
		return policy.Signals{}, nil, nil, errors.New("central GeoLite unavailable")
	}
	geo, err := d.geo.Lookup(ip)
	if err != nil {
		return policy.Signals{}, nil, nil, err
	}
	signals.ASNOrganization = geo.Organization
	signals.Country = geo.Country
	if signals.Country == "" {
		signals.Country = "unknown"
	}
	if geo.ASN > 0 {
		asn := geo.ASN
		signals.ASN = &asn
	}
	var version *string
	if d.feed != nil {
		f := d.feed.Lookup(ip, now)
		signals.TorExit = f.Tor
		signals.ReputationFlag = f.Reputation
		signals.FeedStatus = f.Status
		signals.TorStatus = f.TorStatus
		signals.ReputationStatus = f.ReputationStatus
		version = f.Version
	}
	if consume && p.Mode != "off" {
		if d.limiters == nil {
			d.limiters, _ = lru.New(128)
		}
		entry, ok := d.limiters.Get(domain)
		var l *limiterEntry
		if ok {
			l = entry.(*limiterEntry)
		}
		if l == nil || l.settings.RatePerSecond != p.RatePerSecond || l.settings.Burst != p.Burst {
			l = &limiterEntry{p, risk.NewLimiter(p.RatePerSecond, p.Burst, 16384)}
			d.limiters.Add(domain, l)
		}
		signals.RateLimited = !l.limiter.Allow(ip, now)
	}
	return signals, &geo.ReleaseID, version, nil
}
func (s *Server) decisionFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrUsed):
		s.fail(w, 401, "unauthorized", "Agent is revoked.")
	case errors.Is(err, store.ErrBinding):
		s.fail(w, 422, "binding_mismatch", "Only the bound PHP/API domain is allowed.")
	case errors.Is(err, store.ErrEventConflict):
		s.fail(w, 409, "event_conflict", "Event ID has different request content.")
	case errors.Is(err, pgx.ErrNoRows):
		s.fail(w, 404, "not_found", "Published slug is unavailable.")
	default:
		s.unavailable(w)
	}
}
func (s *Server) makeDecision(w http.ResponseWriter, r *http.Request, a store.Agent) {
	if !s.method(w, r, http.MethodPost) {
		return
	}
	if a.Method != "php_zip" {
		s.decisionFailure(w, store.ErrBinding)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if !s.acquireDecision(w, r) {
		return
	}
	defer func() { <-s.decisionSlots }()
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var input decisionInput
	if !s.decode(w, r, &input) {
		return
	}
	ip, err := netip.ParseAddr(input.Signals.IP)
	if err != nil || ip.Zone() != "" || !ip.IsGlobalUnicast() || !security.ValidToken(input.EventID) || !slugPattern.MatchString(input.Slug) || input.Signals.UserAgent == nil || len(*input.Signals.UserAgent) > 4096 || input.OccurredAt.IsZero() || input.OccurredAt.Before(time.Now().Add(-48*time.Hour)) || input.OccurredAt.After(time.Now().Add(5*time.Minute)) {
		s.fail(w, 400, "validation_failed", "Invalid visitor IP, UA, event ID, timestamp or slug.")
		return
	}
	if input.DomainID != a.DomainID {
		s.decisionFailure(w, store.ErrBinding)
		return
	}
	input.Signals.IP = ip.Unmap().String()
	input.OccurredAt = input.OccurredAt.UTC()
	hash, err := s.requestFingerprint(ctx, input)
	if err != nil {
		s.unavailable(w)
		return
	}
	// UA parsing does not depend on mutable routing. Keep regex work outside SQL/domain locks.
	uaDone := store.Measure(ctx, "ua_parse")
	visitor := policy.Detect(*input.Signals.UserAgent)
	uaDone()
	// Provider network calls stay outside PostgreSQL decision locks.
	if s.proxy.Enabled(ctx) {
		if use, err := s.store.LinkUsesProxy(ctx, a.DomainID, input.Slug); err != nil {
			s.unavailable(w)
			return
		} else if use {
			proxyResult := s.proxy.Lookup(ctx, ip.Unmap())
			visitor.ProxyDetected = proxyResult.Detected
			visitor.ProxyStatus = proxyResult.Status
			visitor.IPQuality = proxyResult.Flags
		}
	}
	decision, replayed, err := s.store.MakeDecision(ctx, a, input.EventID, hash, input.Slug, input.OccurredAt, func(routing *store.Routing, link store.Link) (policy.TrafficEvent, error) {
		signals, geo, feed, e := s.classify(ctx, a.DomainID, visitor, ip.Unmap(), routing.Protection, true)
		if e != nil {
			return policy.TrafficEvent{}, e
		}
		d := routing.Engine.EvaluateRouting(link.ID, link.Destination, link.Routing, signals, routing.Protection)
		d.GeoRelease = geo
		d.FeedVersion = feed
		return policy.TrafficEvent{ClientIP: ip.Unmap().String(), ASN: signals.ASN, ASNOrganization: signals.ASNOrganization, Decision: d, Country: signals.Country, Device: signals.Device, OS: signals.OS, Browser: signals.Browser, OSVersion: signals.OSVersion, BrowserVersion: signals.BrowserVersion, DeviceBrand: signals.DeviceBrand, DeviceModel: signals.DeviceModel}, nil
	})
	if err != nil {
		s.decisionFailure(w, err)
		return
	}
	w.Header().Set("X-Decision-Replayed", "false")
	if replayed {
		w.Header().Set("X-Decision-Replayed", "true")
	}
	s.json(w, 200, decision)
}
func (s *Server) adapterProbe(w http.ResponseWriter, r *http.Request, a store.Agent) {
	if !s.method(w, r, http.MethodPost) {
		return
	}
	if a.Method != "php_zip" {
		s.decisionFailure(w, store.ErrBinding)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if !s.acquireDecision(w, r) {
		return
	}
	defer func() { <-s.decisionSlots }()
	var input struct {
		DomainID string `json:"domainId"`
		Hostname string `json:"hostname"`
		Nonce    string `json:"nonce"`
		Version  string `json:"version"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if !s.decode(w, r, &input) {
		return
	}
	if input.DomainID != a.DomainID {
		s.decisionFailure(w, store.ErrBinding)
		return
	}
	if !policy.ValidProbeNonce(input.Nonce) || len(input.Version) < 1 || len(input.Version) > 128 {
		s.fail(w, 400, "validation_failed", "Invalid nonce/version.")
		return
	}
	_, geo, _, err := s.classify(ctx, a.DomainID, policy.Detect(""), netip.MustParseAddr("1.1.1.1"), policy.DefaultProtection(), false)
	if err != nil {
		s.unavailable(w)
		return
	}
	state, err := s.store.AdapterProbe(ctx, a, input.Hostname, input.Version, input.Nonce, *geo)
	if err != nil {
		s.decisionFailure(w, err)
		return
	}
	raw, err := json.Marshal(state)
	if err != nil {
		s.unavailable(w)
		return
	}
	w.Header().Set("X-Shortlink-Proof", policy.ProbeMAC(policy.ProbeDigest(r.Header.Get("Authorization")[7:]), "response", raw))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(raw)
}

// Admission is bounded independently of DB connections and covered by the request deadline.
func (s *Server) acquireDecision(w http.ResponseWriter, r *http.Request) bool {
	defer store.Measure(r.Context(), "admission")()
	if r.Context().Err() != nil {
		s.fail(w, 503, "decision_busy", "Decision request canceled.")
		return false
	}
	select {
	case s.decisionSlots <- struct{}{}:
		return true
	default:
	}
	select {
	case s.decisionWaiting <- struct{}{}:
		defer func() { <-s.decisionWaiting }()
	default:
		s.fail(w, 503, "decision_busy", "Decision admission queue full.")
		return false
	}
	timer := time.NewTimer(25 * time.Millisecond)
	defer timer.Stop()
	select {
	case s.decisionSlots <- struct{}{}:
		return true
	case <-r.Context().Done():
	case <-timer.C:
	}
	s.fail(w, 503, "decision_busy", "Decision admission wait expired.")
	return false
}
