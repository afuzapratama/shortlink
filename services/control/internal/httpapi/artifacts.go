package httpapi

import (
	"crypto/ed25519"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"shortlink.local/artifacts"
	"shortlink.local/control/internal/security"
	"strings"
	"time"
)

func (s *Server) publicURL() string {
	if s.cfg.PublicURL != "" {
		return s.cfg.PublicURL
	}
	return s.cfg.Origin
}
func (s *Server) artifactAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
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
		a, e := s.store.AuthenticateAgent(r.Context(), raw)
		if e != nil || a.Method != "cli" {
			s.fail(w, 401, "unauthorized", "Active CLI agent required.")
			return
		}
		actor = a.ID
	} else {
		o, _, ok := s.authorize(w, r)
		if !ok {
			return
		}
		actor = o.ID
	}
	if s.cfg.AgentAssetRoot == "" {
		s.fail(w, 503, "artifact_unavailable", "Agent release has not been configured.")
		return
	}
	seed, err := s.store.Seed(r.Context())
	if err != nil {
		s.unavailable(w)
		return
	}
	key := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if r.URL.Path == "/v1/assets/agent/manifest" {
		e, err := artifacts.Read(s.cfg.AgentAssetRoot, key, "")
		if err != nil {
			s.fail(w, 503, "artifact_unavailable", "No valid agent release is available.")
			return
		}
		etag := `"` + security.Digest(e.SignedPayload) + `"`
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
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/assets/agent/releases/"), "/")
	if len(parts) != 2 || r.URL.RawPath != "" || !artifacts.ValidVersion(parts[0]) || !artifacts.ValidName(parts[1]) {
		s.fail(w, 404, "not_found", "Artifact not found.")
		return
	}
	allowed, err := s.store.AllowLogin(r.Context(), security.Digest("artifact-download:"+actor), 30)
	if err != nil {
		s.unavailable(w)
		return
	}
	if !allowed {
		s.limited(w)
		return
	}
	select {
	case s.artifactSlots <- struct{}{}:
		defer func() { <-s.artifactSlots }()
	default:
		s.fail(w, 503, "control_unavailable", "Artifact downloads are busy; retry later.")
		return
	}
	e, err := artifacts.Read(s.cfg.AgentAssetRoot, key, parts[0])
	if errors.Is(err, os.ErrNotExist) {
		s.fail(w, 404, "not_found", "Release not found.")
		return
	}
	if err != nil {
		s.fail(w, 503, "artifact_unavailable", "Release signature is invalid.")
		return
	}
	var meta artifacts.File
	for _, f := range e.Artifacts {
		if f.Name == parts[1] {
			meta = f
		}
	}
	path := filepath.Join(s.cfg.AgentAssetRoot, "releases", e.Version, meta.Name)
	file, err := os.Open(path)
	if err != nil {
		s.fail(w, 503, "artifact_unavailable", "Artifact unavailable.")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != meta.Size {
		s.fail(w, 503, "artifact_unavailable", "Artifact size/type invalid.")
		return
	}
	sum, n, err := artifacts.Hash(file)
	if err != nil || sum != meta.SHA256 || n != meta.Size {
		s.fail(w, 503, "artifact_unavailable", "Artifact checksum invalid.")
		return
	}
	if _, err = file.Seek(0, 0); err != nil {
		s.unavailable(w)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+meta.Name+`"`)
	w.Header().Set("ETag", `"`+meta.SHA256+`"`)
	w.Header().Set("X-Artifact-SHA256", meta.SHA256)
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Minute))
	http.ServeContent(w, r, meta.Name, time.Time{}, file)
}
