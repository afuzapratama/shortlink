// Package artifacts defines signed, immutable private agent releases.
package artifacts

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[a-z0-9.-]+)?$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidVersion(v string) bool { return len(v) <= 64 && versionPattern.MatchString(v) }

var Names = []string{"agent-linux-amd64", "agent-linux-arm64", "image-linux-amd64.tar", "image-linux-arm64.tar", "compose.agent.yml", "INSTALL.md"}

func ValidName(name string) bool {
	for _, n := range Names {
		if n == name {
			return true
		}
	}
	return false
}

type File struct {
	Name              string  `json:"name"`
	Version           string  `json:"version"`
	Platform          string  `json:"platform"`
	Size              int64   `json:"size"`
	SHA256            string  `json:"sha256"`
	DownloadURL       string  `json:"downloadUrl"`
	ImageConfigDigest *string `json:"imageConfigDigest"`
	ImageID           *string `json:"imageId"`
	ImageReference    *string `json:"imageReference"`
	ImageDigest       *string `json:"imageDigest"`
}
type Manifest struct {
	Version        string    `json:"version"`
	PublishedAt    time.Time `json:"publishedAt"`
	InstallerReady bool      `json:"installerReady"`
	Artifacts      []File    `json:"artifacts"`
}
type Envelope struct {
	Manifest
	Signature     string `json:"signature"`
	SignedPayload string `json:"signedPayload"`
}
type Build struct {
	Version        string           `json:"version"`
	InstallerReady bool             `json:"installerReady"`
	Images         map[string]Image `json:"images"`
}
type Image struct {
	ConfigDigest string  `json:"configDigest"`
	ID           string  `json:"id"`
	Reference    *string `json:"reference"`
	Digest       *string `json:"digest"`
}

func Path(version, name string) string { return "/v1/assets/agent/releases/" + version + "/" + name }
func Validate(m Manifest) error {
	if !ValidVersion(m.Version) || m.PublishedAt.IsZero() || len(m.Artifacts) != len(Names) {
		return errors.New("invalid release or installer capability")
	}
	seen := map[string]bool{}
	for _, f := range m.Artifacts {
		if !ValidName(f.Name) || seen[f.Name] || f.Version != m.Version || f.Size <= 0 || f.Size > 512<<20 || !hashPattern.MatchString(f.SHA256) || f.DownloadURL != Path(m.Version, f.Name) {
			return errors.New("invalid artifact metadata")
		}
		seen[f.Name] = true
		platform := "all"
		if strings.Contains(f.Name, "amd64") {
			platform = "linux/amd64"
		}
		if strings.Contains(f.Name, "arm64") {
			platform = "linux/arm64"
		}
		if f.Platform != platform {
			return errors.New("platform mismatch")
		}
		image := strings.HasPrefix(f.Name, "image-")
		if image {
			if f.ImageID == nil || !ValidDigest(*f.ImageID) || f.ImageConfigDigest == nil || !ValidDigest(*f.ImageConfigDigest) {
				return errors.New("invalid image config ID")
			}
			if (f.ImageReference == nil) != (f.ImageDigest == nil) {
				return errors.New("incomplete registry reference")
			}
			if f.ImageDigest != nil && (!ValidDigest(*f.ImageDigest) || f.ImageReference == nil || !validReference(*f.ImageReference, *f.ImageDigest)) {
				return errors.New("invalid immutable registry reference")
			}
		} else if f.ImageConfigDigest != nil || f.ImageID != nil || f.ImageReference != nil || f.ImageDigest != nil {
			return errors.New("unexpected image metadata")
		}
	}
	return nil
}
func ValidDigest(v string) bool {
	return strings.HasPrefix(v, "sha256:") && hashPattern.MatchString(strings.TrimPrefix(v, "sha256:"))
}
func validReference(ref, digest string) bool {
	if !strings.HasSuffix(ref, "@"+digest) || strings.ContainsAny(ref, " \t\n\r\\") || strings.Contains(ref, "://") {
		return false
	}
	base := strings.TrimSuffix(ref, "@"+digest)
	if strings.Contains(base, "@") {
		return false
	}
	parts := strings.Split(base, "/")
	return len(parts) >= 2 && parts[0] != "" && parts[len(parts)-1] != ""
}
func Verify(raw []byte, key ed25519.PublicKey) (Envelope, error) {
	var outer Envelope
	if len(raw) > 64<<10 || json.Unmarshal(raw, &outer) != nil {
		return outer, errors.New("invalid manifest envelope")
	}
	payload, err := base64.RawURLEncoding.DecodeString(outer.SignedPayload)
	if err != nil {
		return outer, errors.New("invalid signed payload")
	}
	sig, err := base64.RawURLEncoding.DecodeString(outer.Signature)
	if err != nil || !ed25519.Verify(key, payload, sig) {
		return outer, errors.New("manifest signature rejected")
	}
	var m Manifest
	if json.Unmarshal(payload, &m) != nil {
		return outer, errors.New("invalid signed manifest")
	}
	if err = Validate(m); err != nil {
		return outer, err
	}
	return Envelope{Manifest: m, Signature: outer.Signature, SignedPayload: outer.SignedPayload}, nil
}
func Hash(reader io.Reader) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, reader)
	return hex.EncodeToString(h.Sum(nil)), n, err
}
func CheckFile(path string, meta File) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != meta.Size {
		return errors.New("artifact size/type mismatch")
	}
	sum, n, err := Hash(io.LimitReader(f, meta.Size+1))
	if err != nil || n != meta.Size || sum != meta.SHA256 {
		return errors.New("artifact checksum mismatch")
	}
	return nil
}
func Read(root string, key ed25519.PublicKey, version string) (Envelope, error) {
	file := filepath.Join(root, "current.json")
	if version != "" {
		if !ValidVersion(version) {
			return Envelope{}, errors.New("invalid version")
		}
		file = filepath.Join(root, "releases", version, "manifest.json")
	}
	raw, err := readBound(file, 64<<10)
	if err != nil {
		return Envelope{}, err
	}
	e, err := Verify(raw, key)
	if err == nil && version != "" && e.Version != version {
		return e, errors.New("version mismatch")
	}
	return e, err
}
func write(path string, raw []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = f.Chmod(0600); e != nil {
		return e
	}
	if _, e = f.Write(raw); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	return syncDir(filepath.Dir(path))
}
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func Publish(root, input string, seed []byte, now time.Time) (Envelope, error) {
	if len(seed) != ed25519.SeedSize {
		return Envelope{}, errors.New("invalid signing seed")
	}
	if root == "" || input == "" || filepath.Clean(root) == filepath.Clean(input) {
		return Envelope{}, errors.New("separate root and release input required")
	}
	if err := os.MkdirAll(filepath.Join(root, "releases"), 0700); err != nil {
		return Envelope{}, err
	}
	lock, err := os.OpenFile(filepath.Join(root, ".publish-lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return Envelope{}, err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return Envelope{}, errors.New("publisher busy")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	raw, err := readBound(filepath.Join(input, "build.json"), 16<<10)
	if err != nil {
		return Envelope{}, err
	}
	var build Build
	if len(raw) > 16<<10 || json.Unmarshal(raw, &build) != nil || !ValidVersion(build.Version) {
		return Envelope{}, errors.New("invalid build metadata")
	}
	for _, arch := range []string{"amd64", "arm64"} {
		if err = InspectBinary(filepath.Join(input, "agent-linux-"+arch), arch); err != nil {
			return Envelope{}, err
		}
		image, ok := build.Images[arch]
		if !ok || !ValidDigest(image.ID) {
			return Envelope{}, errors.New("invalid build image metadata")
		}
		if err = InspectImage(filepath.Join(input, "image-linux-"+arch+".tar"), arch, image.ID, image.ConfigDigest); err != nil {
			return Envelope{}, err
		}
	}
	private := ed25519.NewKeyFromSeed(seed)
	key := private.Public().(ed25519.PublicKey)
	existing, err := Read(root, key, build.Version)
	if err == nil {
		if existing.InstallerReady != build.InstallerReady {
			return Envelope{}, errors.New("immutable installer capability changed")
		}
		for _, meta := range existing.Artifacts {
			if err = CheckFile(filepath.Join(input, meta.Name), meta); err != nil {
				return Envelope{}, errors.New("immutable version already contains different files")
			}
			if strings.HasPrefix(meta.Name, "image-") {
				arch := strings.TrimSuffix(strings.TrimPrefix(meta.Name, "image-linux-"), ".tar")
				image, ok := build.Images[arch]
				if !ok || image.ID != *meta.ImageID || image.ConfigDigest != *meta.ImageConfigDigest || !same(image.Reference, meta.ImageReference) || !same(image.Digest, meta.ImageDigest) {
					return Envelope{}, errors.New("immutable image metadata changed")
				}
			}
		}
		out, _ := json.Marshal(existing)
		err = write(filepath.Join(root, "current.json"), out)
		return existing, err
	}
	if !os.IsNotExist(err) {
		return Envelope{}, err
	}
	temp, err := os.MkdirTemp(filepath.Join(root, "releases"), ".candidate-*")
	if err != nil {
		return Envelope{}, err
	}
	defer os.RemoveAll(temp)
	m := Manifest{Version: build.Version, PublishedAt: now.UTC(), InstallerReady: build.InstallerReady, Artifacts: []File{}}
	for _, name := range Names {
		src, err := os.Open(filepath.Join(input, name))
		if err != nil {
			return Envelope{}, err
		}
		info, err := src.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 512<<20 {
			src.Close()
			return Envelope{}, errors.New("invalid artifact file")
		}
		dst, err := os.OpenFile(filepath.Join(temp, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			src.Close()
			return Envelope{}, err
		}
		sum, size, err := Hash(io.TeeReader(io.LimitReader(src, info.Size()+1), dst))
		src.Close()
		if err == nil {
			err = dst.Sync()
		}
		dst.Close()
		if err != nil || size != info.Size() {
			return Envelope{}, errors.New("artifact copy failed")
		}
		f := File{Name: name, Version: m.Version, Platform: "all", Size: size, SHA256: sum, DownloadURL: Path(m.Version, name)}
		for _, arch := range []string{"amd64", "arm64"} {
			if strings.Contains(name, arch) {
				f.Platform = "linux/" + arch
			}
			if name == "image-linux-"+arch+".tar" {
				image, ok := build.Images[arch]
				if !ok {
					return Envelope{}, errors.New("image metadata missing")
				}
				id := image.ID
				f.ImageID = &id
				configDigest := image.ConfigDigest
				f.ImageConfigDigest = &configDigest
				f.ImageReference = image.Reference
				f.ImageDigest = image.Digest
			}
		}
		m.Artifacts = append(m.Artifacts, f)
	}
	if err = Validate(m); err != nil {
		return Envelope{}, err
	}
	payload, _ := json.Marshal(m)
	e := Envelope{Manifest: m, SignedPayload: base64.RawURLEncoding.EncodeToString(payload), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, payload))}
	out, _ := json.Marshal(e)
	if err = write(filepath.Join(temp, "manifest.json"), out); err != nil {
		return e, err
	}
	dest := filepath.Join(root, "releases", m.Version)
	if err = os.Rename(temp, dest); err != nil {
		return e, fmt.Errorf("release activation: %w", err)
	}
	if err = syncDir(filepath.Join(root, "releases")); err != nil {
		return e, err
	}
	return e, write(filepath.Join(root, "current.json"), out)
}
func same(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
