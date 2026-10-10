package runtime

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"shortlink.local/policy"
	"shortlink.local/risk"
	"testing"
	"time"
)

func protectedRequest(a *Agent, ua string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/hello", nil)
	r.Host = a.cfg.Hostname
	r.Header.Set("User-Agent", ua)
	r.RemoteAddr = "8.8.8.8:12345"
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}
func TestRuntimeObserveEnforceRateAndSignedFeed(t *testing.T) {
	a, c, closeServer := testAgent(t)
	defer closeServer()
	s := fixture(1)
	s.Protection = policy.DefaultProtection()
	s.Protection.Burst = 1
	s.Rules = []policy.ScopedRule{{Rule: policy.Rule{ID: "crawler", Name: "Crawler", Priority: 1, Country: "*", Device: "*", OS: "*", Browser: "*", Risk: "known_bot", Action: "block", Enabled: true}, Scope: "global"}}
	c.raw = c.sign(s)
	if e := a.Sync(context.Background()); e != nil {
		t.Fatal(e)
	}
	ua := "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	if w := protectedRequest(a, ua); w.Code != 302 {
		t.Fatal("observation changed redirect", w.Code)
	}
	deadline := time.Now().Add(time.Second)
	for {
		events, e := a.queue.Batch(context.Background(), 100)
		if e != nil {
			t.Fatal(e)
		}
		if len(events) > 0 {
			if events[0].Risk == nil || events[0].Risk.CandidateAction != "block" || events[0].Action != "redirect" {
				t.Fatal(events[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("risk event missing")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Revision = 2
	s.Protection.Mode = "enforce"
	c.raw = c.sign(s)
	if e := a.Sync(context.Background()); e != nil {
		t.Fatal(e)
	}
	if w := protectedRequest(a, ua); w.Code != 403 {
		t.Fatal("bot enforcement", w.Code)
	}
	s.Revision = 3
	s.Rules[0].Risk = "rate_limited"
	c.raw = c.sign(s)
	if e := a.Sync(context.Background()); e != nil {
		t.Fatal(e)
	}
	if w := protectedRequest(a, "Mozilla/5.0"); w.Code != 403 {
		t.Fatal("same budget reset on rule-only sync", w.Code)
	}
	// Changing the budget intentionally resets the local limiter.
	s.Revision = 4
	s.Protection.Burst = 2
	c.raw = c.sign(s)
	if e := a.Sync(context.Background()); e != nil {
		t.Fatal(e)
	}
	if w := protectedRequest(a, "Mozilla/5.0"); w.Code != 302 {
		t.Fatal(w.Code)
	}
	if w := protectedRequest(a, "Mozilla/5.0"); w.Code != 302 {
		t.Fatal(w.Code)
	}
	if w := protectedRequest(a, "Mozilla/5.0"); w.Code != 403 {
		t.Fatal("burst bypass", w.Code)
	}
	now := time.Now().UTC()
	f := risk.Feed{Schema: 1, PublishedAt: now, ExpiresAt: now.Add(time.Hour), TorFetchedAt: &now, Tor: []string{"8.8.8.8"}, Reputation: []string{}}
	env, e := risk.Sign(f, c.private)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(env)
	if e = risk.AtomicFile(filepath.Join(a.cfg.DataDir, "risk", "current.json"), raw); e != nil {
		t.Fatal(e)
	}
	a.feeds, e = risk.NewManager(filepath.Join(a.cfg.DataDir, "risk"), a.key, a.cfg.ControlURL, a.cfg.Credential)
	if e != nil {
		t.Fatal(e)
	}
	s.Revision = 5
	s.Rules[0].Risk = "tor_exit"
	c.raw = c.sign(s)
	if e = a.Sync(context.Background()); e != nil {
		t.Fatal(e)
	}
	if w := protectedRequest(a, "Mozilla/5.0"); w.Code != 403 {
		t.Fatal("Tor enforcement", w.Code)
	}
	if a.feeds.Lookup(netip.MustParseAddr("8.8.8.8"), now.Add(2*time.Hour)).Tor {
		t.Fatal("expired feed enforced")
	}
	s.Revision = 6
	s.Protection.Mode = "off"
	c.raw = c.sign(s)
	if e = a.Sync(context.Background()); e != nil {
		t.Fatal(e)
	}
	if w := protectedRequest(a, ua); w.Code != 302 {
		t.Fatal("off mode blocked", w.Code)
	}
}
