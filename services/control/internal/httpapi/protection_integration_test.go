package httpapi

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"shortlink.local/policy"
	"shortlink.local/risk"
	"testing"
	"time"
)

func TestProtectionCapabilityObserveEnforceAndFeedAuth(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	cookie, csrf := login(t, s, nil)
	d, e := db.CreateDomain(ctx, security.Token(), "risk.example.test", "local")
	if e != nil {
		t.Fatal(e)
	}
	ch, e := db.Challenge(ctx, d.ID, "test")
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Verify(ctx, d.ID, ch.Value, "test"); e != nil {
		t.Fatal(e)
	}
	tok, e := db.IssueToken(ctx, d.ID, "cli", "test")
	if e != nil {
		t.Fatal(e)
	}
	agent, credential, e := db.Enroll(ctx, tok.Token, tok.InstallationID, security.Token(), d.Hostname, "cli", "b13")
	if e != nil {
		t.Fatal(e)
	}
	link, e := db.CreateLink(ctx, security.Token(), d.ID, "hello", "https://example.org", "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.PublishLink(ctx, link.ID, link.Revision, 0, "test"); e != nil {
		t.Fatal(e)
	}
	rules, e := db.CreateRuleSet(ctx, "domain", &d.ID, "test")
	if e != nil {
		t.Fatal(e)
	}
	r := policy.Rule{ID: "bot", Name: "Crawler", Priority: 1, Country: "*", Device: "*", OS: "*", Browser: "*", Risk: "known_bot", Action: "block", Enabled: true}
	rules, e = db.UpdateRuleSet(ctx, rules.ID, rules.Revision, []policy.Rule{r}, "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.PublishRules(ctx, rules.ID, rules.Revision, 0, "test"); e != store.ErrCapability {
		t.Fatal("old agent accepted risk", e)
	}
	d, _ = db.Domain(ctx, d.ID)
	if e = db.Heartbeat(ctx, agent, d.DesiredRevision, "b13", 0, 0, 0, nil, nil, store.QueueHealth{ProtectionVersion: 1}); e != nil {
		t.Fatal(e)
	}
	if _, e = db.PublishRules(ctx, rules.ID, rules.Revision, 0, "test"); e != nil {
		t.Fatal(e)
	}
	signals := policy.Detect("Googlebot/2.1")
	decision, e := db.Simulate(ctx, link.ID, signals, "", nil)
	if e != nil || decision.Action != "redirect" || decision.Risk == nil || decision.Risk.CandidateAction != "block" {
		t.Fatal("observation", decision, e)
	}
	path := "/v1/admin/domains/" + d.ID + "/protection"
	status(t, request(s, "GET", path, "", "", "", nil), 401)
	status(t, request(s, "GET", path, "", "", "", cookie), 200)
	d, _ = db.Domain(ctx, d.ID)
	input, _ := json.Marshal(map[string]any{"expectedRevision": d.DesiredRevision, "settings": policy.Protection{Mode: "enforce", RatePerSecond: 20, Burst: 40}})
	status(t, request(s, "PATCH", path, string(input), testOrigin, "", cookie), 403)
	status(t, request(s, "PATCH", path, string(input), testOrigin, csrf, cookie), 200)
	status(t, request(s, "PATCH", path, string(input), testOrigin, csrf, cookie), 409)
	decision, e = db.Simulate(ctx, link.ID, signals, "", nil)
	if e != nil || decision.Action != "block" {
		t.Fatal("enforcement", decision, e)
	}
	// Risk observation is durable, aggregated once, and remains distinguishable from actual redirects.
	destination := "https://example.org"
	event := policy.TrafficEvent{Decision: policy.Decision{EventID: security.Token(), Action: "redirect", ReasonCode: "link_destination", ConfigRevision: d.DesiredRevision, Destination: &destination, Risk: &policy.RiskObservation{Mode: "observe", Signals: []string{"known_bot"}, BotCategory: "search", FeedStatus: "unavailable", TorStatus: "unavailable", ReputationStatus: "unavailable", CandidateAction: "block", CandidateRuleID: &r.ID, CandidateReasonCode: "rule_block"}}, OccurredAt: time.Now().UTC(), DomainID: d.ID, LinkID: link.ID, Country: "unknown", Device: "unknown", OS: "unknown", Browser: "unknown", Source: "go"}
	if _, _, e = db.IngestEvents(ctx, agent, []policy.TrafficEvent{event}); e != nil {
		t.Fatal(e)
	}
	if _, e = db.AggregateEvents(ctx, 1000); e != nil {
		t.Fatal(e)
	}
	if _, e = db.AggregateEvents(ctx, 1000); e != nil {
		t.Fatal(e)
	}
	summary, e := db.RiskSummary(ctx, d.ID)
	if e != nil || len(summary) != 1 || summary[0].Events != 1 || summary[0].CandidateAction != "block" {
		t.Fatal(summary, e)
	}
	broken := *event.Risk
	broken.Signals = []string{"known_bot", "known_bot"}
	if validRiskObservation(broken) {
		t.Fatal("duplicate signals accepted")
	}
	seed, _ := db.Seed(ctx)
	s.cfg.RiskFeedRoot = t.TempDir()
	if e = risk.AtomicFile(filepath.Join(s.cfg.RiskFeedRoot, "staging", "tor.txt"), []byte("8.8.8.8\n")); e != nil {
		t.Fatal(e)
	}
	env, _, e := risk.Publish(s.cfg.RiskFeedRoot, seed, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	status(t, request(s, "GET", "/v1/assets/feeds/manifest", "", "", "", nil), 401)
	rr := httptest.NewRequest("GET", "/v1/assets/feeds/manifest", nil)
	rr.Header.Set("Authorization", "Bearer "+credential)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, rr)
	status(t, w, 200)
	if _, _, e = risk.Verify(w.Body.Bytes(), ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey), time.Now(), true); e != nil {
		t.Fatal(e)
	}
	rr.Header.Set("If-None-Match", `"`+env.Version+`"`)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, rr)
	status(t, w, 304)
	status(t, request(s, "GET", "/v1/admin/system/feeds", "", "", "", cookie), 200)
	db.DB.Exec(ctx, "UPDATE domains SET last_seen_at=now()-interval '5 minutes' WHERE id=$1", d.ID)
	d, _ = db.Domain(ctx, d.ID)
	if _, e = db.SetProtection(ctx, d.ID, d.DesiredRevision, policy.Protection{Mode: "enforce", RatePerSecond: 20, Burst: 40}, "test"); e != store.ErrCapability {
		t.Fatal("offline agent accepted enforce", e)
	}
	// Challenge without a supported provider remains rejected rather than silently allowing traffic.
	r.Action = "challenge"
	if _, e = db.UpdateRuleSet(ctx, rules.ID, rules.Revision+1, []policy.Rule{r}, "test"); e == nil {
		t.Fatal("challenge accepted")
	}
	if e = db.Revoke(ctx, agent.ID, "test"); e != nil {
		t.Fatal(e)
	}
	rr.Header.Del("If-None-Match")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, rr)
	status(t, w, 401)

}
