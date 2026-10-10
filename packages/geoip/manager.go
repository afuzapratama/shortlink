package geoip

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	maxminddb "github.com/oschwald/maxminddb-golang/v2"
)

type Manager struct {
	root               string
	key                ed25519.PublicKey
	origin, credential string
	client             *http.Client
	updateMu           sync.Mutex
	mu                 sync.RWMutex
	manifest           *Manifest
	readers            map[string]*maxminddb.Reader
}
type Result struct {
	Country      string `json:"country"`
	City         string `json:"city"`
	ASN          uint32 `json:"asn"`
	Organization string `json:"organization"`
	ReleaseID    string `json:"releaseId"`
}

func NewManager(root string, key ed25519.PublicKey, origin, credential string) (*Manager, error) {
	if err := os.MkdirAll(filepath.Join(root, "releases"), 0700); err != nil {
		return nil, err
	}
	m := &Manager{root: root, key: key, origin: strings.TrimRight(origin, "/"), credential: credential, client: &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	e, _, err := ReadManifest(root, key, time.Now(), false)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	m.manifest = &e.Manifest
	if time.Now().Before(e.ExpiresAt) {
		m.readers, err = OpenRelease(root, e.Manifest)
		if err != nil {
			return nil, err
		}
	}
	return m, nil
}
func (m *Manager) request(ctx context.Context, path, etag string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", m.origin+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+m.credential)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	return m.client.Do(req)
}
func (m *Manager) Update(ctx context.Context) error {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	m.mu.RLock()
	var old *Manifest
	if m.manifest != nil {
		copy := *m.manifest
		old = &copy
	}
	m.mu.RUnlock()
	etag := ""
	if old != nil {
		etag = `"` + old.ReleaseID + `"`
	}
	resp, err := m.request(ctx, "/v1/assets/geoip/manifest", etag)
	if err != nil {
		return errors.New("geo manifest unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return ErrCredential
	}
	if resp.StatusCode == 304 {
		if old == nil || !time.Now().Before(old.ExpiresAt) {
			return ErrUnavailable
		}
		return nil
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("geo manifest HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxManifestSize+1))
	if err != nil {
		return err
	}
	e, err := Verify(raw, m.key, time.Now(), true)
	if err != nil {
		return err
	}
	if old != nil {
		if e.PublishedAt.Before(old.PublishedAt) {
			return errors.New("geo manifest rollback rejected")
		}
		for i, f := range e.Files {
			if f.BuildEpoch < old.Files[i].BuildEpoch {
				return errors.New("geo database rollback rejected")
			}
		}
		if e.ReleaseID == old.ReleaseID {
			if !e.ExpiresAt.Equal(old.ExpiresAt) || !e.PublishedAt.Equal(old.PublishedAt) {
				return errors.New("unchanged content cannot renew geo expiry")
			}
			return nil
		}
	}
	temp, err := os.MkdirTemp(filepath.Join(m.root, "releases"), ".download-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	for _, file := range e.Files {
		if err = m.download(ctx, temp, file); err != nil {
			return err
		}
	}
	canonical, _ := json.Marshal(e)
	if err = AtomicWrite(filepath.Join(temp, "manifest.json"), canonical); err != nil {
		return err
	}
	final := filepath.Join(m.root, "releases", e.ReleaseID)
	// Reuse a complete immutable orphan from a crash; validate it before activation.
	if _, err = os.Stat(final); os.IsNotExist(err) {
		if err = os.Rename(temp, final); err != nil {
			return err
		}
		if err = SyncDir(filepath.Join(m.root, "releases")); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	readers, err := OpenRelease(m.root, e.Manifest)
	if err != nil {
		return err
	}
	if err = AtomicWrite(filepath.Join(m.root, "current.json"), canonical); err != nil {
		CloseReaders(readers)
		return err
	}
	m.mu.Lock()
	previous := m.readers
	m.readers = readers
	m.manifest = &e.Manifest
	CloseReaders(previous)
	m.mu.Unlock()
	return Cleanup(m.root, m.key, time.Now())
}
func (m *Manager) download(ctx context.Context, temp string, file File) error {
	resp, err := m.request(ctx, file.DownloadURL, "")
	if err != nil {
		return errors.New("geo download unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return ErrCredential
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("geo download HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength >= 0 && resp.ContentLength != file.Size {
		return errors.New("geo content length mismatch")
	}
	f, err := os.OpenFile(filepath.Join(temp, file.Edition+".mmdb"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, file.Size+1))
	if err != nil {
		return err
	}
	if n != file.Size || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return errors.New("geo size or checksum mismatch")
	}
	return f.Sync()
}
func (m *Manager) ActiveRelease() *string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.manifest == nil || len(m.readers) != 3 || !time.Now().Before(m.manifest.ExpiresAt) {
		return nil
	}
	id := m.manifest.ReleaseID
	return &id
}
func (m *Manager) Lookup(ip netip.Addr) (Result, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.manifest == nil || len(m.readers) != 3 || !time.Now().Before(m.manifest.ExpiresAt) {
		return Result{}, ErrUnavailable
	}
	var location struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
		City struct {
			Names struct {
				English string `maxminddb:"en"`
			} `maxminddb:"names"`
		} `maxminddb:"city"`
	}
	if err := m.readers["GeoLite2-City"].Lookup(ip.Unmap()).Decode(&location); err != nil {
		return Result{}, err
	}
	if location.Country.ISOCode == "" {
		var country struct {
			Country struct {
				ISOCode string `maxminddb:"iso_code"`
			} `maxminddb:"country"`
		}
		if err := m.readers["GeoLite2-Country"].Lookup(ip.Unmap()).Decode(&country); err != nil {
			return Result{}, err
		}
		location.Country.ISOCode = country.Country.ISOCode
	}
	var asn struct {
		Number       uint32 `maxminddb:"autonomous_system_number"`
		Organization string `maxminddb:"autonomous_system_organization"`
	}
	if err := m.readers["GeoLite2-ASN"].Lookup(ip.Unmap()).Decode(&asn); err != nil {
		return Result{}, err
	}
	return Result{Country: location.Country.ISOCode, City: location.City.Names.English, ASN: asn.Number, Organization: asn.Organization, ReleaseID: m.manifest.ReleaseID}, nil
}
func (m *Manager) Close() { m.mu.Lock(); defer m.mu.Unlock(); CloseReaders(m.readers); m.readers = nil }
func (m *Manager) Cleanup() error {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	m.mu.Lock()
	if m.manifest != nil && !time.Now().Before(m.manifest.ExpiresAt) {
		CloseReaders(m.readers)
		m.readers = nil
	}
	m.mu.Unlock()
	return Cleanup(m.root, m.key, time.Now())
}
