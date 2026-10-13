package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateComposeKeepsOperatorConfiguration(t *testing.T) {
	original := []byte("# operator settings\nservices:\n  agent:\n    image: old-image\n    environment:\n      CUSTOM: keep-me\n  proxy:\n    image: custom-caddy\n    volumes:\n      - /private/custom:/etc/caddy/Caddyfile\n  monitor:\n    image: monitor-image\n")
	got, err := updateComposeImage(original, "sha256:new-image", "resolved-caddy")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"operator settings", "CUSTOM: keep-me", "custom-caddy", "/private/custom:/etc/caddy/Caddyfile", "monitor-image", "sha256:new-image"} {
		if !strings.Contains(string(got), want) {
			t.Fatal("lost operator setting", want)
		}
	}
	if _, err := updateComposeImage([]byte("services: {}"), "new", "resolved-caddy"); err == nil {
		t.Fatal("missing managed image accepted")
	}
}
func TestWriteRuntimePreservesCustomProxyAndCompose(t *testing.T) {
	root := t.TempDir()
	i := &Installer{Root: root, State: State{UID: os.Getuid(), Port: 8091, Image: "sha256:new", Bootstrap: Bootstrap{Hostname: "agent.example.test"}}}
	caddy := []byte("agent.example.test { reverse_proxy 127.0.0.1:8091 }\nother.example.test { reverse_proxy 127.0.0.1:9000 }\n")
	if err := os.WriteFile(filepath.Join(root, "Caddyfile"), caddy, 0600); err != nil {
		t.Fatal(err)
	}
	compose := []byte("services:\n  agent:\n    image: old\n  proxy:\n    image: custom-caddy\n")
	if err := os.WriteFile(filepath.Join(root, "compose.yml"), compose, 0600); err != nil {
		t.Fatal(err)
	}
	if err := i.writeRuntime(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "Caddyfile"))
	if err != nil || string(got) != string(caddy) {
		t.Fatal("operator proxy overwritten", err)
	}
	got, err = os.ReadFile(filepath.Join(root, "compose.yml"))
	if err != nil || !strings.Contains(string(got), "custom-caddy") || !strings.Contains(string(got), "sha256:new") {
		t.Fatal("operator Compose overwritten", err)
	}
}

func TestComposeFirstInstallResolvesEmptyProxyImage(t *testing.T) {
	raw := []byte("services:\n  agent:\n    image: \"\"\n  proxy:\n    image: \"\"\n")
	result, err := updateComposeImage(raw, "sha256:agent", "caddy@sha256:pinned")
	if err != nil || !strings.Contains(string(result), "caddy@sha256:pinned") {
		t.Fatal("unresolved proxy image", err)
	}
}
