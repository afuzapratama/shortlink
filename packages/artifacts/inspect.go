package artifacts

import (
	"archive/tar"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"strings"
)

func InspectBinary(file, arch string) error {
	f, err := elf.Open(file)
	if err != nil {
		return errors.New("artifact is not ELF")
	}
	defer f.Close()
	machine := elf.EM_X86_64
	if arch == "arm64" {
		machine = elf.EM_AARCH64
	}
	if f.Class != elf.ELFCLASS64 || f.Data != elf.ELFDATA2LSB || f.Machine != machine || f.Type != elf.ET_EXEC || len(f.Progs) == 0 {
		return errors.New("binary platform/type mismatch")
	}
	return nil
}

// Inspect the archive without extracting. Bind the signed image ID to the actual config digest.
func InspectImage(file, arch, id, configDigest string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	reader := tar.NewReader(f)
	configs := map[string][]byte{}
	var manifest []struct {
		Config string   `json:"Config"`
		Layers []string `json:"Layers"`
	}
	seen := map[string]bool{}
	for {
		h, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name == "" || strings.HasPrefix(name, "/") || path.Clean(name) != name || strings.HasPrefix(name, "../") || strings.Contains(name, "\\") || seen[name] {
			return errors.New("unsafe or duplicate image archive path")
		}
		seen[name] = true
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			return errors.New("image archive contains unsupported entry")
		}
		if h.Name == "manifest.json" {
			raw, err := io.ReadAll(io.LimitReader(reader, 1<<20))
			if err != nil || h.Size > 1<<20 || json.Unmarshal(raw, &manifest) != nil {
				return errors.New("invalid docker archive manifest")
			}
		} else if h.Size <= 1<<20 {
			raw, err := io.ReadAll(reader)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(raw)
			if "sha256:"+hex.EncodeToString(sum[:]) == id || "sha256:"+hex.EncodeToString(sum[:]) == configDigest {
				configs[h.Name] = raw
			}
		}
	}
	if len(manifest) != 1 || len(manifest[0].Layers) == 0 {
		return errors.New("archive must contain one image")
	}
	raw, ok := configs[manifest[0].Config]
	if !ok {
		return errors.New("archive config digest differs from build metadata")
	}
	if id != configDigest {
		matched := false
		for _, entry := range configs {
			sum := sha256.Sum256(entry)
			if "sha256:"+hex.EncodeToString(sum[:]) != id {
				continue
			}
			var descriptor struct {
				Config struct {
					Digest string `json:"digest"`
				} `json:"config"`
			}
			if json.Unmarshal(entry, &descriptor) == nil && descriptor.Config.Digest == configDigest {
				matched = true
			}
		}
		if !matched {
			return errors.New("archive OCI image ID does not bind its config")
		}
	}
	for _, layer := range manifest[0].Layers {
		if !seen[layer] {
			return errors.New("archive layer missing")
		}
	}
	var config struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
		Config       struct {
			User       string   `json:"User"`
			Entrypoint []string `json:"Entrypoint"`
			Cmd        []string `json:"Cmd"`
			Env        []string `json:"Env"`
		} `json:"config"`
	}
	if json.Unmarshal(raw, &config) != nil || config.Architecture != arch || config.OS != "linux" || config.Config.User != "10001:10001" || strings.Join(config.Config.Entrypoint, " ") != "/usr/local/bin/agent" || strings.Join(config.Config.Cmd, " ") != "serve --config /run/shortlink/agent.json" {
		return errors.New("image platform/runtime config mismatch")
	}
	for _, env := range config.Config.Env {
		if strings.Contains(strings.ToLower(env), "token") || strings.Contains(strings.ToLower(env), "credential") || strings.Contains(strings.ToLower(env), "maxmind") {
			return errors.New("unexpected secret environment in image")
		}
	}
	return nil
}
func readBound(file string, limit int64) ([]byte, error) {
	f, e := os.Open(file)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e == nil && int64(len(raw)) > limit {
		return nil, errors.New("metadata exceeds limit")
	}
	return raw, e
}
