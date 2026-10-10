package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"shortlink.local/control/internal/security"
	"shortlink.local/geoip"
	"shortlink.local/geoip/testutil"
	"shortlink.local/policy"
	"strings"
	"testing"
	"time"
)

func TestReadinessPrerequisitesAuthenticatedProbeAndExpiry(t *testing.T) {
	s, db := setup(t)
	cookie, csrf := login(t, s, nil)
	ctx := context.Background()
	d, e := db.CreateDomain(ctx, security.Token(), "ready.domainanda.com", "local")
	if e != nil {
		t.Fatal(e)
	}
	path := "/v1/admin/domains/" + d.ID
	status(t, request(s, "POST", path+"/probe", "", testOrigin, "", cookie), 403)
	calls := 0
	s.publicProbe = func(_ context.Context, host, nonce, digest string) (policy.ProbeState, error) {
		calls++
		return policy.ProbeState{}, errors.New("no TLS")
	}
	status(t, request(s, "POST", path+"/probe", "", testOrigin, csrf, cookie), 200)
	if calls != 0 {
		t.Fatal("probed without prerequisites")
	}
	challenge, e := db.Challenge(ctx, d.ID, "test")
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Verify(ctx, d.ID, challenge.Value, "test"); e != nil {
		t.Fatal(e)
	}
	token, e := db.IssueToken(ctx, d.ID, "cli", "test")
	if e != nil {
		t.Fatal(e)
	}
	agent, credential, e := db.Enroll(ctx, token.Token, token.InstallationID, security.Token(), d.Hostname, "cli", "b11")
	if e != nil {
		t.Fatal(e)
	}
	seed, _ := db.Seed(ctx)
	s.cfg.GeoIPRoot = t.TempDir()
	if e = testutil.Staging(s.cfg.GeoIPRoot, time.Now().Add(-time.Hour).Unix(), "ID"); e != nil {
		t.Fatal(e)
	}
	release, e := geoip.Publish(s.cfg.GeoIPRoot, seed, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	d, _ = db.Domain(ctx, d.ID)
	geo := release.ReleaseID
	if e = db.Heartbeat(ctx, agent, d.DesiredRevision, "b11", 0, 0, 0, &geo, nil); e != nil {
		t.Fatal(e)
	}
	d, _ = db.Domain(ctx, d.ID)
	if d.Readiness == "ready" {
		t.Fatal("heartbeat marked ready")
	}
	s.publicProbe = func(_ context.Context, host, nonce, digest string) (policy.ProbeState, error) {
		calls++
		if host != d.Hostname || digest != policy.ProbeDigest(credential) {
			t.Fatal("wrong bound probe")
		}
		return policy.ProbeState{Nonce: nonce, DomainID: d.ID, Revision: d.DesiredRevision, GeoRelease: &geo, Ready: true, Version: "b11"}, nil
	}
	w := request(s, "POST", path+"/probe", "", testOrigin, csrf, cookie)
	status(t, w, 200)
	var result struct{ Readiness string }
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.Readiness != "ready" {
		t.Fatal("valid probe not ready", w.Body.String())
	}
	w = request(s, "GET", path+"/installation", "", "", "", cookie)
	status(t, w, 200)
	if string(w.Body.Bytes()) == "" || json.Valid(w.Body.Bytes()) == false {
		t.Fatal("invalid progress")
	}
	if bytesContain := containsPrivate(w.Body.String(), credential, policy.ProbeDigest(credential)); bytesContain {
		t.Fatal("credential leaked")
	}
	if _, e = db.DB.Exec(ctx, "UPDATE domains SET probe_checked_at=now()-interval '6 minutes' WHERE id=$1", d.ID); e != nil {
		t.Fatal(e)
	}
	d, _ = db.Domain(ctx, d.ID)
	if d.Readiness != "attention" {
		t.Fatal("expired probe remained ready")
	}
	s.publicProbe = func(_ context.Context, host, nonce, digest string) (policy.ProbeState, error) {
		return policy.ProbeState{Nonce: nonce, DomainID: "wrong-domain", Revision: d.DesiredRevision, GeoRelease: &geo, Ready: true}, nil
	}
	w = request(s, "POST", path+"/probe", "", testOrigin, csrf, cookie)
	status(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.Readiness == "ready" {
		t.Fatal("wrong domain accepted")
	}
	s.publicProbe = func(_ context.Context, host, nonce, digest string) (policy.ProbeState, error) {
		if _, err := db.DB.Exec(ctx, "UPDATE domains SET desired_revision=desired_revision+1 WHERE id=$1", d.ID); err != nil {
			t.Fatal(err)
		}
		return policy.ProbeState{Nonce: nonce, DomainID: d.ID, Revision: d.DesiredRevision, GeoRelease: &geo, Ready: true}, nil
	}
	status(t, request(s, "POST", path+"/probe", "", testOrigin, csrf, cookie), 409)
	if e = db.Revoke(ctx, agent.ID, "test"); e != nil {
		t.Fatal(e)
	}
	status(t, request(s, "POST", path+"/probe", "", testOrigin, csrf, cookie), 200)
}
func containsPrivate(raw string, values ...string) bool {
	for _, v := range values {
		if strings.Contains(raw, v) {
			return true
		}
	}
	return false
}
func TestPublicProbeRejectsPrivateReservedAndMappedIPs(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "::1", "::ffff:192.168.1.1", "fc00::1", "fe80::1", "192.0.2.1"} {
		if publicAddress(netip.MustParseAddr(raw)) {
			t.Fatal("unsafe probe address", raw)
		}
	}
	if !publicAddress(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public rejected")
	}
	if _, e := publicProbe(context.Background(), "localhost", security.Token(), "digest"); e == nil {
		t.Fatal("localhost probe allowed")
	}
}
