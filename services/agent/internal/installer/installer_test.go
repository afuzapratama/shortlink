package installer

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"shortlink.local/artifacts"
	"shortlink.local/artifacts/testutil"
)

func fixture(t *testing.T) (*Installer, artifacts.Envelope, string) {
	t.Helper()
	input := filepath.Join(t.TempDir(), "input")
	if e := testutil.Build(input, "v0.10.0-test"); e != nil {
		t.Fatal(e)
	}
	buildRaw, _ := os.ReadFile(filepath.Join(input, "build.json"))
	var build artifacts.Build
	json.Unmarshal(buildRaw, &build)
	build.InstallerReady = true
	buildRaw, _ = json.Marshal(build)
	os.WriteFile(filepath.Join(input, "build.json"), buildRaw, 0600)
	seed := make([]byte, 32)
	seed[0] = 17
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	releaseRoot := filepath.Join(t.TempDir(), "published")
	envelope, e := artifacts.Publish(releaseRoot, input, seed, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(envelope)
	credential := randomToken()
	installationID := randomToken()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Error("unexpected query")
		}
		if r.URL.Path == "/v1/agents/install-context" {
			json.NewEncoder(w).Encode(map[string]any{"installationId": installationID, "bootstrap": map[string]string{"domainId": "domain-id", "hostname": "agent.example.test", "controlPlaneUrl": "https://center.example.test", "manifestVerificationKey": base64.RawURLEncoding.EncodeToString(pub), "artifactVersion": "v0.10.0-test", "method": "cli"}})
			return
		}
		if r.URL.Path == "/v1/agents/enroll" {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["token"] != "test-token" || body["installationProof"] == "" || body["installationId"] != installationID {
				w.WriteHeader(400)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"credential": credential, "bootstrap": map[string]string{"domainId": "domain-id", "hostname": "agent.example.test", "controlPlaneUrl": "https://center.example.test", "manifestVerificationKey": base64.RawURLEncoding.EncodeToString(pub), "artifactVersion": "v0.10.0-test", "method": "cli"}})
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+credential {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/v1/assets/agent/manifest" {
			w.Write(raw)
			return
		}
		for _, meta := range envelope.Artifacts {
			if r.URL.Path == meta.DownloadURL {
				http.ServeFile(w, r, filepath.Join(releaseRoot, "releases", envelope.Version, meta.Name))
				return
			}
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(server.Close)
	// Route the pinned test origin to the TLS test server; all application requests still use that origin.
	client := server.Client()
	transport := client.Transport.(*http.Transport)
	transport.TLSClientConfig.ServerName = "127.0.0.1"
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("redirect rejected") }
	root := t.TempDir()
	os.Chmod(root, 0700)
	i := &Installer{Root: root, Client: client, Timeout: 100 * time.Millisecond, State: State{Bootstrap: Bootstrap{DomainID: "domain-id", Hostname: "agent.example.test", ControlURL: "https://center.example.test", Key: base64.RawURLEncoding.EncodeToString(pub), Version: envelope.Version, Method: "cli"}, InstallationID: randomToken(), Proof: randomToken(), Credential: credential, Proxy: "external", Port: 8091, UID: os.Getuid()}}
	return i, envelope, releaseRoot
}
func TestEnrollmentRecoveryAndPinnedBootstrap(t *testing.T) {
	i, _, _ := fixture(t)
	i.State.Credential = ""
	if e := i.enroll(context.Background(), "test-token"); e != nil {
		t.Fatal(e)
	}
	raw, e := privateRead(filepath.Join(i.Root, "installation.json"), 32<<10)
	if e != nil {
		t.Fatal(e)
	}
	var saved State
	json.Unmarshal(raw, &saved)
	if saved.Credential == "" || saved.Proof != i.State.Proof || saved.InstallationID != i.State.InstallationID {
		t.Fatal("recovery identity/credential lost")
	}
	i.State.Bootstrap.Key = base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if i.enroll(context.Background(), "test-token") == nil {
		t.Fatal("accepted substituted bootstrap")
	}
}
func TestSignedDownloadAndTamperRejection(t *testing.T) {
	i, env, root := fixture(t)
	manifest, e := i.manifest(context.Background())
	if e != nil || manifest.Version != env.Version {
		t.Fatal(e)
	}
	meta := manifest.Artifacts[0]
	path, e := i.download(context.Background(), meta)
	if e != nil {
		t.Fatal(e)
	}
	if e = artifacts.CheckFile(path, meta); e != nil {
		t.Fatal(e)
	}
	os.Remove(path)
	os.WriteFile(filepath.Join(root, "releases", env.Version, meta.Name), []byte("tampered"), 0600)
	if _, e = i.download(context.Background(), meta); e == nil {
		t.Fatal("accepted corrupted download")
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("corrupt artifact installed")
	}
	i.State.Bootstrap.Key = base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if _, e = i.manifest(context.Background()); e == nil {
		t.Fatal("accepted wrong signing key")
	}
}
func TestUpdateFailureRestoresRuntimeAndPersistentData(t *testing.T) {
	i, env, _ := fixture(t)
	i.State.Image = "sha256:" + strings.Repeat("a", 64)
	i.State.Version = "v0.9.0-test"
	old := i.State
	os.Mkdir(filepath.Join(i.Root, "data"), 0700)
	marker := filepath.Join(i.Root, "data", "retained")
	os.WriteFile(marker, []byte("GeoLite and event state"), 0600)
	loads, ups := 0, 0
	i.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.HasPrefix(joined, "load ") {
			loads++
			return nil, nil
		}
		if strings.HasPrefix(joined, "image inspect ") {
			for _, a := range env.Artifacts {
				if a.Name == "image-linux-amd64.tar" {
					return []byte(*a.ImageID + " linux/amd64"), nil
				}
			}
		}
		if strings.Contains(joined, " up -d") {
			ups++
			if ups == 1 {
				return nil, errors.New("new runtime failed")
			}
			return nil, nil
		}
		return nil, nil
	}
	e := i.provision(context.Background(), true)
	if e == nil || !strings.Contains(e.Error(), "previous image restored") {
		t.Fatalf("unexpected result %v", e)
	}
	if i.State.Image != old.Image || i.State.Version != old.Version || loads != 1 || ups != 2 {
		t.Fatal("rollback did not restore old image")
	}
	raw, _ := os.ReadFile(marker)
	if string(raw) != "GeoLite and event state" {
		t.Fatal("persistent data changed")
	}
	raw, _ = os.ReadFile(filepath.Join(i.Root, "compose.yml"))
	if !strings.Contains(string(raw), old.Image) {
		t.Fatal("compose not restored")
	}
}
func TestPrivatePathsAndComposeIsolation(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0700)
	symlink := filepath.Join(root, "alias")
	os.Symlink(root, symlink)
	if secureDirectory(symlink) == nil {
		t.Fatal("symlink root accepted")
	}
	private := filepath.Join(root, "state.json")
	os.WriteFile(private, []byte("secret"), 0644)
	if _, e := privateRead(private, 100); e == nil {
		t.Fatal("public credential file accepted")
	}
	i, _, _ := fixture(t)
	i.Root = filepath.Join(root, "space and colon:path")
	i.State.Image = "sha256:" + strings.Repeat("a", 64)
	yaml := i.composeFile()
	if !strings.Contains(yaml, "network_mode: host") || !strings.Contains(yaml, "read_only: true") || strings.Contains(yaml, i.State.Credential) {
		t.Fatal("runtime security/secret isolation broken")
	}
}
func TestDockerOSBaseline(t *testing.T) {
	for _, v := range []string{"ID=ubuntu\nVERSION_CODENAME=noble", "ID=debian\nVERSION_CODENAME=trixie"} {
		if _, _, e := parseOS(v); e != nil {
			t.Fatal(e)
		}
	}
	if _, _, e := parseOS("ID=ubuntu\nVERSION_CODENAME=$(id)"); e == nil {
		t.Fatal("invalid distro accepted")
	}
}
func TestPreflightDoesNotReplaceExistingDaemon(t *testing.T) {
	i, _, _ := fixture(t)
	calls := 0
	i.Run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls++
		if strings.Join(args, " ") == "--version" {
			return []byte("Docker installed"), nil
		}
		return nil, errors.New("daemon stopped")
	}
	if e := i.installDocker(context.Background()); e == nil || calls != 1 {
		t.Fatal("existing engine was not protected")
	}
}

func TestInstallRerunAndCachedCLITamper(t *testing.T) {
	i, env, _ := fixture(t)
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer health.Close()
	_, port, _ := net.SplitHostPort(health.Listener.Addr().String())
	i.State.Port, _ = strconv.Atoi(port)
	loads := 0
	i.Run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.HasPrefix(joined, "load ") {
			loads++
			return nil, nil
		}
		if strings.HasPrefix(joined, "image inspect ") {
			for _, a := range env.Artifacts {
				if a.Name == "image-linux-amd64.tar" {
					return []byte(*a.ImageID + " linux/amd64"), nil
				}
			}
		}
		if strings.Contains(joined, "ps --quiet agent") {
			return []byte("test-container"), nil
		}
		if strings.HasPrefix(joined, "inspect test-container ") {
			return []byte("true " + i.State.Image), nil
		}
		return nil, nil
	}
	if e := i.provision(context.Background(), false); e != nil {
		t.Fatal(e)
	}
	image, credential := i.State.Image, i.State.Credential
	marker := filepath.Join(i.Root, "data", "geo-marker")
	os.WriteFile(marker, []byte("persist"), 0600)
	if e := i.provision(context.Background(), false); e != nil {
		t.Fatal(e)
	}
	if loads != 1 || i.State.Image != image || i.State.Credential != credential {
		t.Fatal("rerun replaced identity or reloaded image")
	}
	if raw, _ := os.ReadFile(marker); string(raw) != "persist" {
		t.Fatal("rerun removed persistent data")
	}
	previousInput := filepath.Join(t.TempDir(), "previous-input")
	if e := testutil.Build(previousInput, "v0.9.0-test"); e != nil {
		t.Fatal(e)
	}
	seed := make([]byte, 32)
	seed[0] = 17
	previousRoot := filepath.Join(t.TempDir(), "previous-release")
	previous, e := artifacts.Publish(previousRoot, previousInput, seed, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	previousCache := filepath.Join(i.Root, "releases", previous.Version)
	os.MkdirAll(previousCache, 0700)
	manifestRaw, _ := json.Marshal(previous)
	atomic(filepath.Join(previousCache, "manifest.json"), manifestRaw)
	nativeRaw, _ := os.ReadFile(filepath.Join(previousInput, "agent-linux-amd64"))
	atomic(filepath.Join(previousCache, "agent-linux-amd64"), nativeRaw)
	i.State.PreviousVersion = previous.Version
	i.State.PreviousImage = "sha256:" + strings.Repeat("c", 64)
	if e = i.rollback(context.Background()); e != nil {
		t.Fatal(e)
	}
	if i.State.Version != previous.Version || i.State.PreviousVersion != env.Version {
		t.Fatal("manual rollback did not swap versions")
	}
	if e = i.rollback(context.Background()); e != nil {
		t.Fatal(e)
	}
	if i.State.Version != env.Version {
		t.Fatal("return to original version failed")
	}
	native := filepath.Join(i.Root, "releases", i.State.Version, "agent-linux-amd64")
	os.WriteFile(native, []byte("corrupted"), 0600)
	if i.installBinary(native) == nil {
		t.Fatal("cached executable tamper accepted")
	}
}
func TestCaddyConfigurationWithRealImage(t *testing.T) {
	image := os.Getenv("AGENT_TEST_CADDY_IMAGE")
	if image == "" {
		t.Skip("optional real Caddy validation: set AGENT_TEST_CADDY_IMAGE")
	}
	i, _, _ := fixture(t)
	i.State.Proxy = "caddy"
	i.State.ProxyImage = "caddy@sha256:" + strings.Repeat("a", 64)
	i.State.Image = "sha256:" + strings.Repeat("b", 64)
	if e := i.writeRuntime(); e != nil {
		t.Fatal(e)
	}
	if output, e := exec.Command("docker", "run", "--rm", "--mount", "type=bind,source="+filepath.Join(i.Root, "Caddyfile")+",target=/etc/caddy/Caddyfile,readonly", image, "caddy", "validate", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile").CombinedOutput(); e != nil {
		t.Fatalf("Caddy configuration: %v %s", e, output)
	}
}
