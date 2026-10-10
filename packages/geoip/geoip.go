// Package geoip publishes and validates immutable, signed GeoLite releases.
package geoip

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	maxminddb "github.com/oschwald/maxminddb-golang/v2"
)

const MaxFileSize int64 = 512 << 20
const MaxManifestSize = 64 << 10

var Editions = []string{"GeoLite2-Country", "GeoLite2-City", "GeoLite2-ASN"}
var releasePattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var ErrUnavailable = errors.New("geo unavailable")
var ErrCredential = errors.New("geo credential rejected")

type File struct {
	Edition     string `json:"edition"`
	BuildEpoch  int64  `json:"buildEpoch"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	DownloadURL string `json:"downloadUrl"`
}
type Manifest struct {
	ReleaseID   string    `json:"releaseId"`
	PublishedAt time.Time `json:"publishedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Files       []File    `json:"files"`
	Attribution string    `json:"attribution"`
	LicenseURL  string    `json:"licenseUrl"`
}
type Envelope struct {
	Manifest
	Signature     string `json:"signature"`
	SignedPayload string `json:"signedPayload"`
}

func ValidRelease(id string) bool { return releasePattern.MatchString(id) }
func ValidEdition(edition string) bool {
	for _, e := range Editions {
		if edition == e {
			return true
		}
	}
	return false
}
func DownloadPath(id, edition string) string {
	return "/v1/assets/geoip/releases/" + id + "/" + edition
}
func contentID(files []File) string {
	hash := sha256.New()
	for _, f := range files {
		fmt.Fprintf(hash, "%s:%s:%d:%d\n", f.Edition, f.SHA256, f.Size, f.BuildEpoch)
	}
	return hex.EncodeToString(hash.Sum(nil))
}
func Verify(raw []byte, key ed25519.PublicKey, now time.Time, requireFresh bool) (Envelope, error) {
	var e Envelope
	if len(key) != ed25519.PublicKeySize {
		return e, errors.New("invalid verification key")
	}
	if len(raw) > MaxManifestSize || json.Unmarshal(raw, &e) != nil {
		return e, errors.New("invalid manifest envelope")
	}
	payload, err := base64.RawURLEncoding.DecodeString(e.SignedPayload)
	if err != nil {
		return e, errors.New("invalid manifest payload")
	}
	sig, err := base64.RawURLEncoding.DecodeString(e.Signature)
	if err != nil || !ed25519.Verify(key, payload, sig) {
		return e, errors.New("invalid manifest signature")
	}
	var m Manifest
	if json.Unmarshal(payload, &m) != nil {
		return e, errors.New("invalid manifest")
	}
	if !ValidRelease(m.ReleaseID) || len(m.Files) != 3 || m.PublishedAt.After(now.Add(5*time.Minute)) || m.PublishedAt.IsZero() || !m.ExpiresAt.After(m.PublishedAt) || m.ExpiresAt.After(m.PublishedAt.Add(30*24*time.Hour)) || (requireFresh && !now.Before(m.ExpiresAt)) {
		return e, errors.New("invalid manifest time or release")
	}
	for i, f := range m.Files {
		if f.Edition != Editions[i] || f.Size < 1 || f.Size > MaxFileSize || !releasePattern.MatchString(f.SHA256) || f.BuildEpoch < 1 || f.DownloadURL != DownloadPath(m.ReleaseID, f.Edition) {
			return e, errors.New("invalid manifest file")
		}
		if m.ExpiresAt.After(time.Unix(f.BuildEpoch, 0).Add(29 * 24 * time.Hour)) {
			return e, errors.New("manifest exceeds database freshness limit")
		}
	}
	if contentID(m.Files) != m.ReleaseID {
		return e, errors.New("manifest content identity mismatch")
	}
	if m.Attribution == "" || m.LicenseURL != "https://www.maxmind.com/en/geolite/eula" {
		return e, errors.New("missing attribution or license")
	}
	e.Manifest = m // outer duplicates are not authoritative
	return e, nil
}
func ReadManifest(root string, key ed25519.PublicKey, now time.Time, requireFresh bool) (Envelope, []byte, error) {
	raw, err := ReadSignedFile(filepath.Join(root, "current.json"))
	if err != nil {
		return Envelope{}, nil, err
	}
	e, err := Verify(raw, key, now, requireFresh)
	return e, raw, err
}
func ReadSignedFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxManifestSize+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxManifestSize {
		return nil, errors.New("manifest exceeds size limit")
	}
	return raw, nil
}
func AtomicWrite(path string, raw []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(raw); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return SyncDir(filepath.Dir(path))
}
func SyncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func inspect(path, edition string) (File, *maxminddb.Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return File{}, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > MaxFileSize {
		return File{}, nil, errors.New("invalid MMDB file size")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, MaxFileSize+1))
	if err != nil || n != info.Size() {
		return File{}, nil, errors.New("MMDB hashing failed")
	}
	reader, err := maxminddb.Open(path)
	if err != nil {
		return File{}, nil, errors.New("MMDB open failed")
	}
	fail := func() (File, *maxminddb.Reader, error) {
		reader.Close()
		return File{}, nil, errors.New("MMDB metadata or verification failed")
	}
	if reader.Metadata.DatabaseType != edition || reader.Metadata.IPVersion != 6 || reader.Metadata.BuildEpoch == 0 || reader.Metadata.BuildTime().After(time.Now().Add(24*time.Hour)) || reader.Verify() != nil {
		return fail()
	}
	for _, ip := range []string{"1.1.1.1", "2001:4860:4860::8888"} {
		var value any
		if reader.Lookup(netip.MustParseAddr(ip)).Decode(&value) != nil {
			return fail()
		}
	}
	return File{Edition: edition, Size: n, SHA256: hex.EncodeToString(h.Sum(nil)), BuildEpoch: int64(reader.Metadata.BuildEpoch)}, reader, nil
}
func OpenRelease(root string, m Manifest) (map[string]*maxminddb.Reader, error) {
	readers := map[string]*maxminddb.Reader{}
	for _, file := range m.Files {
		meta, reader, err := inspect(filepath.Join(root, "releases", m.ReleaseID, file.Edition+".mmdb"), file.Edition)
		if err != nil || meta.Size != file.Size || meta.SHA256 != file.SHA256 || meta.BuildEpoch != file.BuildEpoch {
			if reader != nil {
				reader.Close()
			}
			CloseReaders(readers)
			return nil, errors.New("MMDB content does not match manifest")
		}
		readers[file.Edition] = reader
	}
	return readers, nil
}
func CloseReaders(readers map[string]*maxminddb.Reader) {
	for _, r := range readers {
		r.Close()
	}
}
func Publish(root string, seed []byte, now time.Time) (Envelope, error) {
	if len(seed) != ed25519.SeedSize {
		return Envelope{}, errors.New("invalid signing seed")
	}
	if err := os.MkdirAll(filepath.Join(root, "releases"), 0700); err != nil {
		return Envelope{}, err
	}
	// Advisory flock is automatically released after process exit/crash (Linux VPS).
	lock, err := os.OpenFile(filepath.Join(root, ".publish-lock"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return Envelope{}, err
	}
	defer lock.Close()
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return Envelope{}, errors.New("publisher locked")
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	temp, err := os.MkdirTemp(filepath.Join(root, "releases"), ".candidate-*")
	if err != nil {
		return Envelope{}, err
	}
	defer os.RemoveAll(temp)
	files := []File{}
	for _, edition := range Editions {
		source := filepath.Join(root, "staging", edition+".mmdb")
		in, err := os.Open(source)
		if err != nil {
			return Envelope{}, errors.New("complete GeoLite staging set required")
		}
		before, err := in.Stat()
		if err != nil || !before.Mode().IsRegular() || before.Size() > MaxFileSize {
			in.Close()
			return Envelope{}, errors.New("invalid staging file")
		}
		out, err := os.OpenFile(filepath.Join(temp, edition+".mmdb"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			return Envelope{}, err
		}
		n, copyErr := io.Copy(out, io.LimitReader(in, MaxFileSize+1))
		syncErr := out.Sync()
		out.Close()
		after, statErr := in.Stat()
		in.Close()
		pathInfo, pathErr := os.Stat(source)
		if copyErr != nil || syncErr != nil || statErr != nil || pathErr != nil || n != before.Size() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || !os.SameFile(before, pathInfo) || !before.ModTime().Equal(pathInfo.ModTime()) {
			return Envelope{}, errors.New("staging changed during copy")
		}
		meta, reader, err := inspect(filepath.Join(temp, edition+".mmdb"), edition)
		if err != nil {
			return Envelope{}, err
		}
		reader.Close()
		if now.Sub(time.Unix(meta.BuildEpoch, 0)) > 30*24*time.Hour {
			return Envelope{}, errors.New("staging database build is older than 30 days")
		}
		files = append(files, meta)
	}
	id := contentID(files)
	key := ed25519.NewKeyFromSeed(seed)
	public := key.Public().(ed25519.PublicKey)
	existing, _, oldErr := ReadManifest(root, public, now, false)
	if oldErr == nil && existing.ReleaseID == id {
		if !now.Before(existing.ExpiresAt) {
			return Envelope{}, errors.New("unchanged GeoLite release expired; fetch a new database build")
		}
		readers, err := OpenRelease(root, existing.Manifest)
		if err != nil {
			return Envelope{}, err
		}
		CloseReaders(readers)
		return existing, nil
	}
	if oldErr != nil && !os.IsNotExist(oldErr) {
		return Envelope{}, errors.New("current manifest corrupt; operator recovery required")
	}
	if oldErr == nil {
		for i, f := range files {
			if f.BuildEpoch < existing.Files[i].BuildEpoch {
				return Envelope{}, errors.New("database build rollback rejected")
			}
		}
	}
	for i := range files {
		files[i].DownloadURL = DownloadPath(id, files[i].Edition)
	}
	expires := time.Unix(files[0].BuildEpoch, 0).UTC().Add(29 * 24 * time.Hour)
	for _, file := range files {
		deadline := time.Unix(file.BuildEpoch, 0).UTC().Add(29 * 24 * time.Hour)
		if deadline.Before(expires) {
			expires = deadline
		}
	}
	if !now.Before(expires) {
		return Envelope{}, errors.New("database freshness limit exceeded; fetch a newer build")
	}
	m := Manifest{ReleaseID: id, PublishedAt: now.UTC(), ExpiresAt: expires, Files: files, Attribution: "This product includes GeoLite2 data created by MaxMind, available from https://www.maxmind.com.", LicenseURL: "https://www.maxmind.com/en/geolite/eula"}
	payload, _ := json.Marshal(m)
	e := Envelope{Manifest: m, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, payload)), SignedPayload: base64.RawURLEncoding.EncodeToString(payload)}
	raw, _ := json.Marshal(e)
	if err = AtomicWrite(filepath.Join(temp, "manifest.json"), raw); err != nil {
		return Envelope{}, err
	}
	destination := filepath.Join(root, "releases", id)
	if _, err = os.Stat(destination); err == nil {
		// Recover a completed immutable release left before current.json was committed.
		orphanRaw, readErr := ReadSignedFile(filepath.Join(destination, "manifest.json"))
		orphan, verifyErr := Verify(orphanRaw, public, now, true)
		if readErr != nil || verifyErr != nil || orphan.ReleaseID != id || (oldErr == nil && orphan.PublishedAt.Before(existing.PublishedAt)) {
			return Envelope{}, errors.New("invalid orphan release; operator recovery required")
		}
		readers, openErr := OpenRelease(root, orphan.Manifest)
		if openErr != nil {
			return Envelope{}, openErr
		}
		CloseReaders(readers)
		if err = AtomicWrite(filepath.Join(root, "current.json"), orphanRaw); err != nil {
			return Envelope{}, err
		}
		return orphan, nil
	}
	if err = os.Rename(temp, destination); err != nil {
		return Envelope{}, err
	}
	if err = SyncDir(filepath.Join(root, "releases")); err != nil {
		return Envelope{}, err
	}
	if err = AtomicWrite(filepath.Join(root, "current.json"), raw); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

// Cleanup deletes signed, expired releases, including an expired active release. No MMDB backup copies are made.
func Cleanup(root string, key ed25519.PublicKey, now time.Time) error {
	entries, err := os.ReadDir(filepath.Join(root, "releases"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() && (strings.HasPrefix(entry.Name(), ".candidate-") || strings.HasPrefix(entry.Name(), ".download-")) {
			if info, e := entry.Info(); e == nil && now.Sub(info.ModTime()) > 24*time.Hour {
				if err := os.RemoveAll(filepath.Join(root, "releases", entry.Name())); err != nil {
					return err
				}
			}
			continue
		}
		if !entry.IsDir() || !ValidRelease(entry.Name()) {
			continue
		}
		path := filepath.Join(root, "releases", entry.Name())
		raw, err := ReadSignedFile(filepath.Join(path, "manifest.json"))
		if err != nil {
			continue
		}
		e, err := Verify(raw, key, now, false)
		if err != nil || e.ReleaseID != entry.Name() {
			continue
		}
		if !now.Before(e.ExpiresAt) {
			if err = os.RemoveAll(path); err != nil {
				return err
			}
		}
	}
	return nil
}
