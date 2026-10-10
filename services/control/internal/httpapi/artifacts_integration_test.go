package httpapi

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"shortlink.local/artifacts"
	"shortlink.local/artifacts/testutil"
	"shortlink.local/control/internal/security"
	"testing"
	"time"
)

func TestPrivateAgentReleaseAndBootstrap(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	cookie, csrf := login(t, s, nil)
	seed, err := db.Seed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	input := t.TempDir()
	s.cfg.AgentAssetRoot = t.TempDir()
	s.cfg.PublicURL = "https://api.example.com"
	if err = testutil.Build(input, "v0.9.0-test"); err != nil {
		t.Fatal(err)
	}
	release, err := artifacts.Publish(s.cfg.AgentAssetRoot, input, seed, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	status(t, request(s, "GET", "/v1/assets/agent/manifest", "", "", "", nil), 401)
	w := request(s, "GET", "/v1/assets/agent/manifest", "", "", "", cookie)
	status(t, w, 200)
	e := readJSON[artifacts.Envelope](t, w)
	if e.Version != release.Version {
		t.Fatal(e)
	}
	file := e.Artifacts[0]
	w = request(s, "GET", file.DownloadURL, "", "", "", cookie)
	status(t, w, 200)
	if int64(w.Body.Len()) != file.Size || w.Header().Get("X-Artifact-SHA256") != file.SHA256 {
		t.Fatal("download metadata mismatch")
	}
	for _, path := range []string{"/v1/assets/agent/releases/v0.9.0-test/build.json", "/v1/assets/agent/releases/v0.9.0-test/unknown", "/v1/assets/agent/releases/v0.9.0-test/../secret"} {
		status(t, request(s, "GET", path, "", "", "", cookie), 404)
	}
	r := httptest.NewRequest("HEAD", file.DownloadURL, nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	status(t, w, 200)
	if w.Body.Len() != 0 {
		t.Fatal("HEAD returned bytes")
	}
	d, err := db.CreateDomain(ctx, security.Token(), "release.domainanda.com", "local")
	if err != nil {
		t.Fatal(err)
	}
	w = request(s, "GET", "/v1/admin/domains/"+d.ID+"/bootstrap", "", "", "", cookie)
	status(t, w, 200)
	b := readJSON[map[string]any](t, w)
	if b["artifactVersion"] != release.Version || b["controlPlaneUrl"] != s.cfg.PublicURL {
		t.Fatal(b)
	}
	ch, err := db.Challenge(ctx, d.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Verify(ctx, d.ID, ch.Value, "test"); err != nil {
		t.Fatal(err)
	}
	token, err := db.IssueToken(ctx, d.ID, "cli", "test")
	if err != nil {
		t.Fatal(err)
	}
	a, credential, err := db.Enroll(ctx, token.Token, token.InstallationID, security.Token(), d.Hostname, "cli", "test")
	if err != nil {
		t.Fatal(err)
	}
	agentRequest := func(header string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", file.DownloadURL, nil)
		r.Header.Set("Authorization", "Bearer "+header)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	status(t, agentRequest(token.Token), 401)
	status(t, agentRequest(credential), 200)
	status(t, request(s, "POST", "/v1/admin/install-tokens", `{"domainId":"missing","method":"cli"}`, s.cfg.Origin, csrf, cookie), 404)
	if err = db.Revoke(ctx, a.ID, "test"); err != nil {
		t.Fatal(err)
	}
	status(t, agentRequest(credential), 401)
	path := filepath.Join(s.cfg.AgentAssetRoot, "releases", e.Version, file.Name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 1
	os.WriteFile(path, raw, 0600)
	status(t, request(s, "GET", file.DownloadURL, "", "", "", cookie), 503)
}
