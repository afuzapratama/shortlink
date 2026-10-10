package runtime

import (
	"encoding/json"
	"net/http"
	"shortlink.local/policy"
	"time"
)

func (a *Agent) readinessProbe(w http.ResponseWriter, r *http.Request) {
	nonce := r.Header.Get("X-Shortlink-Nonce")
	digest := policy.ProbeDigest(a.cfg.Credential)
	if r.Method != "GET" || !policy.ValidProbeNonce(nonce) || !policy.CheckProbeMAC(policy.ProbeMAC(digest, "request", []byte(nonce)), r.Header.Get("X-Shortlink-Proof")) {
		http.NotFound(w, r)
		return
	}
	value := policy.ProbeState{Nonce: nonce, DomainID: a.cfg.DomainID, Version: Version}
	if state := a.current.Load(); state != nil {
		value.Revision = state.snapshot.Revision
		value.GeoRelease = a.geo.ActiveRelease()
		value.Ready = !a.blocked.Load() && time.Now().Before(state.snapshot.LeaseExpiresAt) && a.GeoReady(state)
	}
	raw, _ := json.Marshal(value)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Shortlink-Proof", policy.ProbeMAC(digest, "response", raw))
	w.Write(raw)
}
