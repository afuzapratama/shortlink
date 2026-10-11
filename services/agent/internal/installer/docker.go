package installer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
)

func parseOS(raw string) (string, string, error) {
	values := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			values[k] = strings.Trim(v, "\"'")
		}
	}
	id, code := values["ID"], values["VERSION_CODENAME"]
	supported := map[string]bool{"ubuntu:jammy": true, "ubuntu:noble": true, "ubuntu:resolute": true, "debian:bookworm": true, "debian:trixie": true}
	if !supported[id+":"+code] {
		return "", "", errors.New("automatic Docker installation supports Ubuntu 22.04/24.04/26.04 and Debian 12/13 only; install Docker manually on other distributions")
	}
	return id, code, nil
}
func (i *Installer) installDocker(ctx context.Context) error {
	// Never replace an existing engine, conflicting packages or a stopped daemon.
	if _, e := i.Run(ctx, "docker", "--version"); e == nil {
		return errors.New("Docker exists but daemon/access is unavailable; start or repair it manually")
	}
	raw, e := os.ReadFile("/etc/os-release")
	if e != nil {
		return e
	}
	id, code, e := parseOS(string(raw))
	if e != nil {
		return e
	}
	for _, name := range []string{"docker.io", "docker-compose", "docker-compose-v2", "docker-doc", "docker-buildx", "podman-docker", "containerd", "runc"} {
		out, err := i.Run(ctx, "dpkg-query", "-W", "-f=${Status}", name)
		if err == nil && strings.Contains(string(out), "install ok installed") {
			return fmt.Errorf("package %s conflicts with official Docker; resolve manually before rerun", name)
		}
	}
	if _, e = i.Run(ctx, "apt-get", "update"); e != nil {
		return e
	}
	if _, e = i.Run(ctx, "apt-get", "install", "-y", "ca-certificates"); e != nil {
		return e
	}
	if e = os.MkdirAll("/etc/apt/keyrings", 0755); e != nil {
		return e
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("Docker key redirect rejected") }}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://download.docker.com/linux/"+id+"/gpg", nil)
	res, e := client.Do(req)
	if e != nil {
		return errors.New("Docker repository key download failed")
	}
	defer res.Body.Close()
	key, e := io.ReadAll(io.LimitReader(res.Body, 32<<10))
	if e != nil || res.StatusCode != 200 || !strings.Contains(string(key), "BEGIN PGP PUBLIC KEY BLOCK") {
		return errors.New("invalid Docker repository key")
	}
	keyPath := "/etc/apt/keyrings/shortlink-docker.asc"
	if e = repositoryFile(keyPath, key); e != nil {
		return e
	}
	arch, e := i.Run(ctx, "dpkg", "--print-architecture")
	if e != nil {
		return e
	}
	architecture := strings.TrimSpace(string(arch))
	if !regexp.MustCompile(`^(amd64|arm64)$`).MatchString(architecture) {
		return errors.New("unsupported Docker package architecture")
	}
	source := fmt.Sprintf("Types: deb\nURIs: https://download.docker.com/linux/%s\nSuites: %s\nComponents: stable\nArchitectures: %s\nSigned-By: %s\n", id, code, architecture, keyPath)
	sourcePath := "/etc/apt/sources.list.d/shortlink-docker.sources"
	if e = repositoryFile(sourcePath, []byte(source)); e != nil {
		return e
	}
	if _, e = i.Run(ctx, "apt-get", "update"); e != nil {
		return e
	}
	if _, e = i.Run(ctx, "apt-get", "install", "-y", "docker-ce", "docker-ce-cli", "containerd.io", "docker-buildx-plugin", "docker-compose-plugin"); e != nil {
		return e
	}
	_, e = i.Run(ctx, "systemctl", "enable", "--now", "docker")
	return e
}
func repositoryFile(path string, raw []byte) error {
	info, e := os.Lstat(path)
	if e == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() != int64(len(raw)) {
			return errors.New("installer repository file differs; inspect it before rerun")
		}
		existing, err := os.ReadFile(path)
		if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || err != nil || !bytes.Equal(raw, existing) {
			return errors.New("installer repository file differs; inspect it before rerun")
		}
		return nil
	}
	if !os.IsNotExist(e) {
		return e
	}
	file, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	_, e = file.Write(raw)
	closeErr := file.Close()
	if e != nil {
		return e
	}
	return closeErr
}
