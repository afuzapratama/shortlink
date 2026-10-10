package httpapi

import (
	"crypto/ed25519"
	"net/http"
	"shortlink.local/control/internal/security"
	"shortlink.local/policy"
	"shortlink.local/risk"
	"strings"
	"time"
)

func (s *Server) protectionDomain(w http.ResponseWriter, r *http.Request, domain, actor string) {
	if r.Method == http.MethodGet {
		status, e := s.store.Protection(r.Context(), domain)
		if e != nil {
			s.resourceFailure(w, e, "domain_conflict")
			return
		}
		items, e := s.store.RiskSummary(r.Context(), domain)
		if e != nil {
			s.unavailable(w)
			return
		}
		s.json(w, 200, map[string]any{"status": status, "observations": items})
		return
	}
	if !s.method(w, r, http.MethodPatch) {
		return
	}
	var input struct {
		ExpectedRevision *int64            `json:"expectedRevision"`
		Settings         policy.Protection `json:"settings"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 {
		s.fail(w, 400, "validation_failed", "Expected revision is required.")
		return
	}
	status, e := s.store.SetProtection(r.Context(), domain, *input.ExpectedRevision, input.Settings, actor)
	if e != nil {
		s.policyFailure(w, e)
		return
	}
	s.json(w, 200, status)
}
func (s *Server) feedAsset(w http.ResponseWriter, r *http.Request) {
	if !s.method(w, r, http.MethodGet) {
		return
	}
	// Owner sessions may inspect the feed; runtime always uses an agent bearer.
	if r.Header.Get("Authorization") != "" {
		raw, ok := agentBearer(r)
		if !ok {
			s.fail(w, 401, "unauthorized", "Agent credential required.")
			return
		}
		if _, e := s.store.AuthenticateAgent(r.Context(), raw); e != nil {
			s.fail(w, 401, "unauthorized", "Agent credential rejected.")
			return
		}
	} else {
		if _, _, ok := s.authorize(w, r); !ok {
			return
		}
	}
	if s.cfg.RiskFeedRoot == "" {
		s.fail(w, 503, "feed_unavailable", "Risk feed is not configured.")
		return
	}
	seed, e := s.store.Seed(r.Context())
	if e != nil {
		s.unavailable(w)
		return
	}
	env, _, raw, e := risk.Read(s.cfg.RiskFeedRoot, ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey), time.Now(), true)
	if e != nil {
		s.fail(w, 503, "feed_unavailable", "Risk feed is unavailable or expired.")
		return
	}
	etag := `"` + env.Version + `"`
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(304)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(raw)
}
func (s *Server) feedStatus(w http.ResponseWriter, r *http.Request) {
	if !s.method(w, r, http.MethodGet) {
		return
	}
	result := map[string]any{"available": false, "version": nil, "publishedAt": nil, "expiresAt": nil, "torFetchedAt": nil, "reputationFetchedAt": nil, "torEntries": 0, "reputationEntries": 0}
	if s.cfg.RiskFeedRoot != "" {
		seed, e := s.store.Seed(r.Context())
		if e != nil {
			s.unavailable(w)
			return
		}
		env, f, _, e := risk.Read(s.cfg.RiskFeedRoot, ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey), time.Now(), false)
		if e == nil {
			result = map[string]any{"available": time.Now().Before(f.ExpiresAt), "version": env.Version, "publishedAt": f.PublishedAt, "expiresAt": f.ExpiresAt, "torFetchedAt": f.TorFetchedAt, "reputationFetchedAt": f.ReputationFetchedAt, "torEntries": len(f.Tor), "reputationEntries": len(f.Reputation)}
		}
	}
	s.json(w, 200, result)
}
func agentBearer(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	raw := strings.TrimPrefix(header, "Bearer ")
	return raw, raw != header && security.ValidToken(raw)
}
