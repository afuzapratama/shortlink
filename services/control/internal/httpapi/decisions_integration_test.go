package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"shortlink.local/geoip"
	"shortlink.local/geoip/testutil"
	"shortlink.local/policy"
	"shortlink.local/risk"
	"strings"
	"sync"
	"testing"
	"time"
)

const apiIphone = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"

func apiFixture(t *testing.T) (*Server, *store.Store, store.Domain, store.Agent, store.Link, string) {
	t.Helper()
	s, db := setup(t)
	ctx := context.Background()
	s.cfg.GeoIPRoot = t.TempDir()
	seed, err := db.Seed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = testutil.Staging(s.cfg.GeoIPRoot, time.Now().Add(-time.Hour).Unix(), "ID"); err != nil {
		t.Fatal(err)
	}
	if _, err = geoip.Publish(s.cfg.GeoIPRoot, seed, time.Now()); err != nil {
		t.Fatal(err)
	}
	d, err := db.CreateDomain(ctx, security.Token(), "api.example.test", "api")
	if err != nil {
		t.Fatal(err)
	}
	ch, _ := db.Challenge(ctx, d.ID, "test")
	if err = db.Verify(ctx, d.ID, ch.Value, "test"); err != nil {
		t.Fatal(err)
	}
	tok, err := db.IssueToken(ctx, d.ID, "php_zip", "test")
	if err != nil {
		t.Fatal(err)
	}
	w := request(s, "POST", "/v1/agents/install-context", `{"token":"`+tok.Token+`"}`, "", "", nil)
	status(t, w, 200)
	a, credential, err := db.Enroll(ctx, tok.Token, tok.InstallationID, security.Token(), d.Hostname, "php_zip", "b15-test")
	if err != nil {
		t.Fatal(err)
	}
	l, err := db.CreateLink(ctx, security.Token(), d.ID, "hello", "https://example.org/first", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishLink(ctx, l.ID, l.Revision, 0, "test"); err != nil {
		t.Fatal(err)
	}
	l, err = db.Link(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, err = db.Domain(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s, db, d, a, l, credential
}
func decisionBody(d string, ua string) decisionInput {
	in := decisionInput{EventID: security.Token(), OccurredAt: time.Now().UTC(), DomainID: d, Slug: "hello"}
	in.Signals.IP = "1.1.1.1"
	in.Signals.UserAgent = &ua
	return in
}
func agentCall(s *Server, path, credential string, input any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(input)
	r := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
	r.RemoteAddr = "8.8.8.8:443"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+credential)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestAPIDecisionParityReplayConcurrencyAndPrivacy(t *testing.T) {
	s, db, d, a, l, credential := apiFixture(t)
	ctx := context.Background()
	in := decisionBody(d.ID, apiIphone)
	status(t, agentCall(s, "/v1/decisions", "", in), 401)
	cookie, _ := login(t, s, nil)
	raw, _ := json.Marshal(in)
	status(t, request(s, "POST", "/v1/decisions", string(raw), testOrigin, "", cookie), 401)
	rule := policy.Rule{ID: "iphone", Name: "ID iPhone", Priority: 1, Country: "ID", Device: "mobile", OS: "ios", Browser: "safari", Risk: "*", Action: "fallback", DestinationSource: "custom", Destination: "https://example.org/ios", Enabled: true, ASN: func() *uint32 { n := uint32(64512); return &n }()}
	rs, err := db.CreateRuleSet(ctx, "domain", &d.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	rs, err = db.UpdateRuleSet(ctx, rs.ID, rs.Revision, []policy.Rule{rule}, "test")
	if err != nil {
		t.Fatal(err)
	}
	published, err := db.PublishRules(ctx, rs.ID, rs.Revision, 0, "test")
	if err != nil || len(published.PendingAgentIDs) != 0 {
		t.Fatal(published, err)
	}
	// Concurrent same-ID requests return one immutable decision/event.
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); responses <- agentCall(s, "/v1/decisions", credential, in) }()
	}
	wg.Wait()
	close(responses)
	created := 0
	var first policy.Decision
	for w := range responses {
		status(t, w, 200)
		if w.Header().Get("X-Decision-Replayed") == "false" {
			created++
		}
		var result policy.Decision
		if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Action != "fallback" || result.RuleID == nil || *result.RuleID != "iphone" || result.Destination == nil || *result.Destination != rule.Destination {
			t.Fatal(result)
		}
		first = result
	}
	if created != 1 {
		t.Fatal("new decisions", created)
	}
	signals := policy.Detect(apiIphone)
	signals.Country = "ID"
	signals.ASN = rule.ASN
	sim, err := db.Simulate(ctx, l.ID, signals, "", nil)
	if err != nil || sim.Action != first.Action || *sim.RuleID != *first.RuleID {
		t.Fatal(sim, err)
	}
	var eventJSON, hash string
	if err = db.DB.QueryRow(ctx, "SELECT payload::text FROM traffic_events WHERE agent_id=$1", a.ID).Scan(&eventJSON); err != nil {
		t.Fatal(err)
	}
	if err = db.DB.QueryRow(ctx, "SELECT request_hash FROM api_decisions WHERE agent_id=$1", a.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(eventJSON, `"clientIp": "1.1.1.1"`) && !strings.Contains(eventJSON, `"clientIp":"1.1.1.1"`) || strings.Contains(eventJSON, "8.8.8.8") || strings.Contains(eventJSON, "Mozilla/") || len(hash) != 64 {
		t.Fatal("visitor log fields or raw UA exclusion incorrect")
	}
	var event policy.TrafficEvent
	json.Unmarshal([]byte(eventJSON), &event)
	if event.ClientIP != "1.1.1.1" || event.ASN == nil || *event.ASN != 64512 || event.ASNOrganization != "TEST NETWORK" || event.Source != "php" || event.Country != "ID" || event.OS != "ios" || event.Device != "mobile" {
		t.Fatal(event)
	}
	mapped := in
	mapped.Signals.IP = "::ffff:1.1.1.1"
	status(t, agentCall(s, "/v1/decisions", credential, mapped), 200)
	// Destination/rules change affects new IDs, while retry retains old response.
	l, err = db.UpdateLink(ctx, l.ID, d.ID, l.Slug, "https://example.org/second", l.Revision, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishLink(ctx, l.ID, l.Revision, 0, "test"); err != nil {
		t.Fatal(err)
	}
	w := agentCall(s, "/v1/decisions", credential, in)
	status(t, w, 200)
	if w.Header().Get("X-Decision-Replayed") != "true" {
		t.Fatal(w.Header())
	}
	replayed := readJSON[policy.Decision](t, w)
	if replayed.ConfigRevision != first.ConfigRevision || *replayed.Destination != *first.Destination {
		t.Fatal(replayed)
	}
	changed := in
	changed.Signals.IP = "8.8.8.8"
	status(t, agentCall(s, "/v1/decisions", credential, changed), 409)
	changed = in
	changed.DomainID = "foreign"
	status(t, agentCall(s, "/v1/decisions", credential, changed), 422)
	next := decisionBody(d.ID, "desktop")
	next.Signals.IP = "8.8.8.8"
	w = agentCall(s, "/v1/decisions", credential, next)
	status(t, w, 200)
	newD := readJSON[policy.Decision](t, w)
	if newD.ConfigRevision <= first.ConfigRevision || newD.Destination == nil || *newD.Destination != "https://example.org/second" {
		t.Fatal(newD)
	}
	if n, e := db.AggregateEvents(ctx, 100); e != nil || n != 2 {
		t.Fatal(n, e)
	}
	if n, e := db.AggregateEvents(ctx, 100); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	l, _ = db.Link(ctx, l.ID)
	if l.Clicks != 2 {
		t.Fatal(l)
	}
	status(t, agentCall(s, "/v1/events/batch", credential, policy.EventBatch{Events: []policy.TrafficEvent{event}}), 400)
	event.Source = "go"
	status(t, agentCall(s, "/v1/events/batch", credential, policy.EventBatch{Events: []policy.TrafficEvent{event}}), 422)
	if err = db.Revoke(ctx, a.ID, "test"); err != nil {
		t.Fatal(err)
	}
	status(t, agentCall(s, "/v1/decisions", credential, in), 401)
}
func TestAPIDecisionFailClosedRollbackDeadlineAndProbe(t *testing.T) {
	s, db, d, a, _, credential := apiFixture(t)
	ctx := context.Background()
	in := decisionBody(d.ID, apiIphone)
	bad := in
	bad.Slug = "missing"
	status(t, agentCall(s, "/v1/decisions", credential, bad), 404)
	bad = in
	bad.Signals.IP = "1.1.1.1:443"
	status(t, agentCall(s, "/v1/decisions", credential, bad), 400)
	bad = in
	bad.OccurredAt = time.Now().Add(-49 * time.Hour)
	status(t, agentCall(s, "/v1/decisions", credential, bad), 400)
	raw, _ := json.Marshal(in)
	payload := strings.Replace(string(raw), `"ip":"1.1.1.1"`, `"ip":"1.1.1.1","country":"US"`, 1)
	r := httptest.NewRequest("POST", "/v1/decisions", strings.NewReader(payload))
	r.Header.Set("Authorization", "Bearer "+credential)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	status(t, w, 400)
	// An event write failure cannot leave a replay response or acknowledgement behind.
	if _, err := db.DB.Exec(ctx, `CREATE FUNCTION fail_api_decision() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected'; END $$; CREATE TRIGGER fail_api_decision BEFORE INSERT ON api_decisions FOR EACH ROW EXECUTE FUNCTION fail_api_decision();`); err != nil {
		t.Fatal(err)
	}
	status(t, agentCall(s, "/v1/decisions", credential, in), 503)
	h, err := db.EventHealth(ctx)
	if err != nil || h.Pending != 0 {
		t.Fatal(h, err)
	}
	var keys int
	if err = db.DB.QueryRow(ctx, "SELECT count(*) FROM event_keys").Scan(&keys); err != nil || keys != 0 {
		t.Fatal(keys, err)
	}
	db.DB.Exec(ctx, "DROP TRIGGER fail_api_decision ON api_decisions")
	// Waiting for a database lock respects the endpoint deadline and never emits a target.
	tx, err := db.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(718204012))
	start := time.Now()
	w = agentCall(s, "/v1/decisions", credential, in)
	tx.Rollback(ctx)
	status(t, w, 503)
	if time.Since(start) > 4*time.Second || strings.Contains(w.Body.String(), "https://example.org") {
		t.Fatal("unsafe timeout", time.Since(start))
	}
	s.decisions.mu.Lock()
	s.decisions.geo.Close()
	s.decisions.geo = nil
	s.decisions.checked = time.Now()
	s.decisions.mu.Unlock()
	status(t, agentCall(s, "/v1/decisions", credential, in), 503)
	s.decisions.mu.Lock()
	s.decisions.checked = time.Time{}
	s.decisions.geoVersion = ""
	s.decisions.mu.Unlock()
	status(t, agentCall(s, "/v1/decisions", credential, in), 200)
	probe := map[string]string{"domainId": d.ID, "hostname": d.Hostname, "nonce": security.Token(), "version": "b15-test"}
	w = agentCall(s, "/v1/agents/probe", credential, probe)
	status(t, w, 200)
	if !policy.CheckProbeMAC(policy.ProbeMAC(policy.ProbeDigest(credential), "response", w.Body.Bytes()), w.Header().Get("X-Shortlink-Proof")) {
		t.Fatal("probe proof")
	}
	state := readJSON[policy.ProbeState](t, w)
	if !state.Ready || state.DomainID != d.ID || state.Nonce != probe["nonce"] {
		t.Fatal(state)
	}
	var count int
	db.DB.QueryRow(ctx, "SELECT count(*) FROM traffic_events").Scan(&count)
	if count != 1 {
		t.Fatal("probe recorded click", count)
	}
	probe["hostname"] = "foreign.example.test"
	status(t, agentCall(s, "/v1/agents/probe", credential, probe), 422)
	probe["hostname"] = d.Hostname
	// Real public-probe orchestration uses a controlled network callback, not a fake production readiness claim.
	s.publicProbe = func(ctx context.Context, host, nonce, digest string) (policy.ProbeState, error) {
		probe["nonce"] = nonce
		res := agentCall(s, "/v1/agents/probe", credential, probe)
		if res.Code != 200 {
			return policy.ProbeState{}, context.Canceled
		}
		var p policy.ProbeState
		json.Unmarshal(res.Body.Bytes(), &p)
		return p, nil
	}
	cookie, csrf := login(t, s, nil)
	status(t, request(s, "POST", "/v1/admin/domains/"+d.ID+"/probe", "", testOrigin, csrf, cookie), 200)
	d, _ = db.Domain(ctx, d.ID)
	if d.Readiness != "ready" {
		t.Fatal(d)
	}
	db.DB.Exec(ctx, "UPDATE domains SET last_seen_at=now()-interval '10 minutes' WHERE id=$1", d.ID)
	d, _ = db.Domain(ctx, d.ID)
	if d.Connection != "idle" || d.Readiness != "ready" {
		t.Fatal("idle PHP marked offline", d)
	}
	db.DB.Exec(ctx, "UPDATE domains SET probe_checked_at=now()-interval '6 minutes' WHERE id=$1", d.ID)
	d, _ = db.Domain(ctx, d.ID)
	if d.Readiness != "attention" || d.Connection != "idle" {
		t.Fatal(d)
	}
	if err = db.Revoke(ctx, a.ID, "test"); err != nil {
		t.Fatal(err)
	}
	status(t, agentCall(s, "/v1/agents/probe", credential, probe), 401)
}
func TestAPIRiskObserveEnforceFeedAndRetryBudget(t *testing.T) {
	s, db, d, _, _, credential := apiFixture(t)
	ctx := context.Background()
	seed, _ := db.Seed(ctx)
	s.cfg.RiskFeedRoot = t.TempDir()
	if err := risk.AtomicFile(filepath.Join(s.cfg.RiskFeedRoot, "staging", "tor.txt"), []byte("1.1.1.1\n")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := risk.Publish(s.cfg.RiskFeedRoot, seed, time.Now()); err != nil {
		t.Fatal(err)
	}
	rs, err := db.CreateRuleSet(ctx, "domain", &d.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	r := policy.Rule{ID: "tor", Name: "Tor", Priority: 1, Country: "*", Device: "*", OS: "*", Browser: "*", Risk: "tor_exit", Action: "block", Enabled: true}
	rs, err = db.UpdateRuleSet(ctx, rs.ID, rs.Revision, []policy.Rule{r}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishRules(ctx, rs.ID, rs.Revision, 0, "test"); err != nil {
		t.Fatal(err)
	}
	in := decisionBody(d.ID, apiIphone)
	w := agentCall(s, "/v1/decisions", credential, in)
	status(t, w, 200)
	decision := readJSON[policy.Decision](t, w)
	if decision.Action != "redirect" || decision.Risk == nil || decision.Risk.CandidateAction != "block" || decision.FeedVersion == nil {
		t.Fatal(decision)
	}
	d, _ = db.Domain(ctx, d.ID)
	if _, err = db.SetProtection(ctx, d.ID, d.DesiredRevision, policy.Protection{Mode: "enforce", RatePerSecond: 1, Burst: 2}, "test"); err != nil {
		t.Fatal(err)
	}
	newIn := decisionBody(d.ID, apiIphone)
	w = agentCall(s, "/v1/decisions", credential, newIn)
	status(t, w, 200)
	decision = readJSON[policy.Decision](t, w)
	if decision.Action != "block" || decision.Risk == nil {
		t.Fatal(decision)
	}
	for i := 0; i < 4; i++ {
		status(t, agentCall(s, "/v1/decisions", credential, newIn), 200)
	}
	next := decisionBody(d.ID, apiIphone)
	w = agentCall(s, "/v1/decisions", credential, next)
	status(t, w, 200)
	last := readJSON[policy.Decision](t, w)
	for _, signal := range last.Risk.Signals {
		if signal == "rate_limited" {
			t.Fatal("retries consumed visitor budget")
		}
	}
}

func TestDecisionBatchFinalStatementFailureRollsBackAllWrites(t *testing.T) {
	s, db, d, _, _, credential := apiFixture(t)
	ctx := context.Background()
	var before, after string
	query := `SELECT jsonb_build_object('connection',connection,'lastSeen',last_seen_at,'applied',applied_revision)::text FROM domains WHERE id=$1`
	if err := db.DB.QueryRow(ctx, query, d.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(ctx, `CREATE FUNCTION fail_agent_batch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected final statement'; END $$; CREATE TRIGGER fail_agent_batch BEFORE UPDATE ON agents FOR EACH ROW EXECUTE FUNCTION fail_agent_batch();`); err != nil {
		t.Fatal(err)
	}
	in := decisionBody(d.ID, apiIphone)
	status(t, agentCall(s, "/v1/decisions", credential, in), 503)
	var keys, events, replays int
	if err := db.DB.QueryRow(ctx, "SELECT (SELECT count(*) FROM event_keys),(SELECT count(*) FROM traffic_events),(SELECT count(*) FROM api_decisions)").Scan(&keys, &events, &replays); err != nil {
		t.Fatal(err)
	}
	if keys != 0 || events != 0 || replays != 0 {
		t.Fatal("batch left partial durable writes")
	}
	if err := db.DB.QueryRow(ctx, query, d.ID).Scan(&after); err != nil || before != after {
		t.Fatal("domain update survived batch rollback", err)
	}
	if _, err := db.DB.Exec(ctx, "DROP TRIGGER fail_agent_batch ON agents"); err != nil {
		t.Fatal(err)
	}
	status(t, agentCall(s, "/v1/decisions", credential, in), 200)
	replay := agentCall(s, "/v1/decisions", credential, in)
	status(t, replay, 200)
	if replay.Header().Get("X-Decision-Replayed") != "true" {
		t.Fatal("retry did not replay committed batch")
	}
}
