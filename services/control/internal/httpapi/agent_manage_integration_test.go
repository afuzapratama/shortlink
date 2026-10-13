package httpapi

import (
	"context"
	"encoding/json"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"testing"
	"time"
)

func TestDomainOpeningManageKeysScopeCSRFAndNoClicks(t *testing.T) {
	s, db, d, a, l, credential := apiFixture(t)
	ctx := context.Background()
	cookie, csrf := login(t, s, nil)
	endpoint := "/v1/admin/domains/" + d.ID
	body := func(v any) string { raw, _ := json.Marshal(v); return string(raw) }
	status(t, request(s, "PATCH", endpoint+"/opening", body(map[string]any{"openingUrl": "https://example.org/opening", "expectedRevision": d.DesiredRevision}), testOrigin, "", cookie), 403)
	w := request(s, "PATCH", endpoint+"/opening", body(map[string]any{"openingUrl": "https://example.org/opening", "expectedRevision": d.DesiredRevision}), testOrigin, csrf, cookie)
	status(t, w, 200)
	d = readJSON[store.Domain](t, w)
	if d.OpeningURL != "https://example.org/opening" {
		t.Fatal(d)
	}
	w = agentCall(s, "/v1/agents/opening", credential, map[string]string{"slug": "missing"})
	status(t, w, 200)
	result := readJSON[struct {
		OpeningURL string
		Published  bool
	}](t, w)
	if result.Published || result.OpeningURL != d.OpeningURL {
		t.Fatal(result)
	}
	w = agentCall(s, "/v1/agents/opening", credential, map[string]string{"slug": l.Slug})
	status(t, w, 200)
	if !readJSON[struct{ Published bool }](t, w).Published {
		t.Fatal("published link hidden")
	}
	w = request(s, "POST", endpoint+"/manage-key", "", testOrigin, csrf, cookie)
	status(t, w, 200)
	key := readJSON[store.ManageKey](t, w)
	w = agentCall(s, "/v1/agents/manage/exchange", credential, map[string]string{"key": key.Key})
	status(t, w, 200)
	session := readJSON[store.ManageSession](t, w)
	status(t, agentCall(s, "/v1/agents/manage/exchange", credential, map[string]string{"key": key.Key}), 401)
	other, err := db.CreateDomain(ctx, security.Token(), "other.example.test", "api")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.CreateLink(ctx, security.Token(), other.ID, "private-other", "https://example.org/other"); err != nil {
		t.Fatal(err)
	}
	w = agentCall(s, "/v1/agents/manage/session", credential, map[string]string{"action": "view", "session": session.Session})
	status(t, w, 200)
	view := readJSON[struct {
		Domain store.Domain
		Links  []store.Link
	}](t, w)
	if view.Domain.ID != d.ID || len(view.Links) != 1 || view.Links[0].ID != l.ID {
		t.Fatal("cross domain data", view)
	}
	if err = db.AuthorizeManage(ctx, store.Agent{ID: "other", DomainID: other.ID}, session.Session); err == nil {
		t.Fatal("session shared across agents")
	}
	status(t, agentCall(s, "/v1/agents/manage/session", credential, map[string]any{"action": "update", "session": session.Session, "openingUrl": "https://example.org/changed", "expectedRevision": d.DesiredRevision}), 403)
	w = agentCall(s, "/v1/agents/manage/session", credential, map[string]any{"action": "update", "session": session.Session, "csrf": session.CSRF, "openingUrl": "https://example.org/changed", "expectedRevision": d.DesiredRevision})
	status(t, w, 200)
	status(t, agentCall(s, "/v1/agents/manage/session", credential, map[string]any{"action": "update", "session": session.Session, "csrf": session.CSRF, "openingUrl": "https://" + d.Hostname + "/loop", "expectedRevision": d.DesiredRevision + 1}), 422)
	status(t, agentCall(s, "/v1/agents/manage/session", credential, map[string]string{"action": "logout", "session": session.Session, "csrf": session.CSRF}), 200)
	status(t, agentCall(s, "/v1/agents/manage/session", credential, map[string]string{"action": "view", "session": session.Session}), 401)
	var events int
	if err = db.DB.QueryRow(ctx, "SELECT count(*) FROM event_keys WHERE agent_id=$1", a.ID).Scan(&events); err != nil || events != 0 {
		t.Fatal("manage/root counted as clicks", events, err)
	}
	current, err := db.Domain(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	status(t, request(s, "PATCH", endpoint+"/opening", body(map[string]any{"openingUrl": "", "expectedRevision": current.DesiredRevision}), testOrigin, csrf, cookie), 200)
	newKey, err := db.IssueManageKey(ctx, d.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec(ctx, "UPDATE agent_manage_keys SET expires_at=$1 WHERE key_hash=$2", time.Now().Add(-time.Minute), security.Digest(newKey.Key)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExchangeManageKey(ctx, a, newKey.Key); err == nil {
		t.Fatal("expired key accepted")
	}
}
