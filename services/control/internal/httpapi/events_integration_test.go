package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"shortlink.local/control/internal/security"
	"shortlink.local/policy"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDurableBatchBindingDedupWorkerAndRevoke(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	d, err := db.CreateDomain(ctx, security.Token(), "events.example.test", "local")
	if err != nil {
		t.Fatal(err)
	}
	ch, _ := db.Challenge(ctx, d.ID, "test")
	if err = db.Verify(ctx, d.ID, ch.Value, "test"); err != nil {
		t.Fatal(err)
	}
	tok, _ := db.IssueToken(ctx, d.ID, "cli", "test")
	agent, credential, err := db.Enroll(ctx, tok.Token, tok.InstallationID, security.Token(), d.Hostname, "cli", "b12")
	if err != nil {
		t.Fatal(err)
	}
	link, err := db.CreateLink(ctx, security.Token(), d.ID, "hello", "https://example.org", "test")
	if err != nil {
		t.Fatal(err)
	}
	destination := "https://example.org"
	event := policy.TrafficEvent{Decision: policy.Decision{EventID: security.Token(), Action: "redirect", ReasonCode: "link_destination", Destination: &destination, ConfigRevision: 1}, OccurredAt: time.Now().UTC(), DomainID: d.ID, LinkID: link.ID, Country: "ID", Device: "mobile", OS: "ios", Browser: "safari", Source: "go"}
	send := func(events []policy.TrafficEvent, secret string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(policy.EventBatch{Events: events})
		r := httptest.NewRequest("POST", "/v1/events/batch", strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
		if secret != "" {
			r.Header.Set("Authorization", "Bearer "+secret)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	status(t, send([]policy.TrafficEvent{event}, ""), 401)
	cookie, _ := login(t, s, nil)
	status(t, request(s, "POST", "/v1/events/batch", `{"events":[]}`, testOrigin, "", cookie), 401)
	status(t, send(nil, credential), 400)
	broken := event
	broken.Device = "mobile|desktop"
	status(t, send([]policy.TrafficEvent{broken}, credential), 400)
	broken = event
	broken.OccurredAt = time.Now().Add(-49 * time.Hour)
	status(t, send([]policy.TrafficEvent{broken}, credential), 400)
	broken = event
	broken.DomainID = "foreign"
	status(t, send([]policy.TrafficEvent{broken}, credential), 422)
	broken = event
	broken.LinkID = "foreign"
	status(t, send([]policy.TrafficEvent{broken}, credential), 422)
	response := send([]policy.TrafficEvent{event, event}, credential)
	status(t, response, 200)
	var ack policy.BatchAccepted
	json.Unmarshal(response.Body.Bytes(), &ack)
	if ack.Accepted != 1 || ack.Duplicates != 1 {
		t.Fatal(ack)
	}
	response = send([]policy.TrafficEvent{event}, credential)
	status(t, response, 200)
	json.Unmarshal(response.Body.Bytes(), &ack)
	if ack.Accepted != 0 || ack.Duplicates != 1 {
		t.Fatal("retry", ack)
	}
	broken = event
	broken.Country = "US"
	status(t, send([]policy.TrafficEvent{broken}, credential), 409)
	// A conflicting event rolls back earlier new events in the same batch.
	newEvent := event
	newEvent.EventID = security.Token()
	status(t, send([]policy.TrafficEvent{newEvent, broken}, credential), 409)
	health, err := db.EventHealth(ctx)
	if err != nil || health.Pending != 1 {
		t.Fatal(health, err)
	}
	// Inject a worker failure after rollup insertion; the transaction must undo it.
	if _, e := db.DB.Exec(ctx, `CREATE FUNCTION fail_event_counter() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected worker failure'; END $$; CREATE TRIGGER fail_event_counter BEFORE UPDATE ON links FOR EACH ROW EXECUTE FUNCTION fail_event_counter();`); e != nil {
		t.Fatal(e)
	}
	if _, e := db.AggregateEvents(ctx, 1000); e == nil {
		t.Fatal("worker failure did not propagate")
	}
	var partial int
	if e := db.DB.QueryRow(ctx, "SELECT count(*) FROM traffic_aggregates").Scan(&partial); e != nil || partial != 0 {
		t.Fatal("partial aggregation survived", partial, e)
	}
	if _, e := db.DB.Exec(ctx, "DROP TRIGGER fail_event_counter ON links; DROP FUNCTION fail_event_counter()"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := db.AggregateEvents(ctx, 1000); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if n, e := db.AggregateEvents(ctx, 1000); e != nil || n != 0 {
		t.Fatal("worker replay", n, e)
	}
	var count int64
	if err = db.DB.QueryRow(ctx, "SELECT sum(events) FROM traffic_aggregates WHERE grain='minute'").Scan(&count); err != nil || count != 1 {
		t.Fatal("double aggregate", count, err)
	}
	got, _ := db.Link(ctx, link.ID)
	if got.Clicks != 1 {
		t.Fatal("double link clicks", got.Clicks)
	}
	health, err = db.EventHealth(ctx)
	if err != nil || health.Pending != 0 || health.Watermark == nil {
		t.Fatal(health, err)
	}
	status(t, request(s, "GET", "/v1/admin/system/events", "", "", "", nil), 401)
	status(t, request(s, "GET", "/v1/admin/system/events", "", "", "", cookie), 200)
	// Another domain/agent can use the same random event ID without sharing dedup state.
	d2, e := db.CreateDomain(ctx, security.Token(), "events-two.example.test", "local")
	if e != nil {
		t.Fatal(e)
	}
	ch2, e := db.Challenge(ctx, d2.ID, "test")
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Verify(ctx, d2.ID, ch2.Value, "test"); e != nil {
		t.Fatal(e)
	}
	tok2, e := db.IssueToken(ctx, d2.ID, "cli", "test")
	if e != nil {
		t.Fatal(e)
	}
	_, cred2, e := db.Enroll(ctx, tok2.Token, tok2.InstallationID, security.Token(), d2.Hostname, "cli", "b12")
	if e != nil {
		t.Fatal(e)
	}
	l2, e := db.CreateLink(ctx, security.Token(), d2.ID, "hello", destination, "test")
	if e != nil {
		t.Fatal(e)
	}
	event2 := event
	event2.DomainID = d2.ID
	event2.LinkID = l2.ID
	response = send([]policy.TrafficEvent{event2}, cred2)
	status(t, response, 200)
	json.Unmarshal(response.Body.Bytes(), &ack)
	if ack.Accepted != 1 || ack.Duplicates != 0 {
		t.Fatal("cross-agent dedup", ack)
	}
	if _, e = db.AggregateEvents(ctx, 1000); e != nil {
		t.Fatal(e)
	}
	got, _ = db.Link(ctx, link.ID)
	other, _ := db.Link(ctx, l2.ID)
	if got.Clicks != 1 || other.Clicks != 1 {
		t.Fatal("cross-domain counts", got.Clicks, other.Clicks)
	}
	if err = db.Revoke(ctx, agent.ID, "test"); err != nil {
		t.Fatal(err)
	}
	status(t, send([]policy.TrafficEvent{event}, credential), 401)
	if _, _, err = db.IngestEvents(ctx, agent, []policy.TrafficEvent{event}); err == nil {
		t.Fatal("revoked agent passed commit guard")
	}
}
func TestPartitionRetentionKeepsPending(t *testing.T) {
	_, db := setup(t)
	ctx := context.Background()
	if _, err := db.DB.Exec(ctx, `CREATE TABLE traffic_events_20000101 PARTITION OF traffic_events FOR VALUES FROM ('2000-01-01') TO ('2000-01-02'); INSERT INTO traffic_events(occurred_at,agent_id,domain_id,link_id,event_id,payload) VALUES('2000-01-01','a','d','l','e','{}'); INSERT INTO traffic_aggregates VALUES('minute','2000-01-01','d','l','ID','mobile','ios','safari','redirect','link_destination',1);`); err != nil {
		t.Fatal(err)
	}
	if err := db.RetainEvents(ctx, 7, 90); err != nil {
		t.Fatal(err)
	}
	var exists bool
	db.DB.QueryRow(ctx, "SELECT to_regclass('traffic_events_20000101') IS NOT NULL").Scan(&exists)
	if !exists {
		t.Fatal("unprocessed partition dropped")
	}
	db.DB.Exec(ctx, "UPDATE traffic_events SET processed=true")
	if err := db.RetainEvents(ctx, 7, 90); err != nil {
		t.Fatal(err)
	}
	db.DB.QueryRow(ctx, "SELECT to_regclass('traffic_events_20000101') IS NOT NULL").Scan(&exists)
	if exists {
		t.Fatal("old partition retained")
	}
	var count int
	db.DB.QueryRow(ctx, "SELECT count(*) FROM traffic_aggregates").Scan(&count)
	if count != 0 {
		t.Fatal("aggregate retention")
	}
}
