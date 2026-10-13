package runtime

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"shortlink.local/policy"
	"strings"
	"sync"
	"testing"
	"time"
)

type testCenter struct {
	manageKey     string
	manageSession string
	manageCSRF    string
	manageURL     string
	proxyCalls    int
	proxyDetected bool
	mu            sync.Mutex
	raw           []byte
	status        int
	heartbeat     int64
	etag          string
	private       ed25519.PrivateKey
}

func (c *testCenter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.Header.Get("Authorization") == "" {
		w.WriteHeader(401)
		return
	}
	if c.status != 0 {
		w.WriteHeader(c.status)
		return
	}
	if r.URL.Path == "/v1/agents/manage/exchange" {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["key"] != c.manageKey || c.manageKey == "" {
			w.WriteHeader(401)
			return
		}
		c.manageKey = ""
		json.NewEncoder(w).Encode(map[string]any{"session": c.manageSession, "expiresAt": time.Now().Add(15 * time.Minute)})
		return
	}
	if r.URL.Path == "/v1/agents/manage/session" {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["session"] != c.manageSession || c.manageSession == "" {
			w.WriteHeader(401)
			return
		}
		if body["action"] == "update" || body["action"] == "logout" {
			if body["csrf"] != c.manageCSRF {
				w.WriteHeader(403)
				return
			}
		}
		if body["action"] == "logout" {
			c.manageSession = ""
			json.NewEncoder(w).Encode(map[string]bool{"ok": true})
			return
		}
		if body["action"] == "update" {
			c.manageURL = body["openingUrl"].(string)
		}
		json.NewEncoder(w).Encode(map[string]any{"domain": map[string]any{"hostname": "go.example.test", "openingUrl": c.manageURL, "desiredRevision": 1}, "links": []map[string]any{{"id": "l", "slug": "hello", "destination": "https://example.org/main", "state": "published"}}, "csrf": c.manageCSRF, "nextCursor": ""})
		return
	}
	if r.URL.Path == "/v1/agents/proxycheck" {
		c.proxyCalls++
		json.NewEncoder(w).Encode(map[string]any{"status": "known", "detected": c.proxyDetected, "flags": policy.IPQualityFlags{Proxy: c.proxyDetected, Datacenter: true}, "expiresAt": time.Now().Add(time.Minute)})
		return
	}
	if r.URL.Path == "/v1/agents/heartbeat" {
		var body struct{ AppliedRevision int64 }
		json.NewDecoder(r.Body).Decode(&body)
		c.heartbeat = body.AppliedRevision
		w.Write([]byte(`{"ok":true}`))
		return
	}
	if r.Header.Get("If-None-Match") == c.etag && c.etag != "" {
		w.WriteHeader(304)
		return
	}
	w.Write(c.raw)
}
func fixture(revision int64) Snapshot {
	var s Snapshot
	s.Revision = revision
	s.LeaseExpiresAt = time.Now().Add(time.Hour).UTC()
	s.Domain.ID = "test-domain"
	s.Domain.Hostname = "go.example.test"
	s.Domain.Mode = "local"
	s.Links = []Link{{ID: "hello-link", DomainID: s.Domain.ID, Slug: "hello", Destination: "https://example.org/welcome?source=shortlink", State: "published"}}
	s.Rules = []policy.ScopedRule{}
	return s
}
func (c *testCenter) sign(s Snapshot) []byte {
	payload, _ := json.Marshal(s)
	raw, _ := json.Marshal(map[string]any{"signedPayload": base64.RawURLEncoding.EncodeToString(payload), "signature": base64.RawURLEncoding.EncodeToString(ed25519.Sign(c.private, payload)), "revision": 999, "links": []any{}})
	return raw
}
func testAgent(t *testing.T) (*Agent, *testCenter, func()) {
	t.Helper()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	secret := make([]byte, 32)
	rand.Read(secret)
	c := &testCenter{private: private}
	c.raw = c.sign(fixture(1))
	server := httptest.NewServer(c)
	cfg := Config{Listen: "127.0.0.1:8091", ControlURL: server.URL, DomainID: "test-domain", Hostname: "go.example.test", Credential: base64.RawURLEncoding.EncodeToString(secret), VerificationKey: base64.RawURLEncoding.EncodeToString(public), DataDir: filepath.Join(t.TempDir(), "agent"), Development: true}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a, c, server.Close
}
func redirect(t *testing.T, a *Agent, host, path string, code int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	r.Host = host
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != code {
		t.Fatalf("%s %s: got %d want %d", host, path, w.Code, code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cache enabled")
	}
	return w
}
func TestSignedLinkFallbackRoutesBotsAndAudienceLocally(t *testing.T) {
	a, c, closeServer := testAgent(t)
	defer closeServer()
	snapshot := fixture(2)
	snapshot.Links[0].Routing = &policy.LinkRouting{Fallback: "https://example.org/alternate", Country: "*", Device: "mobile", OS: "ios", Browser: "safari"}
	c.mu.Lock()
	c.raw = c.sign(snapshot)
	c.mu.Unlock()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ ua, target string }{
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", snapshot.Links[0].Destination},
		{"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/120.0 Mobile Safari/537.36", snapshot.Links[0].Routing.Fallback},
		{"Googlebot/2.1 (+http://www.google.com/bot.html)", snapshot.Links[0].Routing.Fallback},
	} {
		req := httptest.NewRequest("GET", "/hello", nil)
		req.Host = a.cfg.Hostname
		req.Header.Set("User-Agent", tc.ua)
		response := httptest.NewRecorder()
		a.ServeHTTP(response, req)
		if response.Code != 302 || response.Header().Get("Location") != tc.target {
			t.Fatalf("%s: %d %s", tc.ua, response.Code, response.Header().Get("Location"))
		}
	}
	snapshot.Revision = 3
	snapshot.Links[0].Routing.Fallback = "https://" + a.cfg.Hostname + "/hello"
	if _, err := a.verify(c.sign(snapshot), time.Now(), true); err == nil {
		t.Fatal("looping fallback activated")
	}
}

func TestLocalProxyLookupCacheAndOfflineUnknownFallback(t *testing.T) {
	a, c, closeServer := testAgent(t)
	snapshot := fixture(2)
	snapshot.Links[0].Routing = &policy.LinkRouting{Fallback: "https://example.org/proxy-fallback", Country: "*", Device: "*", OS: "*", Browser: "*", ProxyCheck: true, ProxyUnknownFallback: true}
	c.mu.Lock()
	c.raw = c.sign(snapshot)
	c.proxyDetected = true
	c.mu.Unlock()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	visit := func(want string) {
		t.Helper()
		r := httptest.NewRequest("GET", "/hello", nil)
		r.Host = a.cfg.Hostname
		r.RemoteAddr = "1.1.1.1:1234"
		r.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/120.0 Mobile Safari/537.36")
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != 302 || w.Header().Get("Location") != want {
			t.Fatalf("%d %s", w.Code, w.Header().Get("Location"))
		}
	}
	for i := 0; i < 3; i++ {
		visit(snapshot.Links[0].Routing.Fallback)
	}
	c.mu.Lock()
	calls := c.proxyCalls
	c.mu.Unlock()
	if calls != 1 {
		t.Fatal("agent did not cache", calls)
	}
	closeServer()
	a.proxy.mu.Lock()
	a.proxy.entries = map[string]proxyResult{}
	a.proxy.mu.Unlock()
	visit(snapshot.Links[0].Routing.Fallback)
}

func TestOpeningRootAndUnknownRedirectWithoutEvents(t *testing.T) {
	a, c, closeServer := testAgent(t)
	defer closeServer()
	snapshot := fixture(2)
	snapshot.Domain.OpeningURL = "https://example.org/opening"
	c.mu.Lock()
	c.raw = c.sign(snapshot)
	c.mu.Unlock()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/missing", "/unknown/extra"} {
		w := redirect(t, a, a.cfg.Hostname, path, 302)
		if w.Header().Get("Location") != snapshot.Domain.OpeningURL {
			t.Fatal(w.Header())
		}
	}
	redirect(t, a, a.cfg.Hostname, "/health/unknown", 404)
	redirect(t, a, a.cfg.Hostname, "/.well-known/unknown", 404)
	redirect(t, a, a.cfg.Hostname, "/__agent/setup", 404)
	snapshot.Revision = 3
	snapshot.Domain.OpeningURL = ""
	c.mu.Lock()
	c.raw = c.sign(snapshot)
	c.mu.Unlock()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	redirect(t, a, a.cfg.Hostname, "/", 503)
	redirect(t, a, a.cfg.Hostname, "/missing", 503)
	snapshot.Revision = 4
	snapshot.Domain.OpeningURL = "https://" + a.cfg.Hostname + "/loop"
	if _, err := a.verify(c.sign(snapshot), time.Now(), true); err == nil {
		t.Fatal("root loop accepted")
	}
}

func TestManageHTTPKeyCookieOriginAndLogout(t *testing.T) {
	a, c, closeServer := testAgent(t)
	defer closeServer()
	raw := make([]byte, 32)
	raw[0] = 1
	key := base64.RawURLEncoding.EncodeToString(raw)
	raw[0] = 2
	session := base64.RawURLEncoding.EncodeToString(raw)
	raw[0] = 3
	csrf := base64.RawURLEncoding.EncodeToString(raw)
	c.mu.Lock()
	c.manageKey = key
	c.manageSession = session
	c.manageCSRF = csrf
	c.mu.Unlock()
	call := func(method, body, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/__agent/manage", strings.NewReader(body))
		r.Host = a.cfg.Hostname + ":8091"
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		return w
	}
	origin := "http://" + a.cfg.Hostname + ":8091"
	if w := call("GET", "", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Key akses manage") {
		t.Fatal(w.Code)
	}
	if w := call("POST", "action=login&key="+key, "https://other.example", nil); w.Code != 403 {
		t.Fatal("foreign origin accepted")
	}
	w := call("POST", "action=login&key="+key, origin, nil)
	if w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe cookie")
	}
	cookie := cookies[0]
	w = call("GET", "", "", cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Shortlink domain ini") || strings.Contains(w.Body.String(), key) {
		t.Fatal(w.Code)
	}
	if w = call("POST", "action=update&expectedRevision=1&openingUrl=https%3A%2F%2Fexample.org%2Fopening&csrf=wrong", origin, cookie); w.Code != 403 {
		t.Fatal("CSRF accepted")
	}
	w = call("POST", "action=update&expectedRevision=1&openingUrl=https%3A%2F%2Fexample.org%2Fopening&csrf="+csrf, origin, cookie)
	if w.Code != 303 {
		t.Fatal(w.Code)
	}
	w = call("POST", "action=logout&csrf="+csrf, origin, cookie)
	if w.Code != 303 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout failed")
	}
}
func TestSyncRoutingPersistenceAndOutage(t *testing.T) {
	a, c, closeServer := testAgent(t)
	defer closeServer()
	redirect(t, a, a.cfg.Hostname, "/hello", 503)
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.heartbeat != 1 {
		t.Fatal("applied revision not acknowledged")
	}
	w := redirect(t, a, "GO.EXAMPLE.TEST.:8091", "/hello?injected=secret", 302)
	if w.Header().Get("Location") != "https://example.org/welcome?source=shortlink" {
		t.Fatal("destination changed or query forwarded")
	}
	redirect(t, a, "foreign.example.test", "/hello", 421)
	for _, path := range []string{"/missing", "/hello/extra", "/%68ello", "/../hello"} {
		redirect(t, a, a.cfg.Hostname, path, 503)
	}
	r := httptest.NewRequest("HEAD", "/hello", nil)
	r.Host = a.cfg.Hostname
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 302 || w.Body.Len() != 0 {
		t.Fatal("HEAD invalid")
	}
	a2, err := New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	redirect(t, a2, a.cfg.Hostname, "/hello", 302)
	c.mu.Lock()
	c.status = 503
	c.mu.Unlock()
	if a.Sync(context.Background()) == nil {
		t.Fatal("outage ignored")
	}
	redirect(t, a, a.cfg.Hostname, "/hello", 302)
	// An expired cache keeps its revision for rollback protection, but cannot redirect.
	s := fixture(2)
	s.LeaseExpiresAt = time.Now().Add(-time.Second)
	raw := c.sign(s)
	if err := atomicFile(a.cachePath, raw); err != nil {
		t.Fatal(err)
	}
	expired, err := New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer expired.Close()
	redirect(t, expired, a.cfg.Hostname, "/hello", 503)
}
func TestSignatureRevisionAndLeaseValidation(t *testing.T) {
	a, c, closeServer := testAgent(t)
	defer closeServer()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(*Snapshot)
	}{
		{"rollback", func(s *Snapshot) { s.Revision = 0 }},
		{"domain", func(s *Snapshot) { s.Domain.ID = "other" }},
		{"expired", func(s *Snapshot) { s.LeaseExpiresAt = time.Now().Add(-time.Second) }},
		{"same revision route", func(s *Snapshot) { s.Links[0].Destination = "https://other.example.org" }},
		{"loop", func(s *Snapshot) { s.Revision = 2; s.Links[0].Destination = "https://GO.EXAMPLE.TEST./hello" }},
		{"draft", func(s *Snapshot) { s.Revision = 2; s.Links[0].State = "draft" }},
		{"unsupported rules", func(s *Snapshot) { s.Revision = 2; s.Rules = []policy.ScopedRule{{}} }},
		{"duplicate", func(s *Snapshot) { s.Revision = 2; s.Links = append(s.Links, s.Links[0]) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := fixture(1)
			tt.change(&s)
			c.mu.Lock()
			c.raw = c.sign(s)
			c.mu.Unlock()
			if a.Sync(context.Background()) == nil {
				t.Fatal("invalid snapshot accepted")
			}
			redirect(t, a, a.cfg.Hostname, "/hello", 302)
		})
	}
	c.mu.Lock()
	c.raw = []byte(`{"signedPayload":"e30","signature":"invalid"}`)
	c.mu.Unlock()
	if a.Sync(context.Background()) == nil {
		t.Fatal("tampering accepted")
	}
	// Envelope duplicate fields are ignored; signature protects the actual payload.
	renewal := fixture(1)
	renewal.LeaseExpiresAt = a.current.Load().snapshot.LeaseExpiresAt.Add(time.Hour)
	c.mu.Lock()
	c.raw = c.sign(renewal)
	c.mu.Unlock()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !a.current.Load().snapshot.LeaseExpiresAt.Equal(renewal.LeaseExpiresAt) {
		t.Fatal("renewal missing")
	}
	c.mu.Lock()
	c.etag = a.current.Load().etag
	c.mu.Unlock()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	changed := fixture(2)
	changed.Links[0].Destination = "https://example.org/new"
	c.mu.Lock()
	c.etag = ""
	c.raw = c.sign(changed)
	c.mu.Unlock()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if redirect(t, a, a.cfg.Hostname, "/hello", 302).Header().Get("Location") != changed.Links[0].Destination {
		t.Fatal("new revision not applied")
	}
	lower := fixture(1)
	c.mu.Lock()
	c.raw = c.sign(lower)
	c.mu.Unlock()
	if a.Sync(context.Background()) == nil {
		t.Fatal("positive lower revision accepted")
	}
}
func TestRevokePersistsAcrossRestart(t *testing.T) {
	a, c, closeServer := testAgent(t)
	defer closeServer()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.status = 401
	c.mu.Unlock()
	if a.Sync(context.Background()) == nil {
		t.Fatal("revocation ignored")
	}
	redirect(t, a, a.cfg.Hostname, "/hello", 503)
	restarted, err := New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	redirect(t, restarted, a.cfg.Hostname, "/hello", 503)
	if restarted.Sync(context.Background()) == nil {
		t.Fatal("blocked credential retried")
	}
}
func TestTrustedProxyChain(t *testing.T) {
	a, _, closeServer := testAgent(t)
	defer closeServer()
	a.cfg.TrustedProxies = []string{"10.0.0.0/8", "2001:db8:1::/48"}
	var err error
	a, err = New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		peer, xff, want string
		invalid         bool
	}{
		{"198.51.100.2:80", "1.1.1.1", "198.51.100.2", false},
		{"10.1.1.1:80", "1.1.1.1, 198.51.100.2, 10.2.2.2", "198.51.100.2", false},
		{"[2001:db8:1::1]:80", "2001:db8:2::2, 2001:db8:1::2", "2001:db8:2::2", false},
		{"10.1.1.1:80", "broken", "", true},
		{"10.1.1.1:80", "", "10.1.1.1", false},
	}
	for _, tt := range cases {
		r := httptest.NewRequest("GET", "/hello", nil)
		r.RemoteAddr = tt.peer
		r.Header.Set("X-Forwarded-For", tt.xff)
		got, err := a.ClientIP(r)
		if (err != nil) != tt.invalid || (!tt.invalid && got.String() != tt.want) {
			t.Fatalf("proxy %s %s: %s %v", tt.peer, tt.xff, got, err)
		}
	}
}
func TestCacheTamperAndPrivatePermissions(t *testing.T) {
	a, _, closeServer := testAgent(t)
	defer closeServer()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(a.cachePath, []byte(`{}`), 0600)
	if _, err := New(a.cfg); err == nil {
		t.Fatal("corrupt cache accepted")
	}
	os.Chmod(a.cfg.DataDir, 0755)
	if _, err := New(a.cfg); err == nil {
		t.Fatal("public directory accepted")
	}
	cfg := a.cfg
	cfg.ControlURL = "http://external.example.org"
	if cfg.Validate() == nil {
		t.Fatal("external plaintext center accepted")
	}
	cfg = a.cfg
	cfg.TrustedProxies = []string{"bad-prefix"}
	if cfg.Validate() == nil {
		t.Fatal("invalid proxy accepted")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	raw, _ := json.Marshal(a.cfg)
	os.WriteFile(path, raw, 0644)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("public secrets accepted")
	}
	if strings.Contains(a.cfg.Credential, " ") {
		t.Fatal("unexpected credential")
	}
}
func TestConcurrentRedirectAndSync(t *testing.T) {
	a, _, closeServer := testAgent(t)
	defer closeServer()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				r := httptest.NewRequest("GET", "/hello", nil)
				r.Host = a.cfg.Hostname
				w := httptest.NewRecorder()
				a.ServeHTTP(w, r)
				if w.Code != 302 {
					t.Error("redirect failed")
				}
			}
		}()
	}
	for i := 0; i < 5; i++ {
		if err := a.Sync(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

func TestExpired304AndFailedPersistence(t *testing.T) {
	a, c, closeServer := testAgent(t)
	defer closeServer()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	old := a.current.Load()
	expired := *old
	expired.snapshot.LeaseExpiresAt = time.Now().Add(-time.Second)
	a.current.Store(&expired)
	c.mu.Lock()
	c.etag = expired.etag
	c.mu.Unlock()
	if a.Sync(context.Background()) == nil {
		t.Fatal("304 renewed expired lease")
	}
	redirect(t, a, a.cfg.Hostname, "/hello", 503)
	a.current.Store(old)
	next := fixture(2)
	next.Links[0].Slug = strings.Repeat("a", 80)
	c.mu.Lock()
	c.etag = ""
	c.raw = c.sign(next)
	c.mu.Unlock()
	a.cachePath = filepath.Join(a.cfg.DataDir, "missing-directory", "snapshot.json")
	if a.Sync(context.Background()) == nil {
		t.Fatal("failed persistence accepted")
	}
	if a.current.Load().snapshot.Revision != 1 {
		t.Fatal("unpersisted revision activated")
	}
	a.cachePath = filepath.Join(a.cfg.DataDir, "snapshot.json")
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	redirect(t, a, a.cfg.Hostname, "/"+strings.Repeat("a", 80), 302)
}

func TestGeoRequiredAndAssetRevocation(t *testing.T) {
	a, c, closeServer := testAgent(t)
	defer closeServer()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.cfg.GeoIPRequired = true
	redirect(t, a, a.cfg.Hostname, "/hello", 503)
	redirect(t, a, a.cfg.Hostname, "/health/ready", 503)
	a.cfg.GeoIPRequired = false
	redirect(t, a, a.cfg.Hostname, "/hello", 302)
	c.mu.Lock()
	c.status = 401
	c.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { a.RunGeo(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("asset credential rejection did not block agent")
	}
	if !a.blocked.Load() {
		t.Fatal("asset revocation ignored")
	}
	redirect(t, a, a.cfg.Hostname, "/hello", 503)
}

func TestLocalBEONDatacenterSelectionUsesRawCachedFlags(t *testing.T) {
	a, c, closeServer := testAgent(t)
	defer closeServer()
	snapshot := fixture(2)
	selected := []string{"datacenter"}
	snapshot.Links[0].Routing = &policy.LinkRouting{ProxyCheck: true, ProxyUnknownFallback: true, IPQualityBlock: &selected, Fallback: "https://example.org/ip-fallback", Country: "*", Device: "*", OS: "*", Browser: "*"}
	c.mu.Lock()
	c.raw = c.sign(snapshot)
	c.mu.Unlock()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	visit := func(want string) {
		t.Helper()
		r := httptest.NewRequest("GET", "/hello", nil)
		r.Host = a.cfg.Hostname
		r.RemoteAddr = "1.1.1.1:1234"
		r.Header.Set("User-Agent", "Mozilla/5.0 Chrome/120.0")
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != 302 || w.Header().Get("Location") != want {
			t.Fatalf("%d %s", w.Code, w.Header().Get("Location"))
		}
	}
	visit(snapshot.Links[0].Routing.Fallback)
	selected = []string{"proxy"}
	snapshot.Revision++
	c.mu.Lock()
	c.raw = c.sign(snapshot)
	c.mu.Unlock()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	visit(snapshot.Links[0].Destination)
	c.mu.Lock()
	calls := c.proxyCalls
	c.mu.Unlock()
	if calls != 1 {
		t.Fatal("raw IP cache was not shared", calls)
	}
}
