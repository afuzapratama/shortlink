package httpapi

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"shortlink.local/artifacts"
	"shortlink.local/artifacts/testutil"
	"shortlink.local/control/internal/security"
	"testing"
	"time"
)

func TestPHPDownloadsOwnerBindingIntegrityAndBootstrap(t *testing.T) {
	s, db := setup(t)
	cookie, _ := login(t, s, nil)
	s.cfg.PHPAssetRoot = t.TempDir()
	input := t.TempDir()
	seed, err := db.Seed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	version := "v0.16.0-b16"
	if err = testutil.PHPBuild(input, version); err != nil {
		t.Fatal(err)
	}
	if _, err = artifacts.PublishPHP(s.cfg.PHPAssetRoot, input, seed, time.Now()); err != nil {
		t.Fatal(err)
	}
	status(t, request(s, "GET", "/v1/assets/php/manifest", "", "", "", nil), 401)
	status(t, bearerPHPRequest(s), 401)
	w := request(s, "GET", "/v1/assets/php/manifest", "", "", "", cookie)
	status(t, w, 200)
	e, err := artifacts.VerifyPHP(w.Body.Bytes(), ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	if err != nil || e.Version != version {
		t.Fatal("signed PHP manifest", err)
	}
	for _, f := range e.Artifacts {
		w = request(s, "GET", f.DownloadURL, "", "", "", cookie)
		status(t, w, 200)
		sum, n, _ := artifacts.Hash(w.Body)
		if sum != f.SHA256 || n != f.Size {
			t.Fatal("PHP download checksum")
		}
		if w.Header().Get("X-Artifact-SHA256") != f.SHA256 {
			t.Fatal("checksum header missing")
		}
		status(t, request(s, "HEAD", f.DownloadURL, "", "", "", cookie), 200)
	}
	status(t, request(s, "GET", "/v1/assets/php/releases/"+version+"/config.json", "", "", "", cookie), 404)
	d, err := db.CreateDomain(context.Background(), security.Token(), "hosting.example.test", "api")
	if err != nil {
		t.Fatal(err)
	}
	w = request(s, "GET", "/v1/admin/domains/"+d.ID+"/bootstrap", "", "", "", cookie)
	status(t, w, 200)
	var b map[string]any
	json.Unmarshal(w.Body.Bytes(), &b)
	if b["artifactVersion"] != version || b["method"] != "php_zip" || b["publicUrl"] != "https://hosting.example.test" || b["developmentLoopback"] != false {
		t.Fatal("PHP bootstrap binding")
	}
	for _, key := range []string{"token", "credential", "installationProof"} {
		if _, ok := b[key]; ok {
			t.Fatal("bootstrap contains secret")
		}
	}
	if err = os.WriteFile(filepath.Join(s.cfg.PHPAssetRoot, "releases", version, "agent-php.zip"), []byte("tamper"), 0600); err != nil {
		t.Fatal(err)
	}
	status(t, request(s, "GET", artifacts.PHPPath(version, "agent-php.zip"), "", "", "", cookie), 503)
}

func bearerPHPRequest(s *Server) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/v1/assets/php/manifest", nil)
	r.Header.Set("Authorization", "Bearer "+security.Token())
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
