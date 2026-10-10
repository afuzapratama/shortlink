package geoip_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"shortlink.local/geoip"
	"shortlink.local/geoip/testutil"
)

func setup(t *testing.T) (string, []byte, ed25519.PublicKey) {
	t.Helper()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	root := t.TempDir()
	if err := testutil.Staging(root, time.Now().Add(-time.Hour).Unix(), "ID"); err != nil {
		t.Fatal(err)
	}
	return root, private.Seed(), public
}
func TestPublisherAtomicValidationAndCleanup(t *testing.T) {
	root, seed, key := setup(t)
	now := time.Now()
	first, err := geoip.Publish(root, seed, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := geoip.Publish(root, seed, now.Add(time.Hour))
	if err != nil || second.ReleaseID != first.ReleaseID || !second.PublishedAt.Equal(first.PublishedAt) {
		t.Fatal("unchanged release republished", err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "current.json"))
	if _, err := geoip.Verify(raw, key, now, true); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "staging", "GeoLite2-City.mmdb"), []byte("corrupt"), 0600)
	if _, err := geoip.Publish(root, seed, now); err == nil {
		t.Fatal("corrupt staging published")
	}
	current, _, err := geoip.ReadManifest(root, key, now, true)
	if err != nil || current.ReleaseID != first.ReleaseID {
		t.Fatal("healthy pointer replaced")
	}
	if err := testutil.Staging(root, now.Unix(), "US"); err != nil {
		t.Fatal(err)
	}
	newer, err := geoip.Publish(root, seed, now.Add(time.Hour))
	if err != nil || newer.ReleaseID == first.ReleaseID {
		t.Fatal("update missing", err)
	}
	if err := geoip.Cleanup(root, key, first.ExpiresAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "releases", first.ReleaseID)); !os.IsNotExist(err) {
		t.Fatal("expired release retained")
	}
	if _, err := os.Stat(filepath.Join(root, "releases", newer.ReleaseID)); err != nil {
		t.Fatal("fresh release removed")
	}
}
func TestManifestRejectsTamperPathAndRollbackMetadata(t *testing.T) {
	root, seed, key := setup(t)
	e, err := geoip.Publish(root, seed, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(e)
	var outer map[string]any
	json.Unmarshal(raw, &outer)
	outer["releaseId"] = "fake"
	raw, _ = json.Marshal(outer)
	v, err := geoip.Verify(raw, key, time.Now(), true)
	if err != nil || v.ReleaseID != e.ReleaseID {
		t.Fatal("outer duplicates used")
	}
	outer["signature"] = "invalid"
	raw, _ = json.Marshal(outer)
	if _, err := geoip.Verify(raw, key, time.Now(), true); err == nil {
		t.Fatal("tamper accepted")
	}
	e.Files[0].DownloadURL = "https://attacker.example.org/token"
	payload, _ := json.Marshal(e.Manifest)
	e.SignedPayload = base64.RawURLEncoding.EncodeToString(payload)
	e.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed), payload))
	raw, _ = json.Marshal(e)
	if _, err := geoip.Verify(raw, key, time.Now(), true); err == nil {
		t.Fatal("off-origin URL accepted")
	}
	if err := testutil.Staging(root, time.Now().Add(-2*time.Hour).Unix(), "US"); err != nil {
		t.Fatal(err)
	}
	if _, err := geoip.Publish(root, seed, time.Now()); err == nil {
		t.Fatal("older database published")
	}
}
func TestManagerDownloadActivationRestartAndCorruption(t *testing.T) {
	root, seed, key := setup(t)
	e, err := geoip.Publish(root, seed, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	broken := false
	outage := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer fixture-secret" {
			w.WriteHeader(401)
			return
		}
		if outage {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/v1/assets/geoip/manifest" {
			manifest, raw, err := geoip.ReadManifest(root, key, time.Now(), true)
			if err != nil {
				w.WriteHeader(503)
				return
			}
			if r.Header.Get("If-None-Match") == `"`+manifest.ReleaseID+`"` {
				w.WriteHeader(304)
				return
			}
			w.Write(raw)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/assets/geoip/releases/"), "/")
		if len(parts) != 2 {
			w.WriteHeader(404)
			return
		}

		raw, err := os.ReadFile(filepath.Join(root, "releases", parts[0], parts[1]+".mmdb"))
		if err != nil {
			w.WriteHeader(404)
			return
		}
		if broken && len(raw) > 0 {
			raw[len(raw)/2] ^= 1
		}
		w.Write(raw)
	}))
	defer server.Close()
	data := t.TempDir()
	manager, err := geoip.NewManager(data, key, server.URL, "fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if err = manager.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if manager.ActiveRelease() == nil || *manager.ActiveRelease() != e.ReleaseID {
		t.Fatal("activation missing")
	}
	for _, ip := range []string{"1.1.1.1", "2001:4860:4860::8888"} {
		result, err := manager.Lookup(netip.MustParseAddr(ip))
		if err != nil || result.Country != "ID" || result.ASN != 64512 {
			t.Fatal("lookup incorrect", result, err)
		}
	}
	if err := manager.Update(context.Background()); err != nil {
		t.Fatal("304 failed", err)
	}
	restarted, err := geoip.NewManager(data, key, server.URL, "fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	restarted.Close()
	if err := testutil.Staging(root, time.Now().Unix(), "US"); err != nil {
		t.Fatal(err)
	}
	if _, err = geoip.Publish(root, seed, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	broken = true
	mu.Unlock()
	if manager.Update(context.Background()) == nil {
		t.Fatal("corrupt download activated")
	}
	if *manager.ActiveRelease() != e.ReleaseID {
		t.Fatal("healthy data lost")
	}
	mu.Lock()
	broken = false
	mu.Unlock()
	if err = manager.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Lookup(netip.MustParseAddr("1.1.1.1"))
	if err != nil || result.Country != "US" {
		t.Fatal("new reader not active", err)
	}
	mu.Lock()
	outage = true
	mu.Unlock()
	if manager.Update(context.Background()) == nil {
		t.Fatal("outage ignored")
	}
	if manager.ActiveRelease() == nil {
		t.Fatal("outage lost healthy data")
	}
	// Simultaneous lookups and reader swaps are checked by the race detector.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if _, err := manager.Lookup(netip.MustParseAddr("1.1.1.1")); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	mu.Lock()
	outage = false
	mu.Unlock()
	if err := manager.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}

func TestExpiredManifestDoesNotRenewOn304(t *testing.T) {
	root, seed, key := setup(t)
	e, err := geoip.Publish(root, seed, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	e.PublishedAt = time.Now().Add(-30 * 24 * time.Hour)
	e.ExpiresAt = time.Now().Add(-24 * time.Hour)
	payload, _ := json.Marshal(e.Manifest)
	e.SignedPayload = base64.RawURLEncoding.EncodeToString(payload)
	e.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed), payload))
	raw, _ := json.Marshal(e)
	data := t.TempDir()
	if err = os.WriteFile(filepath.Join(data, "current.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(304) }))
	defer server.Close()
	manager, err := geoip.NewManager(data, key, server.URL, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if manager.ActiveRelease() != nil {
		t.Fatal("expired data active")
	}
	if manager.Update(context.Background()) == nil {
		t.Fatal("304 renewed GeoLite expiry")
	}
	if _, err = manager.Lookup(netip.MustParseAddr("1.1.1.1")); err == nil {
		t.Fatal("expired geo lookup allowed")
	}
}
func TestWrongEditionAndPublisherCrashRecovery(t *testing.T) {
	root, seed, key := setup(t)
	raw, err := os.ReadFile(filepath.Join(root, "staging", "GeoLite2-Country.mmdb"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "staging", "GeoLite2-City.mmdb"), raw, 0600)
	if _, err = geoip.Publish(root, seed, time.Now()); err == nil {
		t.Fatal("wrong database edition published")
	}
	if err = testutil.Staging(root, time.Now().Add(-time.Hour).Unix(), "ID"); err != nil {
		t.Fatal(err)
	}
	e, err := geoip.Publish(root, seed, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// A complete release can survive a crash immediately before committing the active pointer.
	os.Remove(filepath.Join(root, "current.json"))
	recovered, err := geoip.Publish(root, seed, time.Now().Add(time.Second))
	if err != nil || recovered.ReleaseID != e.ReleaseID || !recovered.PublishedAt.Equal(e.PublishedAt) {
		t.Fatal("orphan recovery failed", err)
	}
	if _, _, err = geoip.ReadManifest(root, key, time.Now(), true); err != nil {
		t.Fatal(err)
	}
}
