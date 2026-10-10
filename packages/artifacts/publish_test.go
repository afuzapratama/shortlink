package artifacts_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"shortlink.local/artifacts"
	"shortlink.local/artifacts/testutil"
	"testing"
	"time"
)

func TestImmutablePublishAndPlatformBinding(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	input, root := t.TempDir(), t.TempDir()
	if err := testutil.Build(input, "v0.9.0-test"); err != nil {
		t.Fatal(err)
	}
	e, err := artifacts.Publish(root, input, key.Seed(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	again, err := artifacts.Publish(root, input, key.Seed(), time.Now().Add(time.Hour))
	if err != nil || again.SignedPayload != e.SignedPayload {
		t.Fatal("republish changed immutable release", err)
	}
	metadataPath := filepath.Join(input, "build.json")
	original, _ := os.ReadFile(metadataPath)
	var build artifacts.Build
	json.Unmarshal(original, &build)
	build.InstallerReady = true
	changed, _ := json.Marshal(build)
	os.WriteFile(metadataPath, changed, 0600)
	if _, err = artifacts.Publish(root, input, key.Seed(), time.Now()); err == nil {
		t.Fatal("immutable installer capability changed")
	}
	os.WriteFile(metadataPath, original, 0600)
	os.WriteFile(filepath.Join(input, "INSTALL.md"), []byte("changed"), 0600)
	if _, err = artifacts.Publish(root, input, key.Seed(), time.Now()); err == nil {
		t.Fatal("version overwritten")
	}
	current, err := artifacts.Read(root, key.Public().(ed25519.PublicKey), "")
	if err != nil || current.SignedPayload != e.SignedPayload {
		t.Fatal("failed publish changed pointer", err)
	}
	if err = artifacts.InspectBinary(filepath.Join(input, "agent-linux-amd64"), "arm64"); err == nil {
		t.Fatal("wrong ELF platform accepted")
	}
	bad := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err = artifacts.InspectImage(filepath.Join(input, "image-linux-amd64.tar"), "amd64", bad, bad); err == nil {
		t.Fatal("wrong image ID accepted")
	}
}
