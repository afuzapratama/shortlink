package httpapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"shortlink.local/artifacts"
	"testing"
	"time"
)

func TestDownloadedPHPZIPWizardRealCentral(t *testing.T) {
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("PHP unavailable")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("Python unavailable")
	}
	s, db, d, a, _, _ := apiFixture(t)
	ctx := context.Background()
	if err := db.Revoke(ctx, a.ID, "test"); err != nil {
		t.Fatal(err)
	}
	// Deliberately isolated loopback hostname fixture; production validation/probe are unchanged.
	if _, err := db.DB.Exec(ctx, "UPDATE domains SET hostname='localhost' WHERE id=$1", d.ID); err != nil {
		t.Fatal(err)
	}
	tok, err := db.IssueToken(ctx, d.ID, "php_zip", "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(s)
	defer server.Close()
	s.cfg.PublicURL = server.URL
	s.cfg.PHPAssetRoot = t.TempDir()
	build := filepath.Join(t.TempDir(), "build")
	script, _ := filepath.Abs("../../../../scripts/build-php-release.py")
	if err = exec.Command("python3", script, "--output", build).Run(); err != nil {
		t.Fatal("actual PHP source build failed", err)
	}
	seed, err := db.Seed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = artifacts.PublishPHP(s.cfg.PHPAssetRoot, build, seed, time.Now()); err != nil {
		t.Fatal(err)
	}
	cookie, _ := login(t, s, nil)
	raw, _ := json.Marshal(map[string]any{"origin": server.URL, "cookie": cookie.String(), "key": base64.RawURLEncoding.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)), "token": tok.Token, "domainId": d.ID, "ua": apiIphone})
	harness, _ := filepath.Abs("../../../agent-php/tests/downloaded.py")
	cmd := exec.Command("python3", harness)
	cmd.Stdin = bytes.NewReader(raw)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal("downloaded ZIP wizard failed", err)
	}
	var result map[string]any
	if json.Unmarshal(output, &result) != nil || result["downloadVerified"] != true || result["wizardInstalled"] != true || result["setupLocked"] != true || result["domainId"] != d.ID {
		t.Fatal("PHP download/setup/redirect workflow failed")
	}
	var count int
	var source, country, osName string
	if err = db.DB.QueryRow(ctx, "SELECT count(*) FROM traffic_events WHERE domain_id=$1", d.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("wizard/probe generated visitor clicks", count, err)
	}
	if err = db.DB.QueryRow(ctx, "SELECT payload->>'source',payload->>'country',payload->>'os' FROM traffic_events WHERE domain_id=$1", d.ID).Scan(&source, &country, &osName); err != nil || source != "php" || country != "ID" || osName != "ios" {
		t.Fatal("downloaded adapter classification failed", err)
	}
}
