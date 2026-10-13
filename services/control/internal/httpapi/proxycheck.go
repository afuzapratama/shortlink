package httpapi

import (
	"net/http"
	"net/netip"
	"shortlink.local/control/internal/proxycheck"
	"shortlink.local/control/internal/store"
)

func (s *Server) proxyAdmin(w http.ResponseWriter, r *http.Request, owner string) {
	if r.URL.Path == "/v1/admin/system/proxycheck/test" {
		if !s.method(w, r, http.MethodPost) {
			return
		}
		var input struct {
			IP string `json:"ip"`
		}
		if !s.decode(w, r, &input) {
			return
		}
		ip, err := netip.ParseAddr(input.IP)
		if err != nil || !proxycheck.Public(ip) {
			s.fail(w, 400, "validation_failed", "Use a public IP address.")
			return
		}
		s.json(w, 200, s.proxy.Lookup(r.Context(), ip))
		return
	}
	switch r.Method {
	case http.MethodGet:
		settings, err := s.store.ProxySettings(r.Context())
		if err != nil {
			s.unavailable(w)
			return
		}
		s.json(w, 200, map[string]any{"settings": settings, "lastStatus": s.proxy.Status()})
	case http.MethodPatch:
		var input struct {
			Provider         string `json:"provider"`
			BaseURL          string `json:"baseUrl"`
			ExpectedRevision *int64 `json:"expectedRevision"`
			Enabled          bool   `json:"enabled"`
			DailyBudget      int    `json:"dailyBudget"`
			APIKey           string `json:"apiKey"`
			RemoveKey        bool   `json:"removeKey"`
		}
		if !s.decode(w, r, &input) {
			return
		}
		if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 {
			s.fail(w, 400, "validation_failed", "expectedRevision required.")
			return
		}
		if err := s.store.UpdateIPQualitySettings(r.Context(), *input.ExpectedRevision, input.Enabled, input.DailyBudget, input.APIKey, input.RemoveKey, owner, input.Provider, input.BaseURL); err != nil {
			s.policyFailure(w, err)
			return
		}
		s.proxy.Invalidate()
		settings, err := s.store.ProxySettings(r.Context())
		if err != nil {
			s.unavailable(w)
			return
		}
		s.json(w, 200, settings)
	default:
		s.fail(w, 405, "method_not_allowed", "Use GET or PATCH.")
	}
}
func (s *Server) proxyAgent(w http.ResponseWriter, r *http.Request, a store.Agent) {
	if a.Method != "cli" {
		s.fail(w, 403, "forbidden", "CLI agent only.")
		return
	}
	if !s.method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		IP string `json:"ip"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	ip, err := netip.ParseAddr(input.IP)
	if err != nil || !proxycheck.Public(ip) {
		s.fail(w, 400, "validation_failed", "Use a public IP address.")
		return
	}
	s.json(w, 200, s.proxy.Lookup(r.Context(), ip))
}
