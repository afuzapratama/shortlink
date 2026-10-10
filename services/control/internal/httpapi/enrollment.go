package httpapi

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"shortlink.local/artifacts"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"shortlink.local/geoip"
	"shortlink.local/risk"
)

func (s *Server) enrollmentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrPrerequisite):
		s.fail(w, 422, "preflight_failed", "Verify domain ownership and revoke any existing active agent first.")
	case errors.Is(err, store.ErrBinding):
		s.fail(w, 422, "domain_mismatch", "Domain, method, installation or revision does not match.")
	case errors.Is(err, store.ErrExpired):
		s.fail(w, 410, "enrollment_expired", "Enrollment token or verification challenge expired.")
	case errors.Is(err, store.ErrUsed):
		s.fail(w, 409, "enrollment_used", "Token is consumed, revoked or recovery proof does not match.")
	default:
		s.resourceFailure(w, err, "enrollment_used")
	}
}
func (s *Server) bootstrap(r *http.Request, d store.Domain) (map[string]any, error) {
	seed, err := s.store.Seed(r.Context())
	if err != nil {
		return nil, err
	}
	method := "cli"
	if d.Mode == "api" {
		method = "php_zip"
	}
	key := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	version := "unreleased"
	if d.Mode == "local" && s.cfg.AgentAssetRoot != "" {
		if e, err := artifacts.Read(s.cfg.AgentAssetRoot, key, ""); err == nil {
			version = e.Version
		}
	}
	if d.Mode == "api" && s.cfg.PHPAssetRoot != "" {
		if e, err := artifacts.ReadPHP(s.cfg.PHPAssetRoot, key, ""); err == nil {
			version = e.Version
		}
	}
	b := map[string]any{"domainId": d.ID, "hostname": d.Hostname, "controlPlaneUrl": s.publicURL(), "fallbackUrls": []string{}, "manifestVerificationKey": base64.RawURLEncoding.EncodeToString(key), "artifactVersion": version, "method": method}
	if d.Mode == "api" {
		b["publicUrl"] = "https://" + d.Hostname
		b["trustedProxies"] = []string{}
		b["developmentLoopback"] = false
	}
	return b, nil
}
func (s *Server) enrollmentAdmin(w http.ResponseWriter, r *http.Request, actor string) bool {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/admin/"), "/")
	if len(parts) == 1 && parts[0] == "install-tokens" {
		if !s.method(w, r, http.MethodPost) {
			return true
		}
		var input struct {
			DomainID string `json:"domainId"`
			Method   string `json:"method"`
		}
		if !s.decode(w, r, &input) {
			return true
		}
		t, err := s.store.IssueToken(r.Context(), input.DomainID, input.Method, actor)
		if err != nil {
			s.enrollmentError(w, err)
			return true
		}
		t.ControlPlaneURL = s.publicURL()
		s.json(w, 201, t)
		return true
	}
	if len(parts) == 3 && parts[0] == "agents" && parts[2] == "revoke" {
		if !s.method(w, r, http.MethodPost) {
			return true
		}
		if err := s.store.Revoke(r.Context(), parts[1], actor); err != nil {
			s.enrollmentError(w, err)
			return true
		}
		s.json(w, 200, map[string]bool{"ok": true})
		return true
	}
	if len(parts) != 3 || parts[0] != "domains" {
		return false
	}
	domain := parts[1]
	switch parts[2] {
	case "protection":
		s.protectionDomain(w, r, domain, actor)
		return true
	case "installation":
		if s.method(w, r, http.MethodGet) {
			s.installationStatus(w, r, domain)
		}
		return true
	case "probe":
		if s.method(w, r, http.MethodPost) {
			s.probeDomain(w, r, domain, actor)
		}
		return true
	case "verification":
		if !s.method(w, r, http.MethodPost) {
			return true
		}
		c, err := s.store.Challenge(r.Context(), domain, actor)
		if err != nil {
			s.enrollmentError(w, err)
			return true
		}
		s.json(w, 200, c)
		return true
	case "verify":
		if !s.method(w, r, http.MethodPost) {
			return true
		}
		allowed, err := s.store.AllowLogin(r.Context(), security.Digest("verify:"+domain), 10)
		if err != nil {
			s.unavailable(w)
			return true
		}
		if !allowed {
			s.limited(w)
			return true
		}
		c, err := s.store.Challenge(r.Context(), domain, actor)
		if err != nil {
			s.enrollmentError(w, err)
			return true
		}
		values, lookupErr := s.lookupTXT(r.Context(), c.Name)
		matched := false
		if lookupErr == nil {
			for _, value := range values {
				if security.Equal(value, c.Value) {
					matched = true
					break
				}
			}
		}
		if matched {
			if err = s.store.Verify(r.Context(), domain, c.Value, actor); err != nil {
				s.enrollmentError(w, err)
				return true
			}
		}
		d, err := s.store.Domain(r.Context(), domain)
		if err != nil {
			s.enrollmentError(w, err)
			return true
		}
		status, message := "fail", "DNS TXT belum cocok atau belum tersebar."
		if matched {
			status, message = "pass", "Kepemilikan domain terverifikasi."
		}
		s.json(w, 200, map[string]any{"checkedAt": time.Now().UTC(), "readiness": d.Readiness, "checks": []map[string]string{{"code": "dns_txt", "status": status, "message": message}}})
		return true
	case "bootstrap":
		if !s.method(w, r, http.MethodGet) {
			return true
		}
		d, err := s.store.Domain(r.Context(), domain)
		if err != nil {
			s.enrollmentError(w, err)
			return true
		}
		b, err := s.bootstrap(r, d)
		if err != nil {
			s.unavailable(w)
			return true
		}
		s.json(w, 200, b)
		return true
	case "agent":
		if !s.method(w, r, http.MethodGet) {
			return true
		}
		_, err := s.store.Domain(r.Context(), domain)
		if err != nil {
			s.enrollmentError(w, err)
			return true
		}
		var id, method, version string
		err = s.store.DB.QueryRow(r.Context(), "SELECT id,method,version FROM agents WHERE domain_id=$1 AND revoked_at IS NULL", domain).Scan(&id, &method, &version)
		if errors.Is(err, pgx.ErrNoRows) {
			s.json(w, 200, map[string]any{"agent": nil})
			return true
		}
		if err != nil {
			s.unavailable(w)
			return true
		}
		s.json(w, 200, map[string]any{"agent": map[string]string{"id": id, "method": method, "version": version}})
		return true
	case "audit":
		if !s.method(w, r, http.MethodGet) {
			return true
		}
		_, err := s.store.Domain(r.Context(), domain)
		if err != nil {
			s.enrollmentError(w, err)
			return true
		}
		rows, err := s.store.DB.Query(r.Context(), "SELECT actor_id,action,occurred_at FROM audit_logs WHERE (domain_id=$1 OR domain_id IS NULL) ORDER BY id DESC LIMIT 50", domain)
		if err != nil {
			s.unavailable(w)
			return true
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var actor, action string
			var at time.Time
			if err = rows.Scan(&actor, &action, &at); err != nil {
				s.unavailable(w)
				return true
			}
			items = append(items, map[string]any{"actorId": actor, "action": action, "occurredAt": at})
		}
		if rows.Err() != nil {
			s.unavailable(w)
			return true
		}
		s.json(w, 200, map[string]any{"items": items})
		return true
	}
	return false
}
func (s *Server) installContext(w http.ResponseWriter, r *http.Request) {
	if !s.method(w, r, http.MethodPost) {
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	allowed, err := s.store.AllowLogin(r.Context(), security.Digest("install-context:"+ip), 30)
	if err != nil {
		s.unavailable(w)
		return
	}
	if !allowed {
		s.limited(w)
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	if !security.ValidToken(input.Token) {
		s.fail(w, 400, "validation_failed", "Provide a valid installation token.")
		return
	}
	installation, d, err := s.store.InstallContext(r.Context(), input.Token)
	if err != nil {
		s.enrollmentError(w, err)
		return
	}
	b, err := s.bootstrap(r, d)
	if err != nil {
		s.unavailable(w)
		return
	}
	s.json(w, 200, map[string]any{"installationId": installation, "bootstrap": b})
}
func (s *Server) enrollAgent(w http.ResponseWriter, r *http.Request) {
	if !s.method(w, r, http.MethodPost) {
		return
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	allowed, err := s.store.AllowLogin(r.Context(), security.Digest("enroll-ip:"+ip), 30)
	if err != nil {
		s.unavailable(w)
		return
	}
	if !allowed {
		s.limited(w)
		return
	}
	var input struct {
		Token             string `json:"token"`
		InstallationID    string `json:"installationId"`
		InstallationProof string `json:"installationProof"`
		Hostname          string `json:"hostname"`
		Method            string `json:"method"`
		Version           string `json:"version"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	if !security.ValidToken(input.Token) || !security.ValidToken(input.InstallationID) || !security.ValidToken(input.InstallationProof) || len(input.Version) < 1 || len(input.Version) > 128 {
		s.fail(w, 400, "validation_failed", "Provide a valid token, installation ID and random 32-byte proof.")
		return
	}
	a, secret, err := s.store.Enroll(r.Context(), input.Token, input.InstallationID, input.InstallationProof, input.Hostname, input.Method, input.Version)
	if err != nil {
		s.enrollmentError(w, err)
		return
	}
	d, err := s.store.Domain(r.Context(), a.DomainID)
	if err != nil {
		s.unavailable(w)
		return
	}
	b, err := s.bootstrap(r, d)
	if err != nil {
		s.unavailable(w)
		return
	}
	s.json(w, 200, map[string]any{"agentId": a.ID, "credential": secret, "configUrl": s.publicURL() + "/v1/agents/config", "capabilities": d.Capabilities, "bootstrap": b})
}
func (s *Server) agentRequest(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if r.Header.Get("Authorization") == raw || !security.ValidToken(raw) {
		s.fail(w, 401, "unauthorized", "Agent credential is required.")
		return
	}
	a, err := s.store.AuthenticateAgent(r.Context(), raw)
	if errors.Is(err, pgx.ErrNoRows) {
		s.fail(w, 401, "unauthorized", "Agent credential is revoked or invalid.")
		return
	}
	if err != nil {
		s.unavailable(w)
		return
	}
	switch r.URL.Path {
	case "/v1/decisions":
		s.makeDecision(w, r, a)
	case "/v1/agents/probe":
		s.adapterProbe(w, r, a)
	case "/v1/events/batch":
		s.ingestEvents(w, r, a)
	case "/v1/agents/config":
		if !s.method(w, r, http.MethodGet) {
			return
		}
		payload, signature, etag, err := s.store.AgentSnapshot(r.Context(), a)
		if err != nil {
			s.enrollmentError(w, err)
			return
		}
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(304)
			return
		}
		var value map[string]any
		if json.Unmarshal(payload, &value) != nil {
			s.unavailable(w)
			return
		}
		value["signature"] = signature
		value["signedPayload"] = base64.RawURLEncoding.EncodeToString(payload)
		s.json(w, 200, value)
	case "/v1/agents/heartbeat":
		if !s.method(w, r, http.MethodPost) {
			return
		}
		var h struct {
			Version           string  `json:"version"`
			AppliedRevision   *int64  `json:"appliedRevision"`
			GeoRelease        *string `json:"geoRelease"`
			FeedVersion       *string `json:"feedVersion"`
			QueueDepth        *int64  `json:"queueDepth"`
			QueueBytes        *int64  `json:"queueBytes"`
			DroppedEvents     *int64  `json:"droppedEvents"`
			BufferedEvents    int64   `json:"bufferedEvents"`
			QueueWriteErrors  int64   `json:"queueWriteErrors"`
			ProtectionVersion int     `json:"protectionVersion"`
		}
		if !s.decode(w, r, &h) {
			return
		}
		if (h.FeedVersion != nil && !risk.ValidVersion(*h.FeedVersion)) || (h.GeoRelease != nil && !geoip.ValidRelease(*h.GeoRelease)) || h.ProtectionVersion < 0 || h.ProtectionVersion > 1 || h.BufferedEvents < 0 || h.QueueWriteErrors < 0 || h.AppliedRevision == nil || h.QueueDepth == nil || h.QueueBytes == nil || h.DroppedEvents == nil || *h.AppliedRevision < 0 || *h.QueueDepth < 0 || *h.QueueBytes < 0 || *h.DroppedEvents < 0 || len(h.Version) < 1 || len(h.Version) > 128 {
			s.fail(w, 400, "validation_failed", "Heartbeat fields are invalid.")
			return
		}
		if err = s.store.Heartbeat(r.Context(), a, *h.AppliedRevision, h.Version, *h.QueueDepth, *h.QueueBytes, *h.DroppedEvents, h.GeoRelease, h.FeedVersion, store.QueueHealth{Buffered: h.BufferedEvents, WriteErrors: h.QueueWriteErrors, ProtectionVersion: h.ProtectionVersion}); err != nil {
			s.enrollmentError(w, err)
			return
		}
		s.json(w, 200, map[string]bool{"ok": true})
	}
}
