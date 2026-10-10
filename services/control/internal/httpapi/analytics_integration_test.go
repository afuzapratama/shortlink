package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/url"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"shortlink.local/policy"
	"strings"
	"testing"
	"time"
)

func TestTrafficPaginationAggregationAndExport(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	d, err := db.CreateDomain(ctx, security.Token(), "analytics.example.test", "local")
	if err != nil {
		t.Fatal(err)
	}
	ch, _ := db.Challenge(ctx, d.ID, "test")
	if err = db.Verify(ctx, d.ID, ch.Value, "test"); err != nil {
		t.Fatal(err)
	}
	tok, _ := db.IssueToken(ctx, d.ID, "cli", "test")
	a, _, err := db.Enroll(ctx, tok.Token, tok.InstallationID, security.Token(), d.Hostname, "cli", "b13")
	if err != nil {
		t.Fatal(err)
	}
	l, err := db.CreateLink(ctx, security.Token(), d.ID, "stats", "https://example.org", "test")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Minute)
	from := now.Add(-time.Hour)
	destination := "https://example.org"
	e := policy.TrafficEvent{Decision: policy.Decision{EventID: security.Token(), Action: "redirect", ReasonCode: "link_destination", ConfigRevision: 1, Destination: &destination}, OccurredAt: now.Add(-time.Minute), DomainID: d.ID, LinkID: l.ID, Country: "ID", Device: "mobile", OS: "ios", Browser: "safari", Source: "go"}
	e2 := e
	e2.EventID = security.Token()
	e2.Action = "block"
	e2.ReasonCode = "rule_block"
	e2.Destination = nil
	e2.Risk = &policy.RiskObservation{Mode: "observe", Signals: []string{"known_bot", "tor_exit"}, BotCategory: "search", FeedStatus: "fresh", TorStatus: "fresh", ReputationStatus: "unconfigured", CandidateAction: "block", CandidateReasonCode: "rule_block"}
	// Deliberately equal receipt timestamps: tie-break must use ingest ID.
	if _, _, err = db.IngestEvents(ctx, a, []policy.TrafficEvent{e, e2}); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := login(t, s, nil)
	q := url.Values{"from": {from.Format(time.RFC3339)}, "to": {now.Format(time.RFC3339)}, "limit": {"1"}}
	get := func(path string) []byte {
		w := request(s, "GET", path, "", "", "", cookie)
		status(t, w, 200)
		return w.Body.Bytes()
	}
	status(t, request(s, "GET", "/v1/admin/events?"+q.Encode(), "", "", "", nil), 401)
	var p struct {
		Items []store.EventRecord `json:"items"`
		Next  *string             `json:"nextCursor"`
	}
	json.Unmarshal(get("/v1/admin/events?"+q.Encode()), &p)
	if len(p.Items) != 1 || p.Items[0].Event.EventID != e2.EventID || p.Next == nil {
		t.Fatal(p)
	}
	cursor := *p.Next
	late := e
	late.EventID = security.Token()
	if _, _, err = db.IngestEvents(ctx, a, []policy.TrafficEvent{late}); err != nil {
		t.Fatal(err)
	}
	q.Set("cursor", cursor)
	json.Unmarshal(get("/v1/admin/events?"+q.Encode()), &p)
	if len(p.Items) != 1 || p.Items[0].Event.EventID != e.EventID || p.Next != nil {
		t.Fatal("late ingest leaked into page", p)
	}
	q.Set("country", "US")
	status(t, request(s, "GET", "/v1/admin/events?"+q.Encode(), "", "", "", cookie), 400)
	q.Del("cursor")
	q.Del("country")
	if _, err = db.AggregateEvents(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if n, er := db.AggregateEvents(ctx, 100); er != nil || n != 0 {
		t.Fatal(n, er)
	}
	var summary struct {
		Buckets []store.TrafficBucket `json:"buckets"`
		Health  store.EventHealth     `json:"health"`
	}
	json.Unmarshal(get("/v1/admin/analytics?"+q.Encode()), &summary)
	total, bot := int64(0), int64(0)
	for _, b := range summary.Buckets {
		total += b.Events
		bot += b.BotEvents
	}
	if total != 3 || bot != 1 || summary.Health.Pending != 0 || summary.Health.Watermark == nil {
		t.Fatal(summary)
	}
	q.Set("action", "block")
	json.Unmarshal(get("/v1/admin/analytics?"+q.Encode()), &summary)
	if len(summary.Buckets) != 1 || summary.Buckets[0].Events != 1 {
		t.Fatal(summary)
	}
	q.Del("action")
	q.Set("device", "mobile|desktop")
	status(t, request(s, "GET", "/v1/admin/events?"+q.Encode(), "", "", "", cookie), 400)
	q.Del("device")
	q.Set("from", from.Add(time.Second).Format(time.RFC3339))
	status(t, request(s, "GET", "/v1/admin/analytics?"+q.Encode(), "", "", "", cookie), 400)
	q.Set("from", from.Format(time.RFC3339))
	status(t, request(s, "POST", "/v1/admin/events/export?"+q.Encode(), "", testOrigin, "", cookie), 403)
	// Formula-like metadata must remain literal in CSV, including leading whitespace.
	if _, err = db.DB.Exec(ctx, `UPDATE traffic_events SET payload=jsonb_set(payload,'{eventId}','" @SUM(1)"'::jsonb) WHERE id=(SELECT min(id) FROM traffic_events)`); err != nil {
		t.Fatal(err)
	}
	w := request(s, "POST", "/v1/admin/events/export?"+q.Encode(), "", testOrigin, csrf, cookie)
	status(t, w, 200)
	records, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	if err != nil || len(records) != 4 || records[3][0] != "' @SUM(1)" || w.Header().Get("X-Export-Truncated") != "false" {
		t.Fatal("CSV", err, w.Body.String())
	}
	q.Set("from", now.Truncate(24*time.Hour).Add(-90*24*time.Hour).Format(time.RFC3339))
	status(t, request(s, "GET", "/v1/admin/analytics?"+q.Encode(), "", "", "", cookie), 400)
	q.Set("from", now.Add(-10*24*time.Hour).Format(time.RFC3339))
	status(t, request(s, "GET", "/v1/admin/events?"+q.Encode(), "", "", "", cookie), 400)
	status(t, request(s, "GET", "/v1/admin/analytics?"+q.Encode(), "", "", "", cookie), 200)
	q.Set("from", from.Format(time.RFC3339))
	// Full-hour and partial-hour rollups must produce the same exact total.
	older := e
	older.EventID = security.Token()
	older.OccurredAt = now.Truncate(time.Hour).Add(-90 * time.Minute)
	if _, _, err = db.IngestEvents(ctx, a, []policy.TrafficEvent{older}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AggregateEvents(ctx, 100); err != nil {
		t.Fatal(err)
	}
	q.Set("from", now.Truncate(time.Hour).Add(-3*time.Hour+17*time.Minute).Format(time.RFC3339))
	json.Unmarshal(get("/v1/admin/analytics?"+q.Encode()), &summary)
	total = 0
	bot = 0
	for _, b := range summary.Buckets {
		total += b.Events
		bot += b.BotEvents
	}
	if total != 4 || bot != 1 {
		t.Fatal("hour/minute boundary overlap", summary)
	}
	q.Set("from", from.Format(time.RFC3339))
	// Bounded export announces truncation instead of silently claiming completeness.
	_, err = db.DB.Exec(ctx, `INSERT INTO traffic_events(occurred_at,agent_id,domain_id,link_id,event_id,payload) SELECT occurred_at,agent_id,domain_id,link_id,event_id,payload FROM traffic_events CROSS JOIN generate_series(1,400)`)
	if err != nil {
		t.Fatal(err)
	}
	w = request(s, "POST", "/v1/admin/events/export?"+q.Encode(), "", testOrigin, csrf, cookie)
	status(t, w, 200)
	records, err = csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	if err != nil || len(records) != 1001 || w.Header().Get("X-Export-Truncated") != "true" {
		t.Fatal(len(records), err)
	}
}

func TestTrafficSeparatesAgentsWithSameEventID(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	cookie, _ := login(t, s, nil)
	now := time.Now().UTC().Truncate(time.Minute)
	sharedID := security.Token()
	var firstDomain string
	for i, host := range []string{"agent-one.example.test", "agent-two.example.test"} {
		d, err := db.CreateDomain(ctx, security.Token(), host, "local")
		if err != nil {
			t.Fatal(err)
		}
		ch, _ := db.Challenge(ctx, d.ID, "test")
		if err = db.Verify(ctx, d.ID, ch.Value, "test"); err != nil {
			t.Fatal(err)
		}
		token, _ := db.IssueToken(ctx, d.ID, "cli", "test")
		a, _, err := db.Enroll(ctx, token.Token, token.InstallationID, security.Token(), host, "cli", "b13")
		if err != nil {
			t.Fatal(err)
		}
		l, err := db.CreateLink(ctx, security.Token(), d.ID, "same", "https://example.org", "test")
		if err != nil {
			t.Fatal(err)
		}
		destination := "https://example.org"
		e := policy.TrafficEvent{Decision: policy.Decision{EventID: sharedID, Action: "redirect", ReasonCode: "link_destination", ConfigRevision: 1, Destination: &destination}, OccurredAt: now.Add(-time.Minute), DomainID: d.ID, LinkID: l.ID, Country: "ID", Device: "desktop", OS: "linux", Browser: "firefox", Source: "go"}
		if _, _, err = db.IngestEvents(ctx, a, []policy.TrafficEvent{e, e}); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			firstDomain = d.ID
		}
	}
	q := url.Values{"from": {now.Add(-time.Hour).Format(time.RFC3339)}, "to": {now.Format(time.RFC3339)}}
	w := request(s, "GET", "/v1/admin/events?"+q.Encode(), "", "", "", cookie)
	status(t, w, 200)
	var p struct {
		Items []store.EventRecord `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 || p.Items[0].AgentID == p.Items[1].AgentID || p.Items[0].ID == p.Items[1].ID || p.Items[0].Event.EventID != sharedID || p.Items[1].Event.EventID != sharedID {
		t.Fatal(p)
	}
	q.Set("domainId", firstDomain)
	w = request(s, "GET", "/v1/admin/events?"+q.Encode(), "", "", "", cookie)
	status(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &p)
	if len(p.Items) != 1 || p.Items[0].Event.DomainID != firstDomain {
		t.Fatal(p)
	}
	h, err := db.EventHealth(ctx)
	if err != nil || h.ActiveAgents != 2 || h.StaleAgents != 2 || h.Pending != 2 {
		t.Fatal(h, err)
	}
	if _, err = db.AggregateEvents(ctx, 100); err != nil {
		t.Fatal(err)
	}
	q.Del("domainId")
	w = request(s, "GET", "/v1/admin/analytics?"+q.Encode(), "", "", "", cookie)
	status(t, w, 200)
	var summary struct {
		Buckets []store.TrafficBucket `json:"buckets"`
	}
	json.Unmarshal(w.Body.Bytes(), &summary)
	if len(summary.Buckets) != 1 || summary.Buckets[0].Events != 2 {
		t.Fatal(summary)
	}
}
