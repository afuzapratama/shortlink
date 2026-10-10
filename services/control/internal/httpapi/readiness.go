package httpapi

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"shortlink.local/geoip"
	"shortlink.local/policy"
)

type readinessCheck struct {
	Code    string `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
}
type installation struct {
	Domain      store.Domain     `json:"domain"`
	AgentID     string           `json:"agentId"`
	Version     string           `json:"version"`
	GeoRelease  *string          `json:"geoRelease"`
	CheckedAt   time.Time        `json:"checkedAt"`
	Checks      []readinessCheck `json:"checks"`
	LastProbe   json.RawMessage  `json:"lastProbe"`
	digest      string
	expectedGeo *string
}

func (v *installation) add(code string, ok bool, success, failure string) {
	status, message := "fail", failure
	if ok {
		status, message = "pass", success
	}
	v.Checks = append(v.Checks, readinessCheck{code, status, message})
}
func (v installation) passes() bool {
	for _, c := range v.Checks {
		if c.Status != "pass" {
			return false
		}
	}
	return true
}
func (s *Server) progress(ctx context.Context, id string) (installation, error) {
	d, e := s.store.Domain(ctx, id)
	v := installation{Domain: d, CheckedAt: time.Now().UTC(), Checks: []readinessCheck{}}
	if e != nil {
		return v, e
	}
	var verified bool
	e = s.store.DB.QueryRow(ctx, "SELECT verified_at IS NOT NULL,probe_result FROM domains WHERE id=$1", id).Scan(&verified, &v.LastProbe)
	if e != nil {
		return v, e
	}
	v.add("dns_ownership", verified, "Kepemilikan domain terverifikasi.", "Tambahkan TXT dan selesaikan verifikasi domain.")
	e = s.store.DB.QueryRow(ctx, "SELECT id,version,geo_release,credential_hash FROM agents WHERE domain_id=$1 AND revoked_at IS NULL", id).Scan(&v.AgentID, &v.Version, &v.GeoRelease, &v.digest)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return v, e
	}
	v.add("agent_enrolled", v.AgentID != "", "Agent aktif sudah terdaftar.", "Buat token dan jalankan installer; agent dicabut memerlukan enrollment baru.")
	if d.Mode == "local" {
		v.add("heartbeat", d.Mode == "local" && d.Connection == "online" && d.LastSeenAt != nil && time.Since(*d.LastSeenAt) < 90*time.Second, "Heartbeat agent masih baru.", "Periksa layanan Docker, koneksi pusat dan credential; tunggu heartbeat.")
	} else {
		v.add("api_backend", v.expectedGeo != nil || s.cfg.GeoIPRoot != "", "Pusat API dikonfigurasi; probe publik harus menguji koneksi adapter.", "Konfigurasi GeoLite pusat untuk API.")
	}
	syncOK := d.AppliedRevision != nil && *d.AppliedRevision == d.DesiredRevision
	if d.Mode == "api" {
		if e = s.store.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM snapshots WHERE domain_id=$1 AND revision=$2)", id, d.DesiredRevision).Scan(&syncOK); e != nil {
			return v, e
		}
	}
	if d.Mode == "api" {
		v.add("config_sync", syncOK, "Snapshot API published tersedia di pusat; probe menguji revision yang dilayani adapter.", "Publish ulang konfigurasi atau periksa snapshot pusat.")
	} else {
		v.add("config_sync", syncOK, "Revision agent sama dengan revision pusat.", "Tunggu polling agent atau periksa snapshot/signature dan credential.")
	}
	if seed, err := s.store.Seed(ctx); err == nil && s.cfg.GeoIPRoot != "" {
		m, _, err := geoip.ReadManifest(s.cfg.GeoIPRoot, ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey), time.Now(), true)
		if err == nil {
			v.expectedGeo = &m.ReleaseID
		}
	}
	match := v.expectedGeo != nil && v.GeoRelease != nil && *v.expectedGeo == *v.GeoRelease
	if d.Mode == "api" {
		match = v.expectedGeo != nil
	}
	if d.Mode == "api" {
		v.add("geoip_sync", match, "GeoLite valid diproses di pusat; MMDB lokal tidak diperlukan.", "Periksa GeoLite pusat pada menu Sistem; adapter PHP tidak perlu download MMDB.")
	} else {
		v.add("geoip_sync", match, "GeoLite agent sesuai rilis pusat.", "Periksa rilis GeoLite pada menu Sistem dan download MMDB agent.")
	}

	return v, nil
}
func (s *Server) installationStatus(w http.ResponseWriter, r *http.Request, id string) {
	v, e := s.progress(r.Context(), id)
	if e != nil {
		s.enrollmentError(w, e)
		return
	}
	s.json(w, 200, v)
}
func (s *Server) probeDomain(w http.ResponseWriter, r *http.Request, id, actor string) {
	allowed, e := s.store.AllowLogin(r.Context(), security.Digest("probe:"+id), 10)
	if e != nil {
		s.unavailable(w)
		return
	}
	if !allowed {
		s.limited(w)
		return
	}
	select {
	case s.probeSlots <- struct{}{}:
		defer func() { <-s.probeSlots }()
	default:
		s.limited(w)
		return
	}
	v, e := s.progress(r.Context(), id)
	if e != nil {
		s.enrollmentError(w, e)
		return
	}
	if v.passes() {
		nonce := security.Token()
		probe := s.publicProbe
		if probe == nil {
			probe = publicProbe
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		state, err := probe(ctx, v.Domain.Hostname, nonce, v.digest)
		cancel()
		// Re-read mutable prerequisites after network I/O, before the transactional revision guard.
		latest, readErr := s.progress(r.Context(), id)
		if readErr != nil {
			s.enrollmentError(w, readErr)
			return
		}
		if latest.AgentID != v.AgentID || latest.Domain.DesiredRevision != v.Domain.DesiredRevision {
			s.fail(w, 409, "revision_conflict", "Installation changed; retry probe.")
			return
		}
		v = latest
		ok := err == nil && state.Nonce == nonce && state.DomainID == id && state.Ready && state.Revision == v.Domain.DesiredRevision && state.GeoRelease != nil && v.expectedGeo != nil && *state.GeoRelease == *v.expectedGeo
		message := "Probe belum lulus: periksa DNS publik, port 443, sertifikat TLS, proxy dan rilis agent B11. Alamat internal/redirect ditolak."
		if v.Domain.Mode == "api" {
			message = "Probe PHP belum lulus: periksa DNS/TLS publik, routing /.well-known/shortlink-ready, private bootstrap/credential, HTTPS outbound ke pusat dan GeoLite pusat. Alamat internal/redirect ditolak."
		}
		v.add("public_https", ok, "HTTPS publik dan identitas/revision agent terautentikasi berhasil.", message)
	} else {
		v.Checks = append(v.Checks, readinessCheck{"public_https", "not_applicable", "Selesaikan prasyarat instalasi/sinkronisasi sebelum probe publik."})
	}
	result := map[string]any{"checkedAt": v.CheckedAt, "checks": v.Checks}
	raw, _ := json.Marshal(result)
	d, e := s.store.RecordProbe(r.Context(), id, v.AgentID, v.Domain.DesiredRevision, v.passes(), raw, actor)
	if e != nil {
		s.resourceFailure(w, e, "revision_conflict")
		return
	}
	s.json(w, 200, map[string]any{"checkedAt": v.CheckedAt, "readiness": d.Readiness, "checks": v.Checks})
}
func publicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "240.0.0.0/4", "2002::/16", "2001::/32", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	return true
}
func publicProbe(ctx context.Context, hostname, nonce, digest string) (policy.ProbeState, error) {
	var state policy.ProbeState
	ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", hostname)
	if e != nil || len(ips) == 0 {
		return state, errors.New("public DNS unavailable")
	}
	for _, ip := range ips {
		if !publicAddress(ip) {
			return state, errors.New("private/reserved address rejected")
		}
	}
	transport := &http.Transport{TLSHandshakeTimeout: 3 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		var err error
		for _, ip := range ips {
			var conn net.Conn
			conn, err = (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), "443"))
			if err == nil {
				return conn, nil
			}
		}
		return nil, err
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 7 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("probe redirect rejected") }}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://"+hostname+"/.well-known/shortlink-ready", nil)
	req.Header.Set("X-Shortlink-Nonce", nonce)
	req.Header.Set("X-Shortlink-Proof", policy.ProbeMAC(digest, "request", []byte(nonce)))
	res, e := client.Do(req)
	if e != nil {
		return state, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return state, fmt.Errorf("probe HTTP %d", res.StatusCode)
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, (16<<10)+1))
	if e != nil || len(raw) > 16<<10 || !policy.CheckProbeMAC(policy.ProbeMAC(digest, "response", raw), res.Header.Get("X-Shortlink-Proof")) {
		return state, errors.New("probe authenticity rejected")
	}
	e = json.Unmarshal(raw, &state)
	return state, e
}
