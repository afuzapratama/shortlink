package httpapi

import (
	"crypto/ed25519"
	"net/http"
	"os"
	"path/filepath"
	"shortlink.local/artifacts"
	"shortlink.local/control/internal/security"
	"strings"
	"time"
)

// PHP packages are operator downloads. Install tokens and agent bearers grant no access.
func (s *Server) phpAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		s.fail(w, 405, "method_not_allowed", "Use GET or HEAD.")
		return
	}
	if r.Header.Get("Authorization") != "" {
		s.fail(w, 401, "unauthorized", "Owner session required.")
		return
	}
	owner, _, ok := s.authorize(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if s.cfg.PHPAssetRoot == "" {
		s.fail(w, 503, "artifact_unavailable", "PHP release is not configured.")
		return
	}
	seed, err := s.store.Seed(r.Context())
	if err != nil {
		s.unavailable(w)
		return
	}
	key := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	version, name := "", ""
	if r.URL.Path != "/v1/assets/php/manifest" {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/assets/php/releases/"), "/")
		if len(parts) != 2 || r.URL.RawPath != "" || !artifacts.ValidVersion(parts[0]) || !artifacts.ValidPHPName(parts[1]) {
			s.fail(w, 404, "not_found", "PHP artifact not found.")
			return
		}
		version, name = parts[0], parts[1]
	}
	e, err := artifacts.ReadPHP(s.cfg.PHPAssetRoot, key, version)
	if os.IsNotExist(err) && version != "" {
		s.fail(w, 404, "not_found", "PHP release not found.")
		return
	}
	if err != nil {
		s.fail(w, 503, "artifact_unavailable", "No valid PHP release is available.")
		return
	}
	if name == "" {
		w.Header().Set("ETag", `"`+security.Digest(e.SignedPayload)+`"`)
		if r.Method == http.MethodHead {
			w.WriteHeader(200)
			return
		}
		s.json(w, 200, e)
		return
	}
	allowed, err := s.store.AllowLogin(r.Context(), security.Digest("php-download:"+owner.ID), 30)
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
		s.fail(w, 503, "control_unavailable", "Downloads busy; retry later.")
		return
	}
	var meta artifacts.File
	for _, f := range e.Artifacts {
		if f.Name == name {
			meta = f
		}
	}
	file, err := os.Open(filepath.Join(s.cfg.PHPAssetRoot, "releases", e.Version, name))
	if err != nil {
		s.fail(w, 503, "artifact_unavailable", "PHP artifact unavailable.")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != meta.Size {
		s.fail(w, 503, "artifact_unavailable", "PHP artifact invalid.")
		return
	}
	sum, n, err := artifacts.Hash(file)
	if err != nil || sum != meta.SHA256 || n != meta.Size {
		s.fail(w, 503, "artifact_unavailable", "PHP checksum invalid.")
		return
	}
	if _, err = file.Seek(0, 0); err != nil {
		s.unavailable(w)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("X-Artifact-SHA256", meta.SHA256)
	w.Header().Set("ETag", `"`+meta.SHA256+`"`)
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(30 * time.Second))
	http.ServeContent(w, r, name, time.Time{}, file)
}
