package httpapi

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"shortlink.local/control/internal/security"
	"shortlink.local/geoip"
	"shortlink.local/geoip/testutil"
)

func TestGeoAssetsAuthenticatedDownloadActivationAndRevoke(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	cookie, _ := login(t, s, nil)
	status(t, request(s, "GET", "/v1/assets/geoip/manifest", "", "", "", nil), 401)
	status(t, request(s, "GET", "/v1/admin/system/geoip", "", "", "", nil), 401)
	status(t, request(s, "GET", "/v1/assets/geoip/manifest", "", "", "", cookie), 503)
	root := t.TempDir()
	s.cfg.GeoIPRoot = root
	seed, err := db.Seed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if err = testutil.Staging(root, time.Now().Add(-time.Hour).Unix(), "ID"); err != nil {
		t.Fatal(err)
	}
	release, err := geoip.Publish(root, seed, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	status(t, request(s, "GET", "/v1/assets/geoip/manifest", "", "", "", cookie), 200)
	domain, err := db.CreateDomain(ctx, security.Token(), "geo.example.test", "local")
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := db.Challenge(ctx, domain.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Verify(ctx, domain.ID, challenge.Value, "test"); err != nil {
		t.Fatal(err)
	}
	token, err := db.IssueToken(ctx, domain.ID, "cli", "test")
	if err != nil {
		t.Fatal(err)
	}
	agent, credential, err := db.Enroll(ctx, token.Token, token.InstallationID, security.Token(), domain.Hostname, "cli", "test")
	if err != nil {
		t.Fatal(err)
	}
	get := func(path, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Authorization", "Bearer "+credential)
		r.Header.Set("If-None-Match", etag)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := get("/v1/assets/geoip/manifest", "")
	status(t, w, 200)
	status(t, get("/v1/assets/geoip/manifest", w.Header().Get("ETag")), 304)
	for _, edition := range geoip.Editions {
		status(t, get(geoip.DownloadPath(release.ReleaseID, edition), ""), 200)
	}
	status(t, get(geoip.DownloadPath(release.ReleaseID, "../secrets"), ""), 404)
	server := httptest.NewServer(s)
	defer server.Close()
	manager, err := geoip.NewManager(t.TempDir(), key, server.URL, credential)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if err = manager.Update(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Lookup(netip.MustParseAddr("1.1.1.1"))
	if err != nil || result.Country != "ID" || result.ReleaseID != release.ReleaseID {
		t.Fatal("real API activation failed", result, err)
	}
	if err = db.Heartbeat(ctx, agent, 1, "test", 0, 0, 0, &release.ReleaseID, nil); err != nil {
		t.Fatal(err)
	}
	w = request(s, "GET", "/v1/admin/system/geoip", "", "", "", cookie)
	status(t, w, 200)
	v := readJSON[struct {
		Status string
		Agents []struct{ GeoRelease *string }
	}](t, w)
	if v.Status != "ready" || len(v.Agents) != 1 || v.Agents[0].GeoRelease == nil || *v.Agents[0].GeoRelease != release.ReleaseID {
		t.Fatal("diagnosis does not reflect actual agent")
	}
	if err = db.Revoke(ctx, agent.ID, "test"); err != nil {
		t.Fatal(err)
	}
	status(t, get("/v1/assets/geoip/manifest", ""), 401)
	status(t, get(geoip.DownloadPath(release.ReleaseID, geoip.Editions[0]), ""), 401)
}
