package httpapi

import (
	"context"
	"encoding/json"
	"shortlink.local/control/internal/security"
	"shortlink.local/policy"
	"testing"
	"time"
)

func TestLinkLifecyclePublishedVersionAndDraftIsolation(t *testing.T) {
	s, db, d, _, l, cred := apiFixture(t)
	ctx := context.Background()
	cookie, csrf := login(t, s, nil)
	history, err := db.History(ctx, l.ID, true)
	if err != nil || len(history) != 1 || history[0].Destination == nil || *history[0].Destination != l.Destination {
		t.Fatal("history destination missing", err)
	}
	original := l.Revision
	if l.PublishedRevision == nil || *l.PublishedRevision != original {
		t.Fatal("published version missing")
	}
	draft, err := db.UpdateLink(ctx, l.ID, d.ID, l.Slug, "https://example.org/new", l.Revision, "test")
	if err != nil {
		t.Fatal(err)
	}
	if *draft.PublishedRevision != original {
		t.Fatal("draft changed active version")
	}
	change := func(disabled bool, expected int64, want int) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"disabled": disabled, "expectedRevision": expected})
		status(t, request(s, "PATCH", "/v1/admin/links/"+l.ID+"/status", string(raw), testOrigin, csrf, cookie), want)
	}
	change(true, l.Revision, 409)
	change(true, draft.Revision, 200)
	disabled, err := db.Link(ctx, l.ID)
	if err != nil || !disabled.Disabled || disabled.DraftDestination == nil || *disabled.PublishedRevision != original {
		t.Fatal(disabled, err)
	}
	status(t, agentCall(s, "/v1/decisions", cred, decisionBody(d.ID, apiIphone)), 404)
	change(false, disabled.Revision, 200)
	active, err := db.Link(ctx, l.ID)
	if err != nil || active.Disabled || active.Destination != l.Destination || active.DraftDestination == nil || *active.PublishedRevision != original {
		t.Fatal(active, err)
	}
	status(t, agentCall(s, "/v1/decisions", cred, decisionBody(d.ID, apiIphone)), 200)
	if _, err = db.PublishLink(ctx, l.ID, active.Revision, 0, "test"); err != nil {
		t.Fatal(err)
	}
	published, _ := db.Link(ctx, l.ID)
	if *published.PublishedRevision != published.Revision || published.Destination != "https://example.org/new" {
		t.Fatal(published)
	}
	if _, err = db.PublishLink(ctx, l.ID, published.Revision, original, "test"); err != nil {
		t.Fatal(err)
	}
	restored, _ := db.Link(ctx, l.ID)
	if restored.Destination != l.Destination || *restored.PublishedRevision != restored.Revision {
		t.Fatal(restored)
	}
	raw, _ := json.Marshal(map[string]any{"slug": "wrong", "expectedRevision": restored.Revision})
	status(t, request(s, "DELETE", "/v1/admin/links/"+l.ID, string(raw), testOrigin, csrf, cookie), 409)
	raw, _ = json.Marshal(map[string]any{"slug": l.Slug, "expectedRevision": restored.Revision})
	status(t, request(s, "DELETE", "/v1/admin/links/"+l.ID, string(raw), testOrigin, "", cookie), 403)
	status(t, request(s, "DELETE", "/v1/admin/links/"+l.ID, string(raw), testOrigin, csrf, cookie), 200)
	status(t, request(s, "GET", "/v1/admin/links/"+l.ID, "", "", "", cookie), 404)
	status(t, agentCall(s, "/v1/decisions", cred, decisionBody(d.ID, apiIphone)), 404)
	replacement, err := db.CreateLink(ctx, security.Token(), d.ID, l.Slug, l.Destination, "test")
	if err != nil || replacement.ID == l.ID {
		t.Fatal(err)
	}
}
func TestDeletedLinkStillAcceptsQueuedGoEvents(t *testing.T) {
	_, db := setup(t)
	ctx := context.Background()
	d, err := db.CreateDomain(ctx, security.Token(), "late-events.example.test", "local")
	if err != nil {
		t.Fatal(err)
	}
	ch, _ := db.Challenge(ctx, d.ID, "test")
	if err = db.Verify(ctx, d.ID, ch.Value, "test"); err != nil {
		t.Fatal(err)
	}
	tok, _ := db.IssueToken(ctx, d.ID, "cli", "test")
	a, _, err := db.Enroll(ctx, tok.Token, tok.InstallationID, security.Token(), d.Hostname, "cli", "test")
	if err != nil {
		t.Fatal(err)
	}
	l, err := db.CreateLink(ctx, security.Token(), d.ID, "late", "https://example.org/", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishLink(ctx, l.ID, l.Revision, 0, "test"); err != nil {
		t.Fatal(err)
	}
	l, _ = db.Link(ctx, l.ID)
	d, _ = db.Domain(ctx, d.ID)
	if _, err = db.ChangeLinkLifecycle(ctx, l.ID, l.Revision, l.Slug, nil, "test"); err != nil {
		t.Fatal(err)
	}
	event := policy.TrafficEvent{Decision: policy.Decision{EventID: security.Token(), ConfigRevision: d.DesiredRevision, Action: "redirect", ReasonCode: "link_destination", Destination: &l.Destination}, OccurredAt: time.Now(), DomainID: d.ID, LinkID: l.ID, Source: "go", Country: "ID", Device: "desktop", OS: "linux", Browser: "firefox"}
	accepted, _, err := db.IngestEvents(ctx, a, []policy.TrafficEvent{event})
	if err != nil || accepted != 1 {
		t.Fatal("late queued event rejected", accepted, err)
	}
	listed, err := db.Links(ctx, "", d.ID, 100)
	if err != nil || len(listed) != 0 {
		t.Fatal("deleted link listed", listed, err)
	}
}
