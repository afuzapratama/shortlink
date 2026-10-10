package risk

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIndexesAndSourceValidation(t *testing.T) {
	index, e := NewIndex([]string{"8.8.8.8", "9.9.9.0/24", "2001:4860::/32"})
	if e != nil {
		t.Fatal(e)
	}
	for _, ip := range []string{"8.8.8.8", "::ffff:8.8.8.8", "9.9.9.9", "2001:4860::8888"} {
		if !index.Contains(netip.MustParseAddr(ip)) {
			t.Fatal(ip)
		}
	}
	if index.Contains(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("clean false positive")
	}
	for _, bad := range []string{"0.0.0.0/0", "127.0.0.1", "10.0.0.0/24", "9.9.9.8/24", "https://example.org", "garbage"} {
		if _, e = ParseList([]byte(bad)); e == nil {
			t.Fatal("bad import", bad)
		}
	}
	values, e := ParseList([]byte("# comment\n8.8.8.8\n8.8.8.8\n9.9.9.0/24\n"))
	if e != nil || len(values) != 2 {
		t.Fatal(values, e)
	}
}
func TestSignedCacheTamperRollbackAndSourceExpiry(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	source := now
	feed := Feed{Schema: 1, PublishedAt: now, ExpiresAt: now.Add(48 * time.Hour), TorFetchedAt: &source, Tor: []string{"8.8.8.8"}, Reputation: []string{}}
	env, e := Sign(feed, private)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(env)
	if _, _, e = Verify(raw, public, now, true); e != nil {
		t.Fatal(e)
	}
	tampered := env
	tampered.Signature = "invalid"
	bad, _ := json.Marshal(tampered)
	if _, _, e = Verify(bad, public, now, true); e == nil {
		t.Fatal("tamper")
	}
	if _, _, e = Verify(raw, public, now.Add(49*time.Hour), true); e == nil {
		t.Fatal("expiry")
	}
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("auth missing")
		}
		w.WriteHeader(status)
		if status == 200 {
			w.Write(raw)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	m, e := NewManager(root, public, server.URL, "test-secret")
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Update(context.Background()); e != nil {
		t.Fatal(e)
	}
	result := m.Lookup(netip.MustParseAddr("8.8.8.8"), now)
	if !result.Tor || result.ReputationStatus != "unconfigured" {
		t.Fatal(result)
	}
	m2, e := NewManager(root, public, server.URL, "test-secret")
	if e != nil || !m2.Lookup(netip.MustParseAddr("8.8.8.8"), now).Tor {
		t.Fatal("restart", e)
	}
	oldFeed := feed
	oldFeed.PublishedAt = now.Add(-time.Hour)
	oldFeed.ExpiresAt = now.Add(time.Hour)
	oldEnv, _ := Sign(oldFeed, private)
	raw, _ = json.Marshal(oldEnv)
	if m.Update(context.Background()) == nil {
		t.Fatal("rollback")
	}
	raw = bad
	if m.Update(context.Background()) == nil {
		t.Fatal("tamper update")
	}
	if !m.Lookup(netip.MustParseAddr("8.8.8.8"), now).Tor {
		t.Fatal("healthy cache replaced")
	}
	stale := now.Add(-49 * time.Hour)
	feed.PublishedAt = now.Add(time.Second)
	feed.TorFetchedAt = &stale
	env, _ = Sign(feed, private)
	raw, _ = json.Marshal(env)
	if e = m.Update(context.Background()); e != nil {
		t.Fatal(e)
	}
	result = m.Lookup(netip.MustParseAddr("8.8.8.8"), now)
	if result.Tor || result.TorStatus != "stale" {
		t.Fatal(result)
	}
	status = 401
	if e = m.Update(context.Background()); e != ErrCredential {
		t.Fatal(e)
	}
}
func TestPublisherDoesNotRefreshStaleSourceTimestamp(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	root := t.TempDir()
	now := time.Now().UTC()
	if e := writeList(root); e != nil {
		t.Fatal(e)
	}
	source := now.Add(-49 * time.Hour)
	os.Chtimes(filepath.Join(root, "staging", "tor.txt"), source, source)
	env, feed, e := Publish(root, key.Seed(), now)
	if e != nil {
		t.Fatal(e)
	}
	if !feed.TorFetchedAt.Equal(source) {
		t.Fatal("source timestamp refreshed")
	}
	other, _, e := Publish(root, key.Seed(), now.Add(time.Minute))
	if e != nil || other.Version != env.Version {
		t.Fatal("unchanged source reissued", e)
	}
}
func writeList(root string) error {
	return AtomicFile(filepath.Join(root, "staging", "tor.txt"), []byte("8.8.8.8\n"))
}
