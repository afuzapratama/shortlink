package artifacts

import (
	"archive/zip"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var PHPNames = []string{"agent-php.zip", "INSTALL.md"}

type PHPManifest struct {
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"publishedAt"`
	Method      string    `json:"method"`
	MinimumPHP  string    `json:"minimumPhp"`
	Artifacts   []File    `json:"artifacts"`
}
type PHPEnvelope struct {
	PHPManifest
	Signature     string `json:"signature"`
	SignedPayload string `json:"signedPayload"`
}

func PHPPath(version, name string) string { return "/v1/assets/php/releases/" + version + "/" + name }
func ValidPHPName(name string) bool       { return name == "agent-php.zip" || name == "INSTALL.md" }
func ValidatePHP(m PHPManifest) error {
	if !ValidVersion(m.Version) || m.PublishedAt.IsZero() || m.Method != "php_zip" || m.MinimumPHP != "8.2" || len(m.Artifacts) != 2 {
		return errors.New("invalid PHP release")
	}
	seen := map[string]bool{}
	for _, f := range m.Artifacts {
		if !ValidPHPName(f.Name) || seen[f.Name] || f.Version != m.Version || f.Platform != "php" || f.Size <= 0 || f.Size > 4<<20 || !hashPattern.MatchString(f.SHA256) || f.DownloadURL != PHPPath(m.Version, f.Name) || f.ImageID != nil || f.ImageDigest != nil || f.ImageReference != nil || f.ImageConfigDigest != nil {
			return errors.New("invalid PHP artifact metadata")
		}
		seen[f.Name] = true
	}
	return nil
}
func VerifyPHP(raw []byte, key ed25519.PublicKey) (PHPEnvelope, error) {
	var e PHPEnvelope
	if len(raw) > 64<<10 || json.Unmarshal(raw, &e) != nil {
		return e, errors.New("invalid PHP envelope")
	}
	payload, err := base64.RawURLEncoding.DecodeString(e.SignedPayload)
	if err != nil {
		return e, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(e.Signature)
	if err != nil || !ed25519.Verify(key, payload, signature) {
		return e, errors.New("PHP signature rejected")
	}
	var m PHPManifest
	if json.Unmarshal(payload, &m) != nil {
		return e, errors.New("invalid PHP payload")
	}
	if err = ValidatePHP(m); err != nil {
		return e, err
	}
	return PHPEnvelope{m, e.Signature, e.SignedPayload}, nil
}
func ReadPHP(root string, key ed25519.PublicKey, version string) (PHPEnvelope, error) {
	if root == "" {
		return PHPEnvelope{}, errors.New("PHP root missing")
	}
	file := filepath.Join(root, "current.json")
	if version != "" {
		if !ValidVersion(version) {
			return PHPEnvelope{}, errors.New("invalid PHP version")
		}
		file = filepath.Join(root, "releases", version, "manifest.json")
	}
	raw, err := readBound(file, 64<<10)
	if err != nil {
		return PHPEnvelope{}, err
	}
	e, err := VerifyPHP(raw, key)
	if err == nil && version != "" && e.Version != version {
		return e, errors.New("PHP version mismatch")
	}
	return e, err
}

// InspectPHPZIP rejects active config, tests, traversal, symlinks, duplicate entries and zip bombs.
// Release authors remain trusted to review source; signatures do not certify arbitrary PHP code.
func InspectPHPZIP(path, version string) ([]byte, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	allowed := map[string]bool{"public/": true, "private/": true, "private/src/": true, "public/index.php": true, "public/setup.css": true, "public/.htaccess": true, "private/src/Adapter.php": true, "private/src/Setup.php": true, "private/bootstrap.example.json": true, "INSTALL.md": true}
	seen := map[string]bool{}
	var tutorial []byte
	var total uint64
	for _, f := range z.File {
		if !allowed[f.Name] || seen[f.Name] || f.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("unsafe PHP ZIP entry")
		}
		seen[f.Name] = true
		if strings.HasSuffix(f.Name, "/") {
			if !f.Mode().IsDir() {
				return nil, errors.New("invalid ZIP directory")
			}
			continue
		}
		if !f.Mode().IsRegular() || f.UncompressedSize64 > 1<<20 {
			return nil, errors.New("invalid ZIP file")
		}
		total += f.UncompressedSize64
		if total > 2<<20 {
			return nil, errors.New("PHP ZIP too large")
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(r, 1<<20+1))
		r.Close()
		if err != nil || len(raw) > 1<<20 {
			return nil, errors.New("invalid ZIP payload")
		}
		if f.Name == "INSTALL.md" {
			tutorial = raw
		}
		if f.Name == "private/src/Adapter.php" && !strings.Contains(string(raw), "const VERSION = '"+version+"';") {
			return nil, errors.New("PHP source/version mismatch")
		}
		if f.Name == "private/bootstrap.example.json" {
			var b map[string]json.RawMessage
			if json.Unmarshal(raw, &b) != nil || len(b) != 5 || string(b["developmentLoopback"]) != "false" {
				return nil, errors.New("unsafe PHP bootstrap")
			}
			for _, key := range []string{"hostname", "publicUrl", "controlPlaneUrl", "trustedProxies", "developmentLoopback"} {
				if _, ok := b[key]; !ok {
					return nil, errors.New("invalid PHP bootstrap")
				}
			}
		}
	}
	for _, name := range []string{"public/index.php", "public/setup.css", "public/.htaccess", "private/src/Adapter.php", "private/src/Setup.php", "private/bootstrap.example.json", "INSTALL.md"} {
		if !seen[name] {
			return nil, errors.New("incomplete PHP ZIP")
		}
	}
	if len(tutorial) == 0 {
		return nil, errors.New("PHP tutorial missing")
	}
	return tutorial, nil
}
func PublishPHP(root, input string, seed []byte, now time.Time) (PHPEnvelope, error) {
	if len(seed) != ed25519.SeedSize || root == "" || input == "" || filepath.Clean(root) == filepath.Clean(input) {
		return PHPEnvelope{}, errors.New("separate PHP root/input and seed required")
	}
	if err := os.MkdirAll(filepath.Join(root, "releases"), 0700); err != nil {
		return PHPEnvelope{}, err
	}
	lock, err := os.OpenFile(filepath.Join(root, ".publish-lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return PHPEnvelope{}, err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return PHPEnvelope{}, errors.New("PHP publisher busy")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	raw, err := readBound(filepath.Join(input, "manifest.json"), 16<<10)
	if err != nil {
		return PHPEnvelope{}, err
	}
	var build struct {
		Version    string `json:"version"`
		Method     string `json:"method"`
		MinimumPHP string `json:"minimumPhp"`
		Filename   string `json:"filename"`
		Size       int64  `json:"size"`
		SHA256     string `json:"sha256"`
	}
	if json.Unmarshal(raw, &build) != nil || !ValidVersion(build.Version) || build.Method != "php_zip" || build.MinimumPHP != "8.2" || build.Filename != "agent-php-"+build.Version+".zip" || build.Size <= 0 || build.Size > 4<<20 || !hashPattern.MatchString(build.SHA256) {
		return PHPEnvelope{}, errors.New("invalid PHP build")
	}
	zipPath := filepath.Join(input, build.Filename)
	if err = CheckFile(zipPath, File{Size: build.Size, SHA256: build.SHA256}); err != nil {
		return PHPEnvelope{}, err
	}
	tutorial, err := InspectPHPZIP(zipPath, build.Version)
	if err != nil {
		return PHPEnvelope{}, err
	}
	zipRaw, err := readBound(zipPath, 4<<20)
	if err != nil {
		return PHPEnvelope{}, err
	}
	if sum, size, _ := Hash(strings.NewReader(string(zipRaw))); sum != build.SHA256 || size != build.Size {
		return PHPEnvelope{}, errors.New("PHP build changed during publication")
	}
	files := map[string][]byte{"agent-php.zip": zipRaw, "INSTALL.md": tutorial}
	key := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	existing, err := ReadPHP(root, key, build.Version)
	if err == nil {
		for _, f := range existing.Artifacts {
			sum, size, _ := Hash(strings.NewReader(string(files[f.Name])))
			if sum != f.SHA256 || size != f.Size || CheckFile(filepath.Join(root, "releases", existing.Version, f.Name), f) != nil {
				return PHPEnvelope{}, errors.New("immutable PHP release changed")
			}
		}
		out, _ := json.Marshal(existing)
		return existing, write(filepath.Join(root, "current.json"), out)
	}
	if !os.IsNotExist(err) {
		return PHPEnvelope{}, err
	}
	temp, err := os.MkdirTemp(filepath.Join(root, "releases"), ".candidate-")
	if err != nil {
		return PHPEnvelope{}, err
	}
	defer os.RemoveAll(temp)
	m := PHPManifest{Version: build.Version, PublishedAt: now.UTC(), Method: "php_zip", MinimumPHP: "8.2", Artifacts: []File{}}
	for _, name := range PHPNames {
		raw := files[name]
		if err = write(filepath.Join(temp, name), raw); err != nil {
			return PHPEnvelope{}, err
		}
		sum, size, _ := Hash(strings.NewReader(string(raw)))
		m.Artifacts = append(m.Artifacts, File{Name: name, Version: m.Version, Platform: "php", Size: size, SHA256: sum, DownloadURL: PHPPath(m.Version, name)})
	}
	if err = ValidatePHP(m); err != nil {
		return PHPEnvelope{}, err
	}
	payload, _ := json.Marshal(m)
	e := PHPEnvelope{m, base64.RawURLEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed), payload)), base64.RawURLEncoding.EncodeToString(payload)}
	out, _ := json.Marshal(e)
	if err = write(filepath.Join(temp, "manifest.json"), out); err != nil {
		return e, err
	}
	if err = os.Rename(temp, filepath.Join(root, "releases", m.Version)); err != nil {
		return e, err
	}
	if err = syncDir(filepath.Join(root, "releases")); err != nil {
		return e, err
	}
	return e, write(filepath.Join(root, "current.json"), out)
}
