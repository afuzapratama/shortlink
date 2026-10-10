package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"shortlink.local/control/internal/config"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"shortlink.local/control/internal/testdb"
)

const testOrigin = "https://control.example.com"
const testPassword = "test owner password 123"

func setup(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	s := testdb.New(t)
	if err := s.Bootstrap(context.Background(), security.Token(), "owner@example.com", security.HashPassword(testPassword)); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Origin: testOrigin, SessionTTL: time.Hour}
	handler := New(cfg, s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(handler.Close)
	return handler, s
}
func request(s *Server, method, path, body, origin, csrf string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.10:4000"
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func status(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("response cacheable")
	}
}
func loginBody(email, password string) string {
	b, _ := json.Marshal(map[string]string{"email": email, "password": password})
	return string(b)
}
func login(t *testing.T, s *Server, old *http.Cookie) (*http.Cookie, string) {
	t.Helper()
	w := request(s, "POST", "/v1/auth/login", loginBody("owner@example.com", testPassword), testOrigin, "", old)
	status(t, w, 200)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("session cookie missing")
	}
	return cookies[0], w.Header().Get("X-CSRF-Token")
}
func TestSessionLifecycle(t *testing.T) {
	s, db := setup(t)
	status(t, request(s, "GET", "/health/live", "", "", "", nil), 200)
	status(t, request(s, "GET", "/health/ready", "", "", "", nil), 200)
	status(t, request(s, "GET", "/v1/auth/session", "", "", "", nil), 401)
	for _, path := range []string{"/v1/admin/domains", "/v1/admin/links", "/v1/admin/events", "/v1/admin/rule-sets", "/v1/admin/future-operation"} {
		status(t, request(s, "GET", path, "", "", "", nil), 401)
		status(t, request(s, "POST", path, "{}", testOrigin, "", nil), 401)
	}
	c, csrf := login(t, s, nil)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.MaxAge != 3600 || c.Domain != "" {
		t.Fatalf("unsafe session cookie: %+v", c)
	}
	if csrf == "" || csrf == c.Value {
		t.Fatal("CSRF missing or same as bearer")
	}
	// Handler restart uses the durable session; it does not depend on in-memory session state.
	restarted := New(s.cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w := request(restarted, "GET", "/v1/auth/session", "", "", "", c)
	status(t, w, 200)
	var session map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session["email"] != "owner@example.com" || session["role"] != "owner" || session["csrfToken"] != csrf {
		t.Fatal("incorrect session projection", session)
	}
	status(t, request(s, "GET", "/v1/admin/domains", "", "", "", c), 200)
	status(t, request(s, "POST", "/v1/admin/domains", "{}", testOrigin, "", c), 403)
	status(t, request(s, "POST", "/v1/admin/domains", "{}", testOrigin, csrf, c), 400)
	for _, path := range []string{"/v1/agents/config", "/v1/decisions"} {
		status(t, request(s, "GET", path, "", "", "", c), 401)
	}
	// GeoLite assets also accept an owner session; absent configuration remains unavailable.
	status(t, request(s, "GET", "/v1/assets/geoip/manifest", "", "", "", c), 503)
	status(t, request(s, "POST", "/v1/auth/logout", "", testOrigin, "", c), 403)
	status(t, request(s, "POST", "/v1/auth/logout", "", "https://evil.example", csrf, c), 403)
	newer, newCSRF := login(t, s, c)
	if c.Value == newer.Value || csrf == newCSRF {
		t.Fatal("login did not rotate session")
	}
	status(t, request(s, "GET", "/v1/auth/session", "", "", "", c), 401)
	status(t, request(s, "POST", "/v1/auth/logout", "", testOrigin, csrf, newer), 403)
	w = request(s, "POST", "/v1/auth/logout", "", testOrigin, newCSRF, newer)
	status(t, w, 200)
	if w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not expire browser cookie")
	}
	status(t, request(s, "GET", "/v1/auth/session", "", "", "", newer), 401)
	c, _ = login(t, s, nil)
	if _, err := db.DB.Exec(context.Background(), "UPDATE sessions SET expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	status(t, request(s, "GET", "/v1/auth/session", "", "", "", c), 401)
}
func TestCredentialAndRequestBoundaries(t *testing.T) {
	s, db := setup(t)
	body := loginBody("owner@example.com", testPassword)
	for _, origin := range []string{"", "https://evil.example", testOrigin + "/"} {
		status(t, request(s, "POST", "/v1/auth/login", body, origin, "", nil), 403)
	}
	w := request(s, "POST", "/v1/auth/login", loginBody("owner@example.com", "wrong password 123"), testOrigin, "", nil)
	status(t, w, 401)
	other := request(s, "POST", "/v1/auth/login", loginBody("unknown@example.com", "wrong password 123"), testOrigin, "", nil)
	status(t, other, 401)
	var a, b struct {
		Error struct{ Code, Message string }
	}
	json.Unmarshal(w.Body.Bytes(), &a)
	json.Unmarshal(other.Body.Bytes(), &b)
	if a != b {
		t.Fatal("unknown account reveals different error")
	}
	forged := &http.Cookie{Name: cookieName, Value: security.Token()}
	status(t, request(s, "GET", "/v1/auth/session", "", "", "", forged), 401)
	for _, bad := range []string{"{", body + " {}", `{"email":"owner@example.com","password":"test owner password 123","admin":true}`} {
		status(t, request(s, "POST", "/v1/auth/login", bad, testOrigin, "", nil), 400)
	}
	status(t, request(s, "POST", "/v1/auth/login", strings.Repeat("x", 9000), testOrigin, "", nil), 400)
	oversized := `{"email":"` + strings.Repeat("x", 9000) + `","password":"longpassword123"}`
	status(t, request(s, "POST", "/v1/auth/login", oversized, testOrigin, "", nil), 413)
	status(t, request(s, "GET", "/v1/auth/login", "", "", "", nil), 405)
	status(t, request(s, "GET", "/unknown", "", "", "", nil), 404)
	r := httptest.NewRequest("POST", "/v1/auth/login", strings.NewReader(body))
	r.Header.Set("Origin", testOrigin)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	status(t, w, 415)
	r = httptest.NewRequest("POST", "/v1/auth/login", strings.NewReader(body))
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	status(t, w, 403)
	c, _ := login(t, s, nil)
	if _, err := db.DB.Exec(context.Background(), "UPDATE owners SET mfa_enabled=true"); err != nil {
		t.Fatal(err)
	}
	status(t, request(s, "GET", "/v1/auth/session", "", "", "", c), 401)
	status(t, request(s, "POST", "/v1/auth/login", body, testOrigin, "", nil), 200)
	var logs bytes.Buffer
	s.logger = slog.New(slog.NewTextHandler(&logs, nil))
	request(s, "GET", "/v1/auth/session?secret=private-query", "", "", "", c)
	if strings.Contains(logs.String(), c.Value) || strings.Contains(logs.String(), "private-query") || strings.Contains(logs.String(), testPassword) {
		t.Fatal("credentials or query leaked into request logs")
	}
}
func TestDurableRateLimitsIgnoreForwardedIP(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if _, err := db.AllowLogin(ctx, security.Digest("email:owner@example.com"), 10); err != nil {
			t.Fatal(err)
		}
	}
	status(t, request(s, "POST", "/v1/auth/login", loginBody("owner@example.com", testPassword), testOrigin, "", nil), 429)
	if _, err := db.DB.Exec(ctx, "TRUNCATE auth_buckets"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if _, err := db.AllowLogin(ctx, security.Digest("ip:192.0.2.10"), 30); err != nil {
			t.Fatal(err)
		}
	}
	restarted := New(s.cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := httptest.NewRequest("POST", "/v1/auth/login", strings.NewReader(loginBody("owner@example.com", testPassword)))
	r.RemoteAddr = "192.0.2.10:4000"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("X-Forwarded-For", "198.51.100.77")
	w := httptest.NewRecorder()
	restarted.ServeHTTP(w, r)
	status(t, w, 429)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("rate limit has no retry hint")
	}
	if _, err := db.DB.Exec(ctx, "UPDATE auth_buckets SET window_started=now()-interval '16 minutes'"); err != nil {
		t.Fatal(err)
	}
	login(t, s, nil)
}

func TestDatabaseFailureDoesNotAuthorize(t *testing.T) {
	s, db := setup(t)
	cookie, _ := login(t, s, nil)
	db.Close()
	for _, w := range []*httptest.ResponseRecorder{
		request(s, "GET", "/health/ready", "", "", "", nil),
		request(s, "GET", "/v1/auth/session", "", "", "", cookie),
		request(s, "POST", "/v1/auth/login", loginBody("owner@example.com", testPassword), testOrigin, "", nil),
	} {
		status(t, w, 503)
		var payload struct {
			Error struct{ Code, Message, RequestID string }
		}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Error.Code != "control_unavailable" || payload.Error.RequestID != w.Header().Get("X-Request-ID") {
			t.Fatal("invalid error envelope")
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("database failure issued a cookie")
		}
	}
	status(t, request(s, "GET", "/health/live", "", "", "", nil), 200)
}
