package testutil

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

func PHPBuild(root, version string) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	name := "agent-php-" + version + ".zip"
	file, err := os.Create(filepath.Join(root, name))
	if err != nil {
		return err
	}
	z := zip.NewWriter(file)
	files := map[string]string{"public/index.php": "<?php // synthetic test package", "public/setup.css": "body{}", "public/.htaccess": "RewriteEngine On", "private/src/Adapter.php": "<?php const VERSION = '" + version + "';", "private/src/Setup.php": "<?php // setup fixture", "private/bootstrap.example.json": `{"hostname":"example.test","publicUrl":"https://example.test","controlPlaneUrl":"https://control.example.test","trustedProxies":[],"developmentLoopback":false}`, "INSTALL.md": "Synthetic PHP release tutorial"}
	for name, raw := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0600)
		w, err := z.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err = w.Write([]byte(raw)); err != nil {
			return err
		}
	}
	if err = z.Close(); err != nil {
		return err
	}
	file.Close()
	raw, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	m := map[string]any{"version": version, "method": "php_zip", "minimumPhp": "8.2", "filename": name, "size": len(raw), "sha256": hex.EncodeToString(sum[:])}
	payload, _ := json.Marshal(m)
	return os.WriteFile(filepath.Join(root, "manifest.json"), payload, 0600)
}
