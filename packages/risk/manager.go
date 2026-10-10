package risk

import (
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type activeFeed struct {
	env             Envelope
	feed            Feed
	tor, reputation *Index
}
type Manager struct {
	path               string
	key                ed25519.PublicKey
	origin, credential string
	client             *http.Client
	mu                 sync.Mutex
	active             atomic.Pointer[activeFeed]
}
type Lookup struct {
	Tor              bool
	Reputation       bool
	Status           string
	Version          *string
	TorStatus        string
	ReputationStatus string
}

func NewManager(root string, key ed25519.PublicKey, origin, credential string) (*Manager, error) {
	m := &Manager{path: filepath.Join(root, "current.json"), key: key, origin: strings.TrimRight(origin, "/"), credential: credential, client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	env, feed, _, e := Read(root, key, time.Now(), false)
	if os.IsNotExist(e) {
		return m, nil
	}
	if e != nil {
		return nil, e
	}
	m.install(env, feed)
	return m, nil
}
func (m *Manager) install(env Envelope, feed Feed) {
	tor, _ := NewIndex(feed.Tor)
	rep, _ := NewIndex(feed.Reputation)
	m.active.Store(&activeFeed{env, feed, tor, rep})
}
func (m *Manager) Lookup(ip netip.Addr, now time.Time) Lookup {
	state := m.active.Load()
	result := Lookup{Status: "unavailable", TorStatus: "unavailable", ReputationStatus: "unavailable"}
	if state == nil {
		return result
	}
	if !now.Before(state.feed.ExpiresAt) {
		result.Status = "expired"
		result.TorStatus = "expired"
		result.ReputationStatus = "expired"
		return result
	}
	result.Status = "fresh"
	v := state.env.Version
	result.Version = &v
	source := func(t *time.Time) string {
		if t == nil {
			return "unconfigured"
		}
		if now.Sub(*t) > 48*time.Hour {
			return "stale"
		}
		return "fresh"
	}
	result.TorStatus = source(state.feed.TorFetchedAt)
	result.ReputationStatus = source(state.feed.ReputationFetchedAt)
	result.Tor = result.TorStatus == "fresh" && state.tor.Contains(ip)
	result.Reputation = result.ReputationStatus == "fresh" && state.reputation.Contains(ip)
	return result
}
func (m *Manager) Version() *string { return m.Lookup(netip.Addr{}, time.Now()).Version }
func (m *Manager) Update(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	req, e := http.NewRequestWithContext(ctx, "GET", m.origin+"/v1/assets/feeds/manifest", nil)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+m.credential)
	old := m.active.Load()
	if old != nil {
		req.Header.Set("If-None-Match", `"`+old.env.Version+`"`)
	}
	response, e := m.client.Do(req)
	if e != nil {
		return errors.New("feed unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return ErrCredential
	}
	if response.StatusCode == 304 {
		if old == nil || !time.Now().Before(old.feed.ExpiresAt) {
			return errors.New("feed expired")
		}
		return nil
	}
	if response.StatusCode != 200 {
		return errors.New("feed unavailable")
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, MaxBytes+1))
	if e != nil {
		return e
	}
	env, feed, e := Verify(raw, m.key, time.Now(), true)
	if e != nil {
		return e
	}
	if old != nil && (feed.PublishedAt.Before(old.feed.PublishedAt) || feed.PublishedAt.Equal(old.feed.PublishedAt) && env.Version != old.env.Version) {
		return errors.New("feed rollback rejected")
	}
	if e = AtomicFile(m.path, raw); e != nil {
		return e
	}
	m.install(env, feed)
	return nil
}
