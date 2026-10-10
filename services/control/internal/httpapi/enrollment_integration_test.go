package httpapi

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
)

func TestEnrollmentVerificationRecoveryAndRevocation(t *testing.T) {
	s, db := setup(t)
	c, csrf := login(t, s, nil)
	ctx := context.Background()
	d, err := db.CreateDomain(ctx, security.Token(), "go.domainanda.com", "local")
	if err != nil {
		t.Fatal(err)
	}
	issue := fmt.Sprintf(`{"domainId":%q,"method":"cli"}`, d.ID)
	status(t, request(s, "POST", "/v1/admin/install-tokens", issue, testOrigin, csrf, c), 422)
	w := request(s, "POST", "/v1/admin/domains/"+d.ID+"/verification", "", testOrigin, csrf, c)
	status(t, w, 200)
	challenge := readJSON[store.Challenge](t, w)
	s.lookupTXT = func(context.Context, string) ([]string, error) { return []string{"wrong-value"}, nil }
	w = request(s, "POST", "/v1/admin/domains/"+d.ID+"/verify", "", testOrigin, csrf, c)
	status(t, w, 200)
	var failed struct{ Readiness string }
	json.Unmarshal(w.Body.Bytes(), &failed)
	if failed.Readiness != "verification_pending" {
		t.Fatal("wrong TXT verified domain")
	}
	s.lookupTXT = func(_ context.Context, name string) ([]string, error) {
		if name != challenge.Name {
			t.Error("DNS lookup outside bound hostname")
		}
		return []string{challenge.Value}, nil
	}
	status(t, request(s, "POST", "/v1/admin/domains/"+d.ID+"/verify", "", testOrigin, csrf, c), 200)
	status(t, request(s, "POST", "/v1/admin/install-tokens", fmt.Sprintf(`{"domainId":%q,"method":"php_zip"}`, d.ID), testOrigin, csrf, c), 422)
	w = request(s, "POST", "/v1/admin/install-tokens", issue, testOrigin, csrf, c)
	status(t, w, 201)
	old := readJSON[store.InstallToken](t, w)
	w = request(s, "POST", "/v1/admin/install-tokens", issue, testOrigin, csrf, c)
	status(t, w, 201)
	token := readJSON[store.InstallToken](t, w)
	contextBody := fmt.Sprintf(`{"token":%q}`, token.Token)
	w = request(s, "POST", "/v1/agents/install-context", contextBody, "", "", nil)
	status(t, w, 200)
	installContext := readJSON[struct {
		InstallationID string
		Bootstrap      struct{ DomainID, Hostname string }
	}](t, w)
	if installContext.InstallationID != token.InstallationID || installContext.Bootstrap.DomainID != d.ID || installContext.Bootstrap.Hostname != d.Hostname || strings.Contains(w.Body.String(), `"credential"`) {
		t.Fatal("context lost binding or exposed credential")
	}
	status(t, request(s, "POST", "/v1/agents/install-context", fmt.Sprintf(`{"token":%q}`, old.Token), "", "", nil), 409)
	proof := security.Token()
	body := func(token store.InstallToken, hostname, method, proof string) string {
		return fmt.Sprintf(`{"token":%q,"installationId":%q,"installationProof":%q,"hostname":%q,"method":%q,"version":"test-1"}`, token.Token, token.InstallationID, proof, hostname, method)
	}
	status(t, request(s, "POST", "/v1/agents/enroll", body(old, d.Hostname, "cli", proof), "", "", nil), 409)
	status(t, request(s, "POST", "/v1/agents/enroll", body(token, "wrong.domainanda.com", "cli", proof), "", "", nil), 422)
	status(t, request(s, "POST", "/v1/agents/enroll", body(token, d.Hostname, "php_zip", proof), "", "", nil), 422)
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- request(s, "POST", "/v1/agents/enroll", body(token, d.Hostname, "cli", proof), "", "", nil)
		}()
	}
	wg.Wait()
	close(results)
	var enrolled struct {
		AgentID, Credential, ConfigURL string
		Bootstrap                      struct{ ManifestVerificationKey string }
	}
	for w := range results {
		status(t, w, 200)
		v := readJSON[struct {
			AgentID, Credential, ConfigURL string
			Bootstrap                      struct{ ManifestVerificationKey string }
		}](t, w)
		if enrolled.AgentID != "" && (v.AgentID != enrolled.AgentID || v.Credential != enrolled.Credential) {
			t.Fatal("retry created new agent or credential")
		}
		enrolled = v
	}
	status(t, request(s, "POST", "/v1/agents/enroll", body(token, d.Hostname, "cli", security.Token()), "", "", nil), 409)
	d, err = db.Domain(ctx, d.ID)
	if err != nil || d.Readiness != "preparing" || d.DesiredRevision != 1 || d.AppliedRevision != nil {
		t.Fatal("enrollment marked domain ready or applied", d, err)
	}
	agentRequest := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+enrolled.Credential)
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w = agentRequest("GET", "/v1/agents/config", "")
	status(t, w, 200)
	etag := w.Header().Get("ETag")
	snapshot := readJSON[map[string]any](t, w)
	payload, err := base64.RawURLEncoding.DecodeString(snapshot["signedPayload"].(string))
	if err != nil {
		t.Fatal(err)
	}
	signature, _ := base64.RawURLEncoding.DecodeString(snapshot["signature"].(string))
	key, _ := base64.RawURLEncoding.DecodeString(enrolled.Bootstrap.ManifestVerificationKey)
	if !ed25519.Verify(key, payload, signature) {
		t.Fatal("snapshot signature invalid")
	}
	payload[0] ^= 1
	if ed25519.Verify(key, payload, signature) {
		t.Fatal("tampered snapshot verified")
	}
	r := httptest.NewRequest("GET", "/v1/agents/config", nil)
	r.Header.Set("Authorization", "Bearer "+enrolled.Credential)
	r.Header.Set("If-None-Match", etag)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	status(t, w, 304)
	if w.Body.Len() != 0 {
		t.Fatal("304 returned a body")
	}
	if _, err = db.DB.Exec(ctx, "UPDATE snapshots SET lease_expires_at=now()-interval '1 hour' WHERE domain_id=$1", d.ID); err != nil {
		t.Fatal(err)
	}
	w = agentRequest("GET", "/v1/agents/config", "")
	status(t, w, 200)
	if w.Header().Get("ETag") == etag {
		t.Fatal("lease renewal did not replace signed snapshot")
	}
	heartbeat := `{"version":"test-1","appliedRevision":1,"geoRelease":null,"feedVersion":null,"queueDepth":0,"queueBytes":0,"droppedEvents":0}`
	status(t, agentRequest("POST", "/v1/agents/heartbeat", heartbeat), 200)
	d, _ = db.Domain(ctx, d.ID)
	if d.AppliedRevision == nil || *d.AppliedRevision != 1 || d.Readiness == "ready" {
		t.Fatal("heartbeat acknowledgement wrong", d)
	}
	status(t, agentRequest("POST", "/v1/agents/heartbeat", strings.Replace(heartbeat, `"appliedRevision":1`, `"appliedRevision":0`, 1)), 409)
	status(t, agentRequest("POST", "/v1/agents/heartbeat", strings.Replace(heartbeat, `"appliedRevision":1`, `"appliedRevision":2`, 1)), 422)
	if _, err = db.DB.Exec(ctx, "UPDATE install_tokens SET used_at=now()-interval '11 minutes' WHERE token_hash=$1", security.Digest(token.Token)); err != nil {
		t.Fatal(err)
	}
	status(t, request(s, "POST", "/v1/agents/enroll", body(token, d.Hostname, "cli", proof), "", "", nil), 409)
	status(t, request(s, "POST", "/v1/admin/agents/"+enrolled.AgentID+"/revoke", "", testOrigin, csrf, c), 200)
	status(t, agentRequest("GET", "/v1/agents/config", ""), 401)
	status(t, request(s, "POST", "/v1/agents/enroll", body(token, d.Hostname, "cli", proof), "", "", nil), 409)
	w = request(s, "GET", "/v1/admin/domains/"+d.ID+"/audit", "", "", "", c)
	status(t, w, 200)
	if strings.Contains(w.Body.String(), enrolled.Credential) || strings.Contains(w.Body.String(), token.Token) || strings.Contains(w.Body.String(), proof) {
		t.Fatal("audit leaks credentials")
	}
	var count int
	db.DB.QueryRow(ctx, "SELECT count(*) FROM agents WHERE domain_id=$1", d.ID).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate agent created")
	}
	w = request(s, "POST", "/v1/admin/install-tokens", issue, testOrigin, csrf, c)
	status(t, w, 201)
	replacement := readJSON[store.InstallToken](t, w)
	if _, err = db.DB.Exec(ctx, "UPDATE install_tokens SET expires_at=$2 WHERE token_hash=$1", security.Digest(replacement.Token), time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	status(t, request(s, "POST", "/v1/agents/enroll", body(replacement, d.Hostname, "cli", security.Token()), "", "", nil), 410)
}
