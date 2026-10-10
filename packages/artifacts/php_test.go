package artifacts_test

import (
	"archive/zip"
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

func TestPHPReleaseImmutableSignedAndTamper(t *testing.T) {
	seed := make([]byte, 32)
	rand.Read(seed)
	key := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	root, input := t.TempDir(), t.TempDir()
	version := "v0.16.0-b16"
	if err := testutil.PHPBuild(input, version); err != nil {
		t.Fatal(err)
	}
	e, err := artifacts.PublishPHP(root, input, seed, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(e)
	if _, err = artifacts.VerifyPHP(raw, key); err != nil {
		t.Fatal(err)
	}
	// Mirrored outer fields are not authoritative.
	var mirror map[string]any
	json.Unmarshal(raw, &mirror)
	mirror["version"] = "v9.9.9"
	changed, _ := json.Marshal(mirror)
	v, err := artifacts.VerifyPHP(changed, key)
	if err != nil || v.Version != version {
		t.Fatal("unsigned mirror trusted")
	}
	repeated, err := artifacts.PublishPHP(root, input, seed, time.Now().Add(time.Hour))
	if err != nil || repeated.Signature != e.Signature {
		t.Fatal("immutable republish changed", err)
	}
	mirror["signature"] = "tampered"
	changed, _ = json.Marshal(mirror)
	if _, err = artifacts.VerifyPHP(changed, key); err == nil {
		t.Fatal("tampered signature accepted")
	}
	if err = os.WriteFile(filepath.Join(root, "releases", version, "INSTALL.md"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = artifacts.PublishPHP(root, input, seed, time.Now()); err == nil {
		t.Fatal("corrupted immutable release reactivated")
	}
}
func TestPHPZIPRejectsUnsafeEntries(t *testing.T) {
	for _, name := range []string{"../secret", "private/config.json", "private/pending.json", "tests/router.php"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bad.zip")
			f, _ := os.Create(path)
			z := zip.NewWriter(f)
			w, _ := z.Create(name)
			w.Write([]byte("secret"))
			z.Close()
			f.Close()
			if _, err := artifacts.InspectPHPZIP(path, "v0.16.0-b16"); err == nil {
				t.Fatal("unsafe ZIP accepted")
			}
		})
	}
}
