// Package testutil creates SYNTHETIC, NON-EXECUTABLE release files for isolated API tests.
package testutil

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"shortlink.local/artifacts"
)

func Build(root, version string) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	build := artifacts.Build{Version: version, Images: map[string]artifacts.Image{}}
	for _, arch := range []string{"amd64", "arm64"} {
		raw := make([]byte, 120)
		copy(raw, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
		binary.LittleEndian.PutUint16(raw[16:], 2)
		machine := uint16(62)
		if arch == "arm64" {
			machine = 183
		}
		binary.LittleEndian.PutUint16(raw[18:], machine)
		binary.LittleEndian.PutUint32(raw[20:], 1)
		binary.LittleEndian.PutUint64(raw[32:], 64)
		binary.LittleEndian.PutUint16(raw[52:], 64)
		binary.LittleEndian.PutUint16(raw[54:], 56)
		binary.LittleEndian.PutUint16(raw[56:], 1)
		binary.LittleEndian.PutUint32(raw[64:], 1)
		if err := os.WriteFile(filepath.Join(root, "agent-linux-"+arch), raw, 0600); err != nil {
			return err
		}
		config, _ := json.Marshal(map[string]any{"architecture": arch, "os": "linux", "config": map[string]any{"User": "10001:10001", "Entrypoint": []string{"/usr/local/bin/agent"}, "Cmd": []string{"serve", "--config", "/run/shortlink/agent.json"}}})
		hash := sha256.Sum256(config)
		digest := hex.EncodeToString(hash[:])
		build.Images[arch] = artifacts.Image{ID: "sha256:" + digest, ConfigDigest: "sha256:" + digest}
		name := digest + ".json"
		manifest, _ := json.Marshal([]map[string]any{{"Config": name, "Layers": []string{"layer.tar"}}})
		f, err := os.Create(filepath.Join(root, "image-linux-"+arch+".tar"))
		if err != nil {
			return err
		}
		writer := tar.NewWriter(f)
		for _, entry := range []struct {
			name string
			raw  []byte
		}{{name, config}, {"manifest.json", manifest}, {"layer.tar", []byte("SYNTHETIC TEST IMAGE, NOT EXECUTABLE")}} {
			if err = writer.WriteHeader(&tar.Header{Name: entry.name, Mode: 0600, Size: int64(len(entry.raw))}); err != nil {
				f.Close()
				return err
			}
			if _, err = writer.Write(entry.raw); err != nil {
				f.Close()
				return err
			}
		}
		if err = writer.Close(); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	for _, name := range []string{"compose.agent.yml", "INSTALL.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("SYNTHETIC TEST ONLY\n"), 0600); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(build)
	return os.WriteFile(filepath.Join(root, "build.json"), raw, 0600)
}
