package httpapi

import (
	"errors"
	"net/http"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"strings"
)

func (s *Server) domainManageAdmin(w http.ResponseWriter, r *http.Request, actor string) bool {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/admin/domains/"), "/")
	if !strings.HasPrefix(r.URL.Path, "/v1/admin/domains/") || len(parts) != 2 {
		return false
	}
	switch parts[1] {
	case "opening":
		if !s.method(w, r, http.MethodPatch) {
			return true
		}
		var input struct {
			URL              string `json:"openingUrl"`
			ExpectedRevision *int64 `json:"expectedRevision"`
		}
		if !s.decode(w, r, &input) {
			return true
		}
		if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 {
			s.fail(w, 400, "validation_failed", "expectedRevision required.")
			return true
		}
		d, err := s.store.UpdateOpening(r.Context(), parts[0], strings.TrimSpace(input.URL), *input.ExpectedRevision, actor)
		if err != nil {
			s.policyFailure(w, err)
		} else {
			s.json(w, 200, d)
		}
	case "manage-key":
		if !s.method(w, r, http.MethodPost) {
			return true
		}
		key, err := s.store.IssueManageKey(r.Context(), parts[0], actor)
		if err != nil {
			s.resourceFailure(w, err, "")
		} else {
			s.json(w, 200, key)
		}
	default:
		return false
	}
	return true
}
func (s *Server) manageFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrManageAccess) {
		s.fail(w, 401, "unauthorized", "Manage access invalid or expired.")
	} else {
		s.policyFailure(w, err)
	}
}
func (s *Server) agentOpening(w http.ResponseWriter, r *http.Request, a store.Agent) {
	if !s.method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		Slug string `json:"slug"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	if input.Slug != "" && !slugPattern.MatchString(input.Slug) {
		s.fail(w, 400, "validation_failed", "Invalid slug.")
		return
	}
	target, published, err := s.store.Opening(r.Context(), a, input.Slug)
	if err != nil {
		s.unavailable(w)
		return
	}
	s.json(w, 200, map[string]any{"openingUrl": target, "published": published, "domainId": a.DomainID})
}
func (s *Server) agentManage(w http.ResponseWriter, r *http.Request, a store.Agent) {
	if !s.method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		Key              string `json:"key"`
		Session          string `json:"session"`
		CSRF             string `json:"csrf"`
		Action           string `json:"action"`
		OpeningURL       string `json:"openingUrl"`
		ExpectedRevision *int64 `json:"expectedRevision"`
		After            string `json:"after"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	if r.URL.Path == "/v1/agents/manage/exchange" {
		allowed, err := s.store.AllowLogin(r.Context(), security.Digest("agent-manage:"+a.ID), 10)
		if err != nil {
			s.unavailable(w)
			return
		}
		if !allowed {
			s.limited(w)
			return
		}
		session, err := s.store.ExchangeManageKey(r.Context(), a, input.Key)
		if err != nil {
			s.manageFailure(w, err)
		} else {
			s.json(w, 200, session)
		}
		return
	}
	if err := s.store.AuthorizeManage(r.Context(), a, input.Session); err != nil {
		s.manageFailure(w, err)
		return
	}
	switch input.Action {
	case "logout", "update":
		if !security.Equal(input.CSRF, security.CSRF(input.Session)) {
			s.fail(w, 403, "forbidden", "Manage CSRF required.")
			return
		}
		if input.Action == "logout" {
			if err := s.store.LogoutManage(r.Context(), a, input.Session); err != nil {
				s.unavailable(w)
			} else {
				s.json(w, 200, map[string]bool{"ok": true})
			}
			return
		}
		if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 {
			s.fail(w, 400, "validation_failed", "expectedRevision required.")
			return
		}
		if _, err := s.store.UpdateOpening(r.Context(), a.DomainID, strings.TrimSpace(input.OpeningURL), *input.ExpectedRevision, a.ID, input.Session); err != nil {
			s.manageFailure(w, err)
			return
		}
	case "view":
	default:
		s.fail(w, 400, "validation_failed", "Invalid manage action.")
		return
	}
	if len(input.After) > 128 {
		s.fail(w, 400, "validation_failed", "Invalid cursor.")
		return
	}
	d, err := s.store.Domain(r.Context(), a.DomainID)
	if err != nil {
		s.unavailable(w)
		return
	}
	links, err := s.store.Links(r.Context(), input.After, a.DomainID, 6)
	if err != nil {
		s.unavailable(w)
		return
	}
	next := ""
	if len(links) > 5 {
		links = links[:5]
		next = links[4].ID
	}
	s.json(w, 200, map[string]any{"domain": d, "links": links, "nextCursor": next, "csrf": security.CSRF(input.Session)})
}
