package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"shortlink.local/control/internal/config"
	"shortlink.local/control/internal/proxycheck"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"shortlink.local/policy"
)

const cookieName = "shortlink_session"

type Server struct {
	cfg             config.Config
	store           *store.Store
	logger          *slog.Logger
	dummyHash       string
	hashSlots       chan struct{}
	lookupTXT       func(context.Context, string) ([]string, error)
	geoSlots        chan struct{}
	artifactSlots   chan struct{}
	probeSlots      chan struct{}
	trafficSlots    chan struct{}
	decisionSlots   chan struct{}
	decisionWaiting chan struct{}
	proxy           *proxycheck.Client
	decisions       decisionRuntime
	publicProbe     func(context.Context, string, string, string) (policy.ProbeState, error)
}

func New(cfg config.Config, s *store.Store, logger *slog.Logger) *Server {
	return &Server{proxy: proxycheck.New(s.ProxyConfig, s.ReserveProxyQuery, nil), cfg: cfg, store: s, logger: logger, dummyHash: security.HashPassword(security.Token()), hashSlots: make(chan struct{}, 2), lookupTXT: net.DefaultResolver.LookupTXT, geoSlots: make(chan struct{}, 2), artifactSlots: make(chan struct{}, 2), probeSlots: make(chan struct{}, 2), trafficSlots: make(chan struct{}, 2), decisionSlots: make(chan struct{}, 16), decisionWaiting: make(chan struct{}, 16)}
}
func NormalizeEmail(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	a, err := mail.ParseAddress(value)
	return value, err == nil && a.Address == value && len(value) <= 254 && strings.Contains(value, "@")
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	id := security.Token()
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	if !s.cfg.Development {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	// Log route group only: URL/query/header/body may contain untrusted or private data.
	group := "unknown"
	if strings.HasPrefix(r.URL.Path, "/v1/admin/") {
		group = "admin"
	} else if strings.HasPrefix(r.URL.Path, "/v1/auth/") {
		group = "auth"
	} else if strings.HasPrefix(r.URL.Path, "/health/") {
		group = "health"
	}
	defer func() {
		s.logger.Info("request", "requestId", id, "group", group, "method", r.Method, "durationMs", time.Since(started).Milliseconds())
	}()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	switch r.URL.Path {
	case "/health/live":
		if !s.method(w, r, http.MethodGet) {
			return
		}
		s.json(w, 200, map[string]bool{"ok": true})
	case "/health/ready":
		if !s.method(w, r, http.MethodGet) {
			return
		}
		if err := s.store.Ready(r.Context()); err != nil {
			s.fail(w, 503, "control_unavailable", "Control service is not ready.")
			return
		}
		s.json(w, 200, map[string]bool{"ok": true})
	case "/v1/auth/login":
		if !s.method(w, r, http.MethodPost) || !s.origin(w, r) {
			return
		}
		s.login(w, r)
	case "/v1/auth/session":
		if !s.method(w, r, http.MethodGet) {
			return
		}
		o, token, ok := s.authorize(w, r)
		if !ok {
			return
		}
		s.json(w, 200, map[string]any{"userId": o.ID, "email": o.Email, "role": "owner", "mfaEnabled": o.MFAEnabled, "csrfToken": security.CSRF(token)})
	case "/v1/auth/logout":
		if !s.method(w, r, http.MethodPost) {
			return
		}
		_, token, ok := s.authorize(w, r)
		if !ok || !s.mutation(w, r, token) {
			return
		}
		if err := s.store.DeleteSession(r.Context(), security.Digest(token)); err != nil {
			s.unavailable(w)
			return
		}
		s.cookie(w, "", -1)
		s.json(w, 200, map[string]bool{"ok": true})
	case "/v1/auth/mfa/verify":
		s.verifyMFA(w, r)
	case "/v1/control/identity":
		s.controlIdentity(w, r)
	case "/v1/agents/control", "/v1/agents/control/ack":
		s.controlAgent(w, r)
	case "/v1/agents/enroll":
		s.enrollAgent(w, r)
	case "/v1/agents/install-context":
		s.installContext(w, r)
	case "/v1/agents/opening", "/v1/agents/manage/exchange", "/v1/agents/manage/session", "/v1/agents/proxycheck", "/v1/agents/config", "/v1/agents/heartbeat", "/v1/events/batch", "/v1/decisions", "/v1/agents/probe":
		s.agentRequest(w, r)
	default:
		if r.URL.Path == "/v1/assets/php/manifest" || strings.HasPrefix(r.URL.Path, "/v1/assets/php/releases/") {
			s.phpAsset(w, r)
			return
		}
		if r.URL.Path == "/v1/assets/feeds/manifest" {
			s.feedAsset(w, r)
			return
		}
		if r.URL.Path == "/v1/assets/agent/manifest" || strings.HasPrefix(r.URL.Path, "/v1/assets/agent/releases/") {
			s.artifactAsset(w, r)
			return
		}
		if r.URL.Path == "/v1/assets/geoip/manifest" || strings.HasPrefix(r.URL.Path, "/v1/assets/geoip/releases/") {
			s.geoAsset(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/admin/") {
			owner, token, ok := s.authorize(w, r)
			if !ok {
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead && !s.mutation(w, r, token) {
				return
			}
			if s.domainManageAdmin(w, r, owner.ID) {
				return
			}
			if s.controlAdmin(w, r, owner) {
				return
			}
			if s.securityAdmin(w, r, owner, token) {
				return
			}
			if r.URL.Path == "/v1/admin/system/proxycheck" || r.URL.Path == "/v1/admin/system/proxycheck/test" {
				s.proxyAdmin(w, r, owner.ID)
				return
			}
			if r.URL.Path == "/v1/admin/system/feeds" {
				s.feedStatus(w, r)
				return
			}
			if r.URL.Path == "/v1/admin/system/events" {
				if !s.method(w, r, http.MethodGet) {
					return
				}
				h, err := s.store.EventHealth(r.Context())
				if err != nil {
					s.unavailable(w)
					return
				}
				s.json(w, 200, h)
				return
			}
			if r.URL.Path == "/v1/admin/system/geoip" {
				s.geoStatus(w, r)
				return
			}
			if s.trafficAdmin(w, r) {
				return
			}
			if !s.enrollmentAdmin(w, r, owner.ID) && !s.policyAdmin(w, r, owner.ID) {
				s.management(w, r, owner.ID)
			}
			return
		}
		// Unimplemented agent/assets routes never accept an admin cookie as an agent credential.
		if strings.HasPrefix(r.URL.Path, "/v1/agents/") || strings.HasPrefix(r.URL.Path, "/v1/assets/") || r.URL.Path == "/v1/events/batch" || r.URL.Path == "/v1/decisions" {
			s.fail(w, 401, "unauthorized", "Agent authentication is not available yet.")
			return
		}
		s.fail(w, 404, "not_found", "Resource not found.")
	}
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	// Rate limits use socket IP. Forwarded headers are not trusted in this batch.
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	allowed, err := s.store.AllowLogin(r.Context(), security.Digest("ip:"+ip), 30)
	if err != nil {
		s.unavailable(w)
		return
	}
	if !allowed {
		s.limited(w)
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	email, valid := NormalizeEmail(input.Email)
	if !valid || !security.ValidPassword(input.Password) {
		s.fail(w, 400, "validation_failed", "Provide an email and a password of 12–256 bytes.")
		return
	}
	allowed, err = s.store.AllowLogin(r.Context(), security.Digest("email:"+email), 10)
	if err != nil {
		s.unavailable(w)
		return
	}
	if !allowed {
		s.limited(w)
		return
	}
	select {
	case s.hashSlots <- struct{}{}:
		defer func() { <-s.hashSlots }()
	default:
		s.limited(w)
		return
	}
	owner, err := s.store.OwnerByEmail(r.Context(), email)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.unavailable(w)
		return
	}
	hash := s.dummyHash
	if err == nil {
		hash = owner.PasswordHash
	}
	matched := security.VerifyPassword(input.Password, hash)
	if !matched || err != nil {
		s.fail(w, 401, "unauthorized", "Email or password is incorrect.")
		return
	}
	if owner.MFAEnabled {
		s.loginChallenge(w, r, owner)
		return
	}
	token := security.Token()
	oldHash := ""
	if old, err := r.Cookie(cookieName); err == nil && security.ValidToken(old.Value) {
		oldHash = security.Digest(old.Value)
	}
	if err = s.store.RotateSession(r.Context(), security.Digest(token), owner.ID, oldHash, s.cfg.SessionTTL); err != nil {
		if errors.Is(err, store.ErrMFARequired) {
			s.loginChallenge(w, r, owner)
			return
		}
		s.unavailable(w)
		return
	}
	s.cookie(w, token, int(s.cfg.SessionTTL.Seconds()))
	w.Header().Set("X-CSRF-Token", security.CSRF(token))
	s.json(w, 200, map[string]any{"status": "authenticated", "challengeId": nil})
}
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) (store.Owner, string, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil || !security.ValidToken(c.Value) {
		s.fail(w, 401, "unauthorized", "An admin session is required.")
		return store.Owner{}, "", false
	}
	o, err := s.store.Session(r.Context(), security.Digest(c.Value))
	if errors.Is(err, pgx.ErrNoRows) {
		s.cookie(w, "", -1)
		s.fail(w, 401, "unauthorized", "Session expired or revoked.")
		return o, "", false
	}
	if err != nil {
		s.unavailable(w)
		return o, "", false
	}
	return o, c.Value, true
}
func (s *Server) origin(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") != s.cfg.Origin || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		s.fail(w, 403, "forbidden", "Request origin is not allowed.")
		return false
	}
	return true
}
func (s *Server) mutation(w http.ResponseWriter, r *http.Request, token string) bool {
	if !s.origin(w, r) {
		return false
	}
	if !security.Equal(r.Header.Get("X-CSRF-Token"), security.CSRF(token)) {
		s.fail(w, 403, "forbidden", "A valid CSRF token is required.")
		return false
	}
	return true
}
func (s *Server) method(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		w.Header().Set("Allow", method)
		s.fail(w, 405, "method_not_allowed", "Method not allowed.")
		return false
	}
	return true
}
func (s *Server) decode(w http.ResponseWriter, r *http.Request, target any) bool {
	return s.decodeLimit(w, r, target, 8*1024)
}
func (s *Server) decodeLimit(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typ != "application/json" {
		s.fail(w, 415, "validation_failed", "Content-Type must be application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err = dec.Decode(target)
	if err == nil {
		var extra any
		err = dec.Decode(&extra)
		if errors.Is(err, io.EOF) {
			return true
		}
		if err == nil {
			err = errors.New("extra JSON value")
		}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		s.fail(w, 413, "validation_failed", "Request body is too large.")
	} else {
		s.fail(w, 400, "validation_failed", "Invalid JSON request.")
	}
	return false
}
func (s *Server) cookie(w http.ResponseWriter, value string, maxAge int) {
	c := &http.Cookie{Name: cookieName, Value: value, Path: "/", HttpOnly: true, Secure: !s.cfg.Development, SameSite: http.SameSiteStrictMode, MaxAge: maxAge}
	if maxAge < 0 {
		c.Expires = time.Unix(1, 0)
	} else {
		c.Expires = time.Now().Add(s.cfg.SessionTTL)
	}
	http.SetCookie(w, c)
}
func (s *Server) limited(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "900")
	s.fail(w, 429, "rate_limited", "Login is temporarily rate limited.")
}
func (s *Server) unavailable(w http.ResponseWriter) {
	s.fail(w, 503, "control_unavailable", "Control service is temporarily unavailable.")
}
func (s *Server) fail(w http.ResponseWriter, status int, code, message string) {
	s.json(w, status, map[string]any{"error": map[string]string{"code": code, "message": message, "requestId": w.Header().Get("X-Request-ID")}})
}
func (s *Server) json(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
