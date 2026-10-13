package httpapi

import (
	"context"
	"io"
	"net/http"
	"shortlink.local/control/internal/proxycheck"
	"shortlink.local/policy"
	"strings"
	"testing"
)

type proxyTransport func(*http.Request) (*http.Response, error)

func (f proxyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestProxyKeyAuthorizationEncryptionBudgetAndRouting(t *testing.T) {
	s, db, d, _, l, credential := apiFixture(t)
	ctx := context.Background()
	cookie, csrf := login(t, s, nil)
	endpoint := "/v1/admin/system/proxycheck"
	status(t, request(s, "GET", endpoint, "", "", "", nil), 401)
	status(t, request(s, "PATCH", endpoint, `{"expectedRevision":0,"enabled":true,"baseUrl":"https://ipq.example.com","apiKey":"test-private-key"}`, testOrigin, "", cookie), 403)
	status(t, request(s, "PATCH", endpoint, `{"expectedRevision":0,"enabled":true,"baseUrl":"https://ipq.example.com","apiKey":"test-private-key"}`, testOrigin, csrf, cookie), 200)
	got := request(s, "GET", endpoint, "", "", "", cookie)
	status(t, got, 200)
	if strings.Contains(got.Body.String(), "test-private-key") || strings.Contains(got.Body.String(), "apiKey") {
		t.Fatal("key returned")
	}
	var sealed []byte
	if err := db.DB.QueryRow(ctx, "SELECT api_key FROM proxycheck_settings").Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "test-private-key") {
		t.Fatal("key stored plaintext")
	}
	cfg, err := db.ProxyConfig(ctx)
	if err != nil || cfg.Key != "test-private-key" {
		t.Fatal("encrypted key not recoverable", err)
	}
	s.proxy = proxycheck.New(db.ProxyConfig, db.ReserveProxyQuery, &http.Client{Transport: proxyTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ip":"1.1.1.1","proxy":true,"vpn":false,"tor":false,"botnet":false,"datacenter":false,"spam":false,"malware":false,"attacker":false,"data_version":"generation"}`))}, nil
	})})
	routing := &policy.LinkRouting{Fallback: "https://example.org/proxy-fallback", Country: "*", Device: "*", OS: "*", Browser: "*", ProxyCheck: true, ProxyUnknownFallback: true}
	pending, err := db.UpdateLinkRouting(ctx, l.ID, d.ID, l.Slug, l.Destination, l.Revision, routing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishLink(ctx, l.ID, pending.Revision, 0, "test"); err != nil {
		t.Fatal(err)
	}
	w := agentCall(s, "/v1/decisions", credential, decisionBody(d.ID, apiIphone))
	status(t, w, 200)
	decision := readJSON[policy.Decision](t, w)
	if decision.Action != "fallback" || decision.Proxy == nil || !decision.Proxy.Detected || *decision.Destination != routing.Fallback {
		t.Fatal(decision)
	}
	w = request(s, "POST", endpoint+"/test", `{"ip":"1.1.1.1"}`, testOrigin, csrf, cookie)
	status(t, w, 200)
	if r := readJSON[proxycheck.Result](t, w); r.Status != "known" || !r.Detected {
		t.Fatal(r)
	}
	settings, err := db.ProxySettings(ctx)
	if err != nil || settings.QueriesToday != 1 {
		t.Fatal(settings, err)
	}
	if ok, err := db.ReserveProxyQuery(ctx, settings.Revision); !ok || err != nil {
		t.Fatal("unexpected daily limit", ok, err)
	}
	status(t, request(s, "PATCH", endpoint, `{"expectedRevision":0,"enabled":false,"dailyBudget":1}`, testOrigin, csrf, cookie), 409)
	status(t, request(s, "PATCH", endpoint, `{"expectedRevision":1,"enabled":false,"apiKey":"replacement-test-key"}`, testOrigin, csrf, cookie), 200)
	cfg, err = db.ProxyConfig(ctx)
	if err != nil || cfg.Enabled {
		t.Fatal(err)
	}
	status(t, request(s, "PATCH", endpoint, `{"expectedRevision":2,"enabled":false,"removeKey":true}`, testOrigin, csrf, cookie), 200)
	settings, err = db.ProxySettings(ctx)
	if err != nil || settings.HasKey {
		t.Fatal(settings, err)
	}
	status(t, request(s, "POST", endpoint+"/test", `{"ip":"127.0.0.1"}`, testOrigin, csrf, cookie), 400)
}

func TestBEONSettingsRecipientRotationAndSelectedFlagsFallback(t *testing.T) {
	s, db, d, _, l, credential := apiFixture(t)
	ctx := context.Background()
	cookie, csrf := login(t, s, nil)
	endpoint := "/v1/admin/system/proxycheck"
	status(t, request(s, "PATCH", endpoint, `{"expectedRevision":0,"enabled":true,"provider":"beon","baseUrl":"https://ipq.example.com/","apiKey":"beon-test-key"}`, testOrigin, csrf, cookie), 200)
	cfg, err := db.ProxyConfig(ctx)
	if err != nil || cfg.Provider != "beon" || cfg.BaseURL != "https://ipq.example.com" || cfg.Key != "beon-test-key" {
		t.Fatal("configuration not stored", err)
	}
	status(t, request(s, "PATCH", endpoint, `{"expectedRevision":1,"enabled":true,"provider":"beon","baseUrl":"https://other.example.com"}`, testOrigin, csrf, cookie), 422)
	status(t, request(s, "PATCH", endpoint, `{"expectedRevision":1,"enabled":true,"provider":"beon","baseUrl":"http://ipq.example.com","apiKey":"new-key"}`, testOrigin, csrf, cookie), 422)
	status(t, request(s, "PATCH", endpoint, `{"expectedRevision":1,"enabled":true,"provider":"beon","baseUrl":"https://other.example.com","apiKey":"new-key"}`, testOrigin, csrf, cookie), 200)
	// More queries than any saved daily budget must not block BEON lookups.
	if _, err = db.DB.Exec(ctx, `INSERT INTO proxycheck_usage(day,queries) VALUES((now() AT TIME ZONE 'UTC')::date,1000000) ON CONFLICT(day) DO UPDATE SET queries=1000000`); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ReserveProxyQuery(ctx, 2); !ok || err != nil {
		t.Fatal("BEON daily cap applied", err)
	}
	s.proxy = proxycheck.New(db.ProxyConfig, db.ReserveProxyQuery, &http.Client{Transport: proxyTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "other.example.com" || r.Header.Get("X-API-Key") != "new-key" {
			t.Fatal("wrong key recipient")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ip":"1.1.1.1","proxy":false,"vpn":false,"tor":false,"botnet":false,"spam":false,"malware":false,"attacker":false,"datacenter":true,"data_version":"generation"}`))}, nil
	})})
	selected := []string{"datacenter"}
	routing := &policy.LinkRouting{IPQualityBlock: &selected, Fallback: "https://example.org/beon-fallback", Country: "*", Device: "*", OS: "*", Browser: "*", ProxyCheck: true, ProxyUnknownFallback: true}
	pending, err := db.UpdateLinkRouting(ctx, l.ID, d.ID, l.Slug, l.Destination, l.Revision, routing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishLink(ctx, l.ID, pending.Revision, 0, "test"); err != nil {
		t.Fatal(err)
	}
	w := agentCall(s, "/v1/decisions", credential, decisionBody(d.ID, apiIphone))
	status(t, w, 200)
	decision := readJSON[policy.Decision](t, w)
	if decision.Action != "fallback" || decision.Proxy == nil || !decision.Proxy.Detected || *decision.Destination != routing.Fallback {
		t.Fatal(decision)
	}
	status(t, request(s, "POST", "/v1/admin/rules/simulate", `{"linkId":"`+l.ID+`","country":"ID","device":"mobile","os":"ios","browser":"safari","proxyStatus":"known","ipQuality":{"proxy":false,"vpn":false,"tor":false,"datacenter":true,"botnet":false,"spam":false,"malware":false,"attacker":false}}`, testOrigin, csrf, cookie), 200)
	got := request(s, "GET", endpoint, "", "", "", cookie)
	status(t, got, 200)
	if strings.Contains(got.Body.String(), "new-key") {
		t.Fatal("key exposed")
	}
}
