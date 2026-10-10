package httpapi

import (
	"crypto/ed25519"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shortlink.local/control/internal/security"
	"shortlink.local/geoip"
)

func (s *Server) geoAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		s.fail(w, 405, "method_not_allowed", "Use GET or HEAD.")
		return
	}
	actor := ""
	if header := r.Header.Get("Authorization"); header != "" {
		raw := strings.TrimPrefix(header, "Bearer ")
		if raw == header || !security.ValidToken(raw) {
			s.fail(w, 401, "unauthorized", "Agent credential required.")
			return
		}
		agent, err := s.store.AuthenticateAgent(r.Context(), raw)
		if err != nil {
			s.fail(w, 401, "unauthorized", "Agent credential revoked or invalid.")
			return
		}
		actor = agent.ID
	} else {
		owner, _, ok := s.authorize(w, r)
		if !ok {
			return
		}
		actor = owner.ID
	}
	if s.cfg.GeoIPRoot == "" {
		s.fail(w, 503, "geo_unavailable", "GeoLite publisher has not been configured.")
		return
	}
	seed, err := s.store.Seed(r.Context())
	if err != nil {
		s.unavailable(w)
		return
	}
	key := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if r.URL.Path == "/v1/assets/geoip/manifest" {
		e, _, err := geoip.ReadManifest(s.cfg.GeoIPRoot, key, time.Now(), true)
		if err != nil {
			s.fail(w, 503, "geo_unavailable", "No valid GeoLite release is available.")
			return
		}
		etag := `"` + e.ReleaseID + `"`
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(304)
			return
		}
		if r.Method == "HEAD" {
			w.WriteHeader(200)
			return
		}
		s.json(w, 200, e)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/assets/geoip/releases/"), "/")
	if len(parts) != 2 || r.URL.RawPath != "" || !geoip.ValidRelease(parts[0]) || !geoip.ValidEdition(parts[1]) {
		s.fail(w, 404, "not_found", "GeoLite file not found.")
		return
	}
	allowed, err := s.store.AllowLogin(r.Context(), security.Digest("geo-download:"+actor), 30)
	if err != nil {
		s.unavailable(w)
		return
	}
	if !allowed {
		s.limited(w)
		return
	}
	select {
	case s.geoSlots <- struct{}{}:
		defer func() { <-s.geoSlots }()
	default:
		s.fail(w, 503, "control_unavailable", "GeoLite downloads are busy; retry later.")
		return
	}
	releasePath := filepath.Join(s.cfg.GeoIPRoot, "releases", parts[0])
	raw, err := geoip.ReadSignedFile(filepath.Join(releasePath, "manifest.json"))
	if err != nil {
		s.fail(w, 404, "not_found", "GeoLite release not found.")
		return
	}
	e, err := geoip.Verify(raw, key, time.Now(), true)
	if err != nil || e.ReleaseID != parts[0] {
		s.fail(w, 503, "geo_unavailable", "GeoLite release is invalid or expired.")
		return
	}
	var metadata geoip.File
	for _, f := range e.Files {
		if f.Edition == parts[1] {
			metadata = f
			break
		}
	}
	file, err := os.Open(filepath.Join(releasePath, metadata.Edition+".mmdb"))
	if err != nil {
		s.fail(w, 404, "not_found", "GeoLite file not found.")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != metadata.Size {
		s.fail(w, 503, "geo_unavailable", "GeoLite file is unavailable.")
		return
	}
	// Large files have a bounded streaming deadline rather than the normal JSON deadline.
	if err = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Minute)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.unavailable(w)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("ETag", `"`+metadata.SHA256+`"`)
	http.ServeContent(w, r, metadata.Edition+".mmdb", e.PublishedAt, file)
}

func (s *Server) geoStatus(w http.ResponseWriter, r *http.Request) {
	if !s.method(w, r, http.MethodGet) {
		return
	}
	status := "not_configured"
	var manifest *geoip.Envelope
	if s.cfg.GeoIPRoot != "" {
		seed, err := s.store.Seed(r.Context())
		if err != nil {
			s.unavailable(w)
			return
		}
		e, _, err := geoip.ReadManifest(s.cfg.GeoIPRoot, ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey), time.Now(), false)
		switch {
		case os.IsNotExist(err):
			status = "awaiting_database"
		case err != nil:
			status = "invalid"
		case !time.Now().Before(e.ExpiresAt):
			status = "expired"
			manifest = &e
		default:
			status = "ready"
			manifest = &e
		}
	}
	rows, err := s.store.DB.Query(r.Context(), "SELECT d.id,d.hostname,a.geo_release,d.last_seen_at FROM agents a JOIN domains d ON d.id=a.domain_id WHERE a.revoked_at IS NULL AND a.method='cli' ORDER BY d.hostname")
	if err != nil {
		s.unavailable(w)
		return
	}
	defer rows.Close()
	agents := []map[string]any{}
	for rows.Next() {
		var id, host string
		var release *string
		var last *time.Time
		if err = rows.Scan(&id, &host, &release, &last); err != nil {
			s.unavailable(w)
			return
		}
		agents = append(agents, map[string]any{"domainId": id, "hostname": host, "geoRelease": release, "lastSeenAt": last})
	}
	if rows.Err() != nil {
		s.unavailable(w)
		return
	}
	s.json(w, 200, map[string]any{"status": status, "manifest": manifest, "agents": agents})
}
