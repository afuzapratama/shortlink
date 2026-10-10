package httpapi

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
)

func (s *Server) factorFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrFactor) || errors.Is(err, store.ErrUsed) {
		s.fail(w, 401, "factor_invalid", "Authentication factor is invalid, expired or already used.")
	} else {
		s.unavailable(w)
	}
}
func (s *Server) verifyMFA(w http.ResponseWriter, r *http.Request) {
	if !s.method(w, r, http.MethodPost) || !s.origin(w, r) {
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	ok, err := s.store.AllowLogin(r.Context(), security.Digest("mfa-ip:"+ip), 30)
	if err != nil {
		s.unavailable(w)
		return
	}
	if !ok {
		s.limited(w)
		return
	}
	var in struct {
		ChallengeID string `json:"challengeId"`
		Code        string `json:"code"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	if !security.ValidToken(in.ChallengeID) || len(in.Code) > 43 {
		s.fail(w, 400, "validation_failed", "Valid challenge and factor required.")
		return
	}
	token := security.Token()
	if err = s.store.VerifyMFAChallenge(r.Context(), security.Digest(in.ChallengeID), in.Code, security.Digest(token), s.cfg.SessionTTL); err != nil {
		s.factorFailure(w, err)
		return
	}
	s.cookie(w, token, int(s.cfg.SessionTTL.Seconds()))
	w.Header().Set("X-CSRF-Token", security.CSRF(token))
	s.json(w, 200, map[string]any{"status": "authenticated", "challengeId": nil})
}

// Password reauthentication is required for provisioning/security changes, in addition to CSRF/session.
func (s *Server) reauthenticate(w http.ResponseWriter, r *http.Request, owner store.Owner, password string) bool {
	ok, err := s.store.AllowLogin(r.Context(), security.Digest("reauth:"+owner.ID), 10)
	if err != nil {
		s.unavailable(w)
		return false
	}
	if !ok {
		s.limited(w)
		return false
	}
	select {
	case s.hashSlots <- struct{}{}:
		defer func() { <-s.hashSlots }()
	default:
		s.limited(w)
		return false
	}
	o, err := s.store.OwnerByEmail(r.Context(), owner.Email)
	if err != nil {
		s.unavailable(w)
		return false
	}
	if !security.ValidPassword(password) || !security.VerifyPassword(password, o.PasswordHash) {
		s.fail(w, 401, "factor_invalid", "Reauthentication failed.")
		return false
	}
	return true
}
func (s *Server) securityAdmin(w http.ResponseWriter, r *http.Request, owner store.Owner, token string) bool {
	if r.URL.Path != "/v1/admin/security" && r.URL.Path != "/v1/admin/security/mfa/setup" && r.URL.Path != "/v1/admin/security/mfa/confirm" && r.URL.Path != "/v1/admin/security/mfa/disable" && r.URL.Path != "/v1/admin/security/mfa/recovery" {
		return false
	}
	if r.URL.Path == "/v1/admin/security" {
		if !s.method(w, r, http.MethodGet) {
			return true
		}
		var n int
		if err := s.store.DB.QueryRow(r.Context(), "SELECT count(*) FROM mfa_recovery WHERE owner_id=$1", owner.ID).Scan(&n); err != nil {
			s.unavailable(w)
			return true
		}
		s.json(w, 200, map[string]any{"mfaEnabled": owner.MFAEnabled, "recoveryRemaining": n})
		return true
	}
	if !s.method(w, r, http.MethodPost) {
		return true
	}
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !s.decode(w, r, &in) {
		return true
	}
	if !s.reauthenticate(w, r, owner, in.Password) {
		return true
	}
	if r.URL.Path == "/v1/admin/security/mfa/setup" {
		secret, err := s.store.BeginMFA(r.Context(), owner.ID)
		if err != nil {
			s.factorFailure(w, err)
			return true
		}
		uri := "otpauth://totp/" + url.PathEscape("Shortlink:"+owner.Email) + "?secret=" + secret + "&issuer=Shortlink&algorithm=SHA1&digits=6&period=30"
		s.json(w, 200, map[string]any{"secret": secret, "uri": uri, "expiresIn": 600})
		return true
	}
	if len(in.Code) > 43 {
		s.fail(w, 400, "validation_failed", "Factor too long.")
		return true
	}
	var codes []string
	var err error
	if r.URL.Path == "/v1/admin/security/mfa/confirm" {
		codes, err = s.store.ConfirmMFA(r.Context(), owner.ID, in.Code, security.Digest(token))
	} else {
		codes, err = s.store.ChangeMFA(r.Context(), owner.ID, in.Code, security.Digest(token), r.URL.Path == "/v1/admin/security/mfa/disable")
	}
	if err != nil {
		s.factorFailure(w, err)
		return true
	}
	s.json(w, 200, map[string]any{"ok": true, "recoveryCodes": codes})
	return true
}

func (s *Server) loginChallenge(w http.ResponseWriter, r *http.Request, owner store.Owner) {
	challenge := security.Token()
	old := ""
	if cookie, err := r.Cookie(cookieName); err == nil && security.ValidToken(cookie.Value) {
		old = security.Digest(cookie.Value)
	}
	if err := s.store.MFAChallenge(r.Context(), owner.ID, security.Digest(challenge), old); err != nil {
		s.unavailable(w)
		return
	}
	s.cookie(w, "", -1)
	s.json(w, 200, map[string]any{"status": "mfa_required", "challengeId": challenge})
}
