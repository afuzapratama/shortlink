package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"shortlink.local/policy"
	"testing"
)

func TestPolicyPublishAtomicRollbackAndAuthorization(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	cookie, csrf := login(t, s, nil)
	domain, err := db.CreateDomain(ctx, security.Token(), "policy.domainanda.com", "local")
	if err != nil {
		t.Fatal(err)
	}
	l, err := db.CreateLink(ctx, security.Token(), domain.ID, "policy", "https://destination.com/original")
	if err != nil {
		t.Fatal(err)
	}
	status(t, request(s, "GET", "/v1/admin/rule-sets", "", "", "", nil), 401)
	body := fmt.Sprintf(`{"scope":"link","scopeId":%q}`, l.ID)
	status(t, request(s, "POST", "/v1/admin/rule-sets", body, testOrigin, "", cookie), 403)
	w := request(s, "POST", "/v1/admin/rule-sets", body, testOrigin, csrf, cookie)
	status(t, w, 201)
	set := readJSON[store.RuleSet](t, w)
	rule := policy.Rule{ID: "rule-test", Name: "iPhone", Priority: 10, Country: "ID", Device: "mobile", OS: "ios", Browser: "safari", Risk: "*", Action: "block", Enabled: true}
	patch := func(rules []policy.Rule, expected int64) string {
		b, _ := json.Marshal(map[string]any{"expectedRevision": expected, "rules": rules})
		return string(b)
	}
	w = request(s, "PATCH", "/v1/admin/rule-sets/"+set.ID, patch([]policy.Rule{rule}, 0), testOrigin, csrf, cookie)
	status(t, w, 200)
	set = readJSON[store.RuleSet](t, w)
	status(t, request(s, "PATCH", "/v1/admin/rule-sets/"+set.ID, patch([]policy.Rule{rule}, 0), testOrigin, csrf, cookie), 409)
	status(t, request(s, "PATCH", "/v1/admin/rule-sets/"+set.ID, patch([]policy.Rule{rule, rule}, 1), testOrigin, csrf, cookie), 422)
	sim := fmt.Sprintf(`{"linkId":%q,"country":"ID","device":"mobile","os":"ios","browser":"safari","knownBot":false}`, l.ID)
	w = request(s, "POST", "/v1/admin/rules/simulate", sim, testOrigin, csrf, cookie)
	status(t, w, 200)
	if readJSON[policy.Decision](t, w).Action != "redirect" {
		t.Fatal("draft leaked into live rules")
	}
	preview := sim[:len(sim)-1] + fmt.Sprintf(`,"ruleSetId":%q,"expectedRevision":1}`, set.ID)
	w = request(s, "POST", "/v1/admin/rules/simulate", preview, testOrigin, csrf, cookie)
	status(t, w, 200)
	if readJSON[policy.Decision](t, w).Action != "block" {
		t.Fatal("draft preview wrong")
	}
	path := "/v1/admin/rule-sets/" + set.ID
	w = request(s, "POST", path+"/publish", `{"expectedRevision":1}`, testOrigin, csrf, cookie)
	status(t, w, 200)
	pub := readJSON[store.PublishResult](t, w)
	if pub.Revision != 2 || len(pub.DomainRevisions) != 1 {
		t.Fatal(pub)
	}
	w = request(s, "POST", "/v1/admin/rules/simulate", sim, testOrigin, csrf, cookie)
	status(t, w, 200)
	if readJSON[policy.Decision](t, w).Action != "block" {
		t.Fatal("publish not visible")
	}
	// Bot detection has not shipped: failed publish must not change active policy or desired revision.
	rule.Risk = "known_bot"
	rule.Action = "fallback"
	rule.DestinationSource = "custom"
	rule.Destination = "https://destination.com/alternate"
	w = request(s, "PATCH", path, patch([]policy.Rule{rule}, 2), testOrigin, csrf, cookie)
	status(t, w, 200)
	before, _ := db.Domain(ctx, domain.ID)
	status(t, request(s, "POST", path+"/publish", `{"expectedRevision":3}`, testOrigin, csrf, cookie), 422)
	after, _ := db.Domain(ctx, domain.ID)
	if before.DesiredRevision != after.DesiredRevision {
		t.Fatal("failed publish changed snapshot revision")
	}
	status(t, request(s, "POST", path+"/rollback", `{"expectedRevision":3,"targetRevision":2}`, testOrigin, csrf, cookie), 200)
	restored, _ := db.RuleSet(ctx, set.ID)
	if restored.Revision != 4 || restored.Rules[0].Risk != "*" || restored.Rules[0].DestinationSource != "" || restored.Rules[0].Destination != "" {
		t.Fatal(restored)
	}
	status(t, request(s, "GET", path+"/history", "", "", "", cookie), 200)
	status(t, request(s, "POST", "/v1/admin/visitors/detect", `{"userAgent":""}`, testOrigin, csrf, cookie), 200)
	w = request(s, "GET", "/v1/admin/countries", "", "", "", cookie)
	status(t, w, 200)
	countries := readJSON[struct{ Items []string }](t, w)
	if len(countries.Items) < 240 {
		t.Fatal("incomplete ISO countries", len(countries.Items))
	}
}
