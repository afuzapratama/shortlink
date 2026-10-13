package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"shortlink.local/policy"
	"testing"
)

func TestLinkRoutingDraftPublishRollbackAndAPIReplay(t *testing.T) {
	s, db, d, _, l, credential := apiFixture(t)
	ctx := context.Background()
	cookie, csrf := login(t, s, nil)
	routing := policy.LinkRouting{Fallback: "https://example.org/fallback-one", Country: "ID", Device: "mobile", OS: "ios", Browser: "safari"}
	patch := func(value policy.LinkRouting, revision int64) *store.Link {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"domainId": d.ID, "slug": l.Slug, "destination": l.Destination, "routing": value, "expectedRevision": revision})
		w := request(s, "PATCH", "/v1/admin/links/"+l.ID, string(raw), testOrigin, csrf, cookie)
		status(t, w, 200)
		v := readJSON[store.Link](t, w)
		return &v
	}
	pending := patch(routing, l.Revision)
	if pending.Routing != nil || pending.DraftRouting == nil {
		t.Fatal("routing draft leaked into active link", pending)
	}
	if _, err := db.PublishLink(ctx, l.ID, pending.Revision, 0, "test"); err != nil {
		t.Fatal(err)
	}
	active, err := db.Link(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if active.Routing == nil || active.DraftRouting != nil || active.Routing.Fallback != routing.Fallback {
		t.Fatal(active)
	}
	firstRevision := active.Revision
	android := decisionBody(d.ID, "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/120.0 Mobile Safari/537.36")
	w := agentCall(s, "/v1/decisions", credential, android)
	status(t, w, 200)
	first := readJSON[policy.Decision](t, w)
	if first.Action != "fallback" || first.Destination == nil || *first.Destination != routing.Fallback {
		t.Fatal(first)
	}
	w = agentCall(s, "/v1/decisions", credential, decisionBody(d.ID, apiIphone))
	status(t, w, 200)
	if got := readJSON[policy.Decision](t, w); got.Action != "redirect" || got.Destination == nil || *got.Destination != l.Destination {
		t.Fatal(got)
	}
	w = agentCall(s, "/v1/decisions", credential, decisionBody(d.ID, "Googlebot/2.1 (+http://www.google.com/bot.html)"))
	status(t, w, 200)
	if got := readJSON[policy.Decision](t, w); got.Action != "fallback" {
		t.Fatal(got)
	}
	routing.Fallback = "https://example.org/fallback-two"
	pending = patch(routing, active.Revision)
	if _, err = db.PublishLink(ctx, l.ID, pending.Revision, 0, "test"); err != nil {
		t.Fatal(err)
	}
	w = agentCall(s, "/v1/decisions", credential, android)
	status(t, w, 200)
	if got := readJSON[policy.Decision](t, w); got.Destination == nil || *got.Destination != *first.Destination {
		t.Fatal("replay changed", got)
	}
	active, err = db.Link(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishLink(ctx, l.ID, active.Revision, firstRevision, "test"); err != nil {
		t.Fatal(err)
	}
	active, err = db.Link(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if active.Routing == nil || active.Routing.Fallback != *first.Destination || active.DraftRouting != nil {
		t.Fatal("rollback did not restore routing", active)
	}
	bad := routing
	bad.Fallback = "https://" + d.Hostname + "/loop"
	raw, _ := json.Marshal(map[string]any{"domainId": d.ID, "slug": l.Slug, "destination": l.Destination, "routing": bad, "expectedRevision": active.Revision})
	status(t, request(s, "PATCH", "/v1/admin/links/"+l.ID, string(raw), testOrigin, csrf, cookie), 400)
	var unique int
	if err = db.DB.QueryRow(ctx, "SELECT count(*) FROM event_keys WHERE agent_id=(SELECT id FROM agents WHERE domain_id=$1)", d.ID).Scan(&unique); err != nil {
		t.Fatal(err)
	}
	if unique != 3 {
		t.Fatalf("replay duplicated events: %d", unique)
	}
}

func TestLocalRoutingRequiresCapableAgent(t *testing.T) {
	_, db := setup(t)
	ctx := context.Background()
	d, err := db.CreateDomain(ctx, security.Token(), "routing.example.test", "local")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := db.Challenge(ctx, d.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Verify(ctx, d.ID, ch.Value, "test"); err != nil {
		t.Fatal(err)
	}
	tok, err := db.IssueToken(ctx, d.ID, "cli", "test")
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := db.Enroll(ctx, tok.Token, tok.InstallationID, security.Token(), d.Hostname, "cli", "legacy")
	if err != nil {
		t.Fatal(err)
	}
	routing := &policy.LinkRouting{Fallback: "https://example.org/fallback", Country: "*", Device: "*", OS: "*", Browser: "*"}
	l, err := db.CreateLinkRouting(ctx, security.Token(), d.ID, "audience", "https://example.org/main", routing, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishLink(ctx, l.ID, l.Revision, 0, "test"); !errors.Is(err, store.ErrRoutingCapability) {
		t.Fatalf("old agent accepted: %v", err)
	}
	d, err = db.Domain(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Heartbeat(ctx, a, d.DesiredRevision, "routing-dev", 0, 0, 0, nil, nil, store.QueueHealth{RoutingVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishLink(ctx, l.ID, l.Revision, 0, "test"); err != nil {
		t.Fatal(err)
	}
	d, err = db.Domain(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Heartbeat(ctx, a, d.DesiredRevision, "legacy", 0, 0, 0, nil, nil); !errors.Is(err, store.ErrRoutingCapability) {
		t.Fatalf("downgrade accepted: %v", err)
	}
}

func TestSelectedIPFlagsRequireRoutingFourAndPreventDowngrade(t *testing.T) {
	_, db := setup(t)
	ctx := context.Background()
	d, err := db.CreateDomain(ctx, security.Token(), "flags.example.test", "local")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := db.Challenge(ctx, d.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Verify(ctx, d.ID, ch.Value, "test"); err != nil {
		t.Fatal(err)
	}
	tok, err := db.IssueToken(ctx, d.ID, "cli", "test")
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := db.EnrollRouting(ctx, tok.Token, tok.InstallationID, security.Token(), d.Hostname, "cli", "routing-three", 3)
	if err != nil {
		t.Fatal(err)
	}
	plainRouting := &policy.LinkRouting{Fallback: "https://example.org/fallback", Country: "*", Device: "*", OS: "*", Browser: "*"}
	plain, err := db.CreateLinkRouting(ctx, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", d.ID, "plain", "https://example.org/plain", plainRouting, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishLink(ctx, plain.ID, plain.Revision, 0, "test"); err != nil {
		t.Fatal(err)
	}
	selected := []string{"datacenter"}
	routing := &policy.LinkRouting{ProxyCheck: true, IPQualityBlock: &selected, Fallback: "https://example.org/fallback", Country: "*", Device: "*", OS: "*", Browser: "*"}
	l, err := db.CreateLinkRouting(ctx, "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", d.ID, "flags", "https://example.org/main", routing, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishLink(ctx, l.ID, l.Revision, 0, "test"); !errors.Is(err, store.ErrRoutingCapability) {
		t.Fatal("routing v3 accepted", err)
	}
	d, err = db.Domain(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Heartbeat(ctx, a, d.DesiredRevision, "routing-four", 0, 0, 0, nil, nil, store.QueueHealth{RoutingVersion: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishLink(ctx, l.ID, l.Revision, 0, "test"); err != nil {
		t.Fatal(err)
	}
	d, err = db.Domain(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Heartbeat(ctx, a, d.DesiredRevision, "routing-three", 0, 0, 0, nil, nil, store.QueueHealth{RoutingVersion: 3}); !errors.Is(err, store.ErrRoutingCapability) {
		t.Fatal("downgrade accepted", err)
	}
	// Even if a low-capability record is present, snapshot validation must inspect
	// the later selected-flags link rather than stopping at the first plain link.
	if _, err = db.DB.Exec(ctx, "UPDATE agents SET routing_version=3 WHERE id=$1", a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpdateOpening(ctx, d.ID, "", d.DesiredRevision, "test"); !errors.Is(err, store.ErrRoutingCapability) {
		t.Fatal("later link capability ignored", err)
	}

}
