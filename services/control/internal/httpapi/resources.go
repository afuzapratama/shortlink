package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"shortlink.local/policy"
)

var hostnamePattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
var slugPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

func normalizeHostname(value string) (string, bool) {
	host := strings.ToLower(strings.TrimSpace(value))
	if len(host) > 253 || !hostnamePattern.MatchString(host) || net.ParseIP(host) != nil {
		return host, false
	}
	for _, suffix := range []string{"localhost", "local", "internal", "home", "lan", "invalid", "test", "example", "onion"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return host, false
		}
	}
	return host, true
}
func destinationURL(value, host string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) > 4096 {
		return value, false
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Opaque != "" || strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), host) {
		return value, false
	}
	if u.Port() != "" {
		n, e := strconv.Atoi(u.Port())
		if e != nil || n < 1 || n > 65535 {
			return value, false
		}
	}
	if strings.ContainsAny(u.Host, " \\\t\r\n") {
		return value, false
	}
	return u.String(), true
}

type cursor struct {
	Kind   string `json:"kind"`
	After  string `json:"after"`
	Domain string `json:"domain"`
}

func encodeCursor(kind, after, domain string) string {
	b, _ := json.Marshal(cursor{kind, after, domain})
	return base64.RawURLEncoding.EncodeToString(b)
}
func (s *Server) pagination(w http.ResponseWriter, r *http.Request, kind, domain string) (string, int, bool) {
	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 1 || n > 100 {
			s.fail(w, 400, "validation_failed", "Limit must be 1–100.")
			return "", 0, false
		}
		limit = n
	}
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return "", limit, true
	}
	if len(raw) > 1024 {
		s.fail(w, 400, "validation_failed", "Invalid cursor.")
		return "", 0, false
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	var c cursor
	if err != nil || json.Unmarshal(b, &c) != nil || c.Kind != kind || c.Domain != domain || !security.ValidToken(c.After) {
		s.fail(w, 400, "validation_failed", "Cursor does not match this listing.")
		return "", 0, false
	}
	return c.After, limit, true
}
func (s *Server) resourceFailure(w http.ResponseWriter, err error, duplicate string) {
	switch {
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, store.ErrDomainMissing):
		s.fail(w, 404, "not_found", "Domain or shortlink not found.")
	case errors.Is(err, store.ErrDuplicate):
		s.fail(w, 409, duplicate, "Hostname or slug is already registered.")
	case errors.Is(err, store.ErrConflict):
		s.fail(w, 409, "revision_conflict", "The link changed. Reload the latest version before saving.")
	case errors.Is(err, store.ErrImmutable):
		s.fail(w, 422, "validation_failed", "A published shortlink keeps its domain and slug.")
	default:
		s.unavailable(w)
	}
}
func (s *Server) management(w http.ResponseWriter, r *http.Request, actor string) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/admin/"), "/")
	if len(parts) == 1 && parts[0] == "domains" {
		switch r.Method {
		case http.MethodGet:
			after, limit, ok := s.pagination(w, r, "domains", "")
			if !ok {
				return
			}
			items, err := s.store.Domains(r.Context(), after, limit+1)
			if err != nil {
				s.resourceFailure(w, err, "")
				return
			}
			var next *string
			if len(items) > limit {
				items = items[:limit]
				v := encodeCursor("domains", items[limit-1].ID, "")
				next = &v
			}
			s.json(w, 200, map[string]any{"items": items, "nextCursor": next})
		case http.MethodPost:
			var input struct {
				Hostname string `json:"hostname"`
				Mode     string `json:"mode"`
			}
			if !s.decode(w, r, &input) {
				return
			}
			host, valid := normalizeHostname(input.Hostname)
			if !valid || input.Mode != "local" && input.Mode != "api" {
				s.fail(w, 400, "validation_failed", "Provide a public hostname without URL/path/port and mode local or api.")
				return
			}
			d, err := s.store.CreateDomain(r.Context(), security.Token(), host, input.Mode, actor)
			if err != nil {
				s.resourceFailure(w, err, "domain_conflict")
				return
			}
			s.json(w, 201, d)
		default:
			w.Header().Set("Allow", "GET, POST")
			s.fail(w, 405, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	if len(parts) == 2 && parts[0] == "domains" {
		if !s.method(w, r, http.MethodGet) {
			return
		}
		d, err := s.store.Domain(r.Context(), parts[1])
		if err != nil {
			s.resourceFailure(w, err, "")
			return
		}
		s.json(w, 200, d)
		return
	}
	if len(parts) == 1 && parts[0] == "links" {
		switch r.Method {
		case http.MethodGet:
			domain := r.URL.Query().Get("domainId")
			after, limit, ok := s.pagination(w, r, "links", domain)
			if !ok {
				return
			}
			items, err := s.store.Links(r.Context(), after, domain, limit+1)
			if err != nil {
				s.resourceFailure(w, err, "")
				return
			}
			var next *string
			if len(items) > limit {
				items = items[:limit]
				v := encodeCursor("links", items[limit-1].ID, domain)
				next = &v
			}
			s.json(w, 200, map[string]any{"items": items, "nextCursor": next})
		case http.MethodPost:
			s.writeLink(w, r, "", actor)
		default:
			w.Header().Set("Allow", "GET, POST")
			s.fail(w, 405, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	if len(parts) == 2 && parts[0] == "links" {
		switch r.Method {
		case http.MethodGet:
			l, err := s.store.Link(r.Context(), parts[1])
			if err != nil {
				s.resourceFailure(w, err, "")
				return
			}
			s.json(w, 200, l)
		case http.MethodPatch:
			s.writeLink(w, r, parts[1], actor)
		default:
			w.Header().Set("Allow", "GET, PATCH")
			s.fail(w, 405, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	s.fail(w, 501, "not_implemented", "This management operation is not implemented yet.")
}
func (s *Server) writeLink(w http.ResponseWriter, r *http.Request, id, actor string) {
	var input struct {
		Routing          *policy.LinkRouting `json:"routing"`
		DomainID         string              `json:"domainId"`
		Slug             string              `json:"slug"`
		Destination      string              `json:"destination"`
		ExpectedRevision *int64              `json:"expectedRevision"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	if id != "" && (input.ExpectedRevision == nil || *input.ExpectedRevision < 0) || id == "" && input.ExpectedRevision != nil {
		s.fail(w, 400, "validation_failed", "A draft update requires expectedRevision; creation does not accept it.")
		return
	}
	domain, err := s.store.Domain(r.Context(), input.DomainID)
	if err != nil {
		s.resourceFailure(w, err, "")
		return
	}
	destination, valid := destinationURL(input.Destination, domain.Hostname)
	if input.Slug == "__agent" || !valid || policy.ValidateRouting(input.Routing, domain.Hostname) != nil || input.Slug != "" && !slugPattern.MatchString(input.Slug) {
		s.fail(w, 400, "validation_failed", "Use a valid HTTP/HTTPS destination without credentials or a loop, and a valid slug.")
		return
	}
	if id == "" {
		if input.Slug == "" {
			input.Slug = "link-" + security.Token()[:12]
		}
		l, err := s.store.CreateLinkRouting(r.Context(), security.Token(), input.DomainID, input.Slug, destination, input.Routing, actor)
		if err != nil {
			s.resourceFailure(w, err, "slug_conflict")
			return
		}
		s.json(w, 201, l)
	} else {
		l, err := s.store.UpdateLinkRouting(r.Context(), id, input.DomainID, input.Slug, destination, *input.ExpectedRevision, input.Routing, actor)
		if err != nil {
			s.resourceFailure(w, err, "slug_conflict")
			return
		}
		s.json(w, 200, l)
	}
}
