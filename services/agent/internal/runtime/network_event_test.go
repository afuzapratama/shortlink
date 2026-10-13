package runtime

import (
	"context"
	"crypto/ed25519"
	"net/http/httptest"
	"path/filepath"
	"shortlink.local/geoip"
	"shortlink.local/geoip/testutil"
	"testing"
	"time"
)

func TestRedirectRecordsVisitorIPAndGeoLiteASN(t *testing.T) {
	a, c, closeServer := testAgent(t)
	defer closeServer()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(a.cfg.DataDir, "geoip")
	if err := testutil.Staging(root, time.Now().Add(-time.Hour).Unix(), "ID"); err != nil {
		t.Fatal(err)
	}
	if _, err := geoip.Publish(root, c.private.Seed(), time.Now()); err != nil {
		t.Fatal(err)
	}
	m, err := geoip.NewManager(root, c.private.Public().(ed25519.PublicKey), "", "")
	if err != nil {
		t.Fatal(err)
	}
	a.geo.Close()
	a.geo = m
	r := httptest.NewRequest("GET", "/hello", nil)
	r.Host = a.cfg.Hostname
	r.RemoteAddr = "1.1.1.1:12345"
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 302 {
		t.Fatal(w.Code)
	}
	until := time.Now().Add(3 * time.Second)
	for {
		events, err := a.queue.Batch(context.Background(), 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) > 0 {
			e := events[0]
			if e.ClientIP != "1.1.1.1" || e.ASN == nil || *e.ASN != 64512 || e.ASNOrganization != "TEST NETWORK" {
				t.Fatal("visitor network metadata missing")
			}
			break
		}
		if time.Now().After(until) {
			t.Fatal("event not persisted")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
