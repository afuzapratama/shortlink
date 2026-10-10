package artifacts

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture() Manifest {
	m := Manifest{Version: "v0.9.0-test", PublishedAt: time.Now().UTC(), Artifacts: []File{}}
	for _, name := range Names {
		platform := "all"
		if strings.Contains(name, "amd64") {
			platform = "linux/amd64"
		}
		if strings.Contains(name, "arm64") {
			platform = "linux/arm64"
		}
		f := File{Name: name, Version: m.Version, Size: 3, SHA256: strings.Repeat("a", 64), Platform: platform, DownloadURL: Path(m.Version, name)}
		if strings.HasPrefix(name, "image-") {
			id := "sha256:" + strings.Repeat("b", 64)
			f.ImageID = &id
			f.ImageConfigDigest = &id
		}
		m.Artifacts = append(m.Artifacts, f)
	}
	return m
}
func sign(m Manifest, key ed25519.PrivateKey) []byte {
	raw, _ := json.Marshal(m)
	e := Envelope{Manifest: m, SignedPayload: base64.RawURLEncoding.EncodeToString(raw), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, raw))}
	b, _ := json.Marshal(e)
	return b
}
func TestSignedProjectionAndTampering(t *testing.T) {
	public, key, _ := ed25519.GenerateKey(rand.Reader)
	m := fixture()
	raw := sign(m, key)
	var outer Envelope
	json.Unmarshal(raw, &outer)
	outer.Version = "v99.0.0"
	outer.Artifacts[0].DownloadURL = "https://evil.example/token"
	raw, _ = json.Marshal(outer)
	e, err := Verify(raw, public)
	if err != nil || e.Version != m.Version || e.Artifacts[0].DownloadURL != Path(m.Version, m.Artifacts[0].Name) {
		t.Fatal(e, err)
	}
	outer.Signature = base64.RawURLEncoding.EncodeToString(make([]byte, 64))
	raw, _ = json.Marshal(outer)
	if _, err = Verify(raw, public); err == nil {
		t.Fatal("invalid signature accepted")
	}
}
func TestMetadataBoundariesAndChecksum(t *testing.T) {
	for _, version := range []string{"../secret", "latest", "v1.2.3/../../x"} {
		if ValidVersion(version) {
			t.Fatal(version)
		}
	}
	m := fixture()
	m.Artifacts[0].DownloadURL = "https://other.example/file"
	if Validate(m) == nil {
		t.Fatal("cross-origin URL accepted")
	}
	m = fixture()
	m.Artifacts[0].Platform = "linux/arm64"
	if Validate(m) == nil {
		t.Fatal("platform mismatch")
	}
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, []byte("abc"), 0600)
	sum, _, _ := Hash(strings.NewReader("abc"))
	meta := File{Size: 3, SHA256: sum}
	if err := CheckFile(file, meta); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(file, []byte("bad"), 0600)
	if CheckFile(file, meta) == nil {
		t.Fatal("same-size corruption accepted")
	}
}
