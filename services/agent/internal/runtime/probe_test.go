package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"shortlink.local/policy"
	"testing"
)

func TestReadinessProbeAuthenticationAndNonceBinding(t *testing.T) {
	a, _, close := testAgent(t)
	defer close()
	if e := a.Sync(context.Background()); e != nil {
		t.Fatal(e)
	}
	req := httptest.NewRequest("GET", "/.well-known/shortlink-ready", nil)
	w := httptest.NewRecorder()
	a.ServeHTTP(w, req)
	if w.Code != 404 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("anonymous probe exposed state")
	}
	nonce := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	digest := policy.ProbeDigest(a.cfg.Credential)
	req.Header.Set("X-Shortlink-Nonce", nonce)
	req.Header.Set("X-Shortlink-Proof", policy.ProbeMAC(digest, "request", []byte(nonce)))
	w = httptest.NewRecorder()
	a.ServeHTTP(w, req)
	if w.Code != 200 || !policy.CheckProbeMAC(policy.ProbeMAC(digest, "response", w.Body.Bytes()), w.Header().Get("X-Shortlink-Proof")) {
		t.Fatal("authenticated response invalid")
	}
	var value policy.ProbeState
	json.Unmarshal(w.Body.Bytes(), &value)
	if value.Nonce != nonce || value.DomainID != a.cfg.DomainID || value.Revision != 1 || !value.Ready {
		t.Fatal("wrong readiness state", value)
	}
	req.Header.Set("X-Shortlink-Nonce", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	w = httptest.NewRecorder()
	a.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatal("proof accepted for another nonce")
	}
}
