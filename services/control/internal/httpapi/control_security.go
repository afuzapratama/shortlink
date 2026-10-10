package httpapi

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"strings"
)

func (s *Server) controlAdmin(w http.ResponseWriter, r *http.Request, owner store.Owner) bool {
	if r.URL.Path != "/v1/admin/control/agents" && r.URL.Path != "/v1/admin/control/migrations" && r.URL.Path != "/v1/admin/control/rotations" {
		return false
	}
	if r.URL.Path == "/v1/admin/control/agents" {
		if !s.method(w, r, http.MethodGet) {
			return true
		}
		states, err := s.store.ControlStates(r.Context())
		if err != nil {
			s.unavailable(w)
		} else {
			var latest string
			if err = s.store.DB.QueryRow(r.Context(), "SELECT id FROM control_migrations WHERE expires_at>now() ORDER BY created_at DESC LIMIT 1").Scan(&latest); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				s.unavailable(w)
				return true
			}
			s.json(w, 200, map[string]any{"items": states, "migrationId": latest})
		}
		return true
	}
	if !s.method(w, r, http.MethodPost) {
		return true
	}
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
		AgentID  string `json:"agentId"`
		Origin   string `json:"origin"`
	}
	if !s.decode(w, r, &in) || !s.reauthenticate(w, r, owner, in.Password) {
		return true
	}
	var err error
	if r.URL.Path == "/v1/admin/control/rotations" {
		err = s.store.StageRotation(r.Context(), owner.ID, in.Code, in.AgentID)
		if err == nil {
			s.json(w, 200, map[string]bool{"ok": true})
			return true
		}
	} else {
		u, e := url.Parse(in.Origin)
		valid := e == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "" && u.Scheme == "https"
		if e == nil && s.cfg.Development && u.Scheme == "http" {
			ip, e := netip.ParseAddr(u.Hostname())
			valid = e == nil && ip.IsLoopback() && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == ""
		}
		if !valid {
			s.fail(w, 400, "validation_failed", "A trusted HTTPS control origin is required.")
			return true
		}
		var doc store.SignedMigration
		doc, err = s.store.StageMigration(r.Context(), owner.ID, in.Code, in.Origin)
		if err == nil {
			s.json(w, 200, doc)
			return true
		}
	}
	if errors.Is(err, store.ErrUsed) {
		s.fail(w, 409, "rotation_pending", "An unexpired credential rotation already exists.")
	} else if errors.Is(err, pgx.ErrNoRows) {
		s.fail(w, 404, "not_found", "Active agent not found.")
	} else {
		s.factorFailure(w, err)
	}
	return true
}
func (s *Server) controlAgent(w http.ResponseWriter, r *http.Request) {
	raw, ok := agentBearer(r)
	if !ok {
		s.fail(w, 401, "unauthorized", "Agent credential required.")
		return
	}
	a, err := s.store.AuthenticateAgent(r.Context(), raw)
	if err != nil {
		s.fail(w, 401, "unauthorized", "Agent credential invalid.")
		return
	}
	if r.URL.Path == "/v1/agents/control" {
		if !s.method(w, r, http.MethodGet) {
			return
		}
		result, err := s.store.AgentControl(r.Context(), a)
		if err != nil {
			s.unavailable(w)
		} else {
			s.json(w, 200, result)
		}
		return
	}
	if !s.method(w, r, http.MethodPost) {
		return
	}
	var in struct {
		MigrationID string `json:"migrationId"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	if err = s.store.AckControl(r.Context(), a, raw, in.MigrationID, strings.TrimRight(s.cfg.PublicURL, "/")); err != nil {
		s.factorFailure(w, err)
		return
	}
	s.json(w, 200, map[string]bool{"ok": true})
}

// Public nonce proof authenticates the destination before an operator sends a bearer.
func (s *Server) controlIdentity(w http.ResponseWriter, r *http.Request) {
	if !s.method(w, r, http.MethodPost) {
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	allowed, err := s.store.AllowLogin(r.Context(), security.Digest("control-identity:"+ip), 300)
	if err != nil {
		s.unavailable(w)
		return
	}
	if !allowed {
		s.limited(w)
		return
	}
	var in struct {
		Nonce string `json:"nonce"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	if !security.ValidToken(in.Nonce) {
		s.fail(w, 400, "validation_failed", "Random nonce required.")
		return
	}
	seed, err := s.store.Seed(r.Context())
	if err != nil {
		s.unavailable(w)
		return
	}
	payload, _ := json.Marshal(map[string]string{"purpose": "shortlink-control-identity-v1", "nonce": in.Nonce, "origin": strings.TrimRight(s.cfg.PublicURL, "/")})
	doc := store.SignedMigration{Payload: base64.RawURLEncoding.EncodeToString(payload), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed), payload))}
	s.json(w, 200, doc)
}
