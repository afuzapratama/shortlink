// Package installer provisions a dedicated Linux host installation without putting secrets in argv.
package installer

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
	"shortlink.local/agent/internal/runtime"
	"shortlink.local/artifacts"
)

type Bootstrap struct {
	DomainID   string `json:"domainId"`
	Hostname   string `json:"hostname"`
	ControlURL string `json:"controlPlaneUrl"`
	Key        string `json:"manifestVerificationKey"`
	Version    string `json:"artifactVersion"`
	Method     string `json:"method"`
}
type State struct {
	Bootstrap       Bootstrap `json:"bootstrap"`
	InstallationID  string    `json:"installationId"`
	Proof           string    `json:"installationProof"`
	Credential      string    `json:"credential,omitempty"`
	Image           string    `json:"image,omitempty"`
	PreviousImage   string    `json:"previousImage,omitempty"`
	Version         string    `json:"version,omitempty"`
	PreviousVersion string    `json:"previousVersion,omitempty"`
	Proxy           string    `json:"proxy"`
	ProxyImage      string    `json:"proxyImage,omitempty"`
	Port            int       `json:"port"`
	UID             int       `json:"uid"`
	Development     bool      `json:"development"`
}
type Installer struct {
	Root    string
	State   State
	Client  *http.Client
	Run     func(context.Context, string, ...string) ([]byte, error)
	Timeout time.Duration
}

var hostPattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
var proxyPattern = regexp.MustCompile(`^(?:docker\.io/library/)?caddy@sha256:[a-f0-9]{64}$`)

func Run(args []string) error {
	if goruntime.GOOS != "linux" || (goruntime.GOARCH != "amd64" && goruntime.GOARCH != "arm64") {
		return errors.New("installer requires Linux amd64/arm64")
	}
	if len(args) == 0 {
		return errors.New("usage: agent install|status|update|rollback --root /opt/shortlink-agent [--bootstrap bootstrap.json]")
	}
	command := args[0]
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	root := f.String("root", "/opt/shortlink-agent", "dedicated private installation directory")
	bootstrap := f.String("bootstrap", "", "trusted domain bootstrap JSON (first installation)")
	proxy := f.String("proxy", "caddy", "caddy automatic HTTPS or external reverse proxy")
	proxyImage := f.String("proxy-image", "", "optional pinned official caddy@sha256 image; otherwise resolve official caddy:2 once")
	port := f.Int("port", 8091, "loopback runtime port")
	dockerInstall := f.Bool("install-docker", false, "install official Docker packages on clean Ubuntu/Debian")
	development := f.Bool("development", false, "local tests only: center HTTP loopback, external proxy")
	timeout := f.Duration("timeout", 5*time.Minute, "runtime and HTTPS readiness timeout")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected argument; token is entered through hidden prompt")
	}
	if command != "install" && command != "status" && command != "update" && command != "rollback" {
		return errors.New("unknown installer command")
	}
	if !filepath.IsAbs(*root) || strings.ContainsAny(*root, "\n\r\x00") || filepath.Clean(*root) == "/" || *port < 1024 || *port > 65535 || *timeout <= 0 || *timeout > 30*time.Minute {
		return errors.New("invalid root, port or timeout")
	}
	if *proxy != "caddy" && *proxy != "external" {
		return errors.New("proxy must be caddy or external")
	}
	if *proxyImage != "" && !proxyPattern.MatchString(*proxyImage) {
		return errors.New("proxy image must be pinned official caddy@sha256 digest")
	}
	rootPath := filepath.Clean(*root)
	if err := secureDirectory(rootPath); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(rootPath, "operation.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("another installer operation is running")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	i := &Installer{Root: rootPath, Client: &http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect rejected") }}, Run: run, Timeout: *timeout}
	statePath := filepath.Join(rootPath, "installation.json")
	if raw, e := privateRead(statePath, 32<<10); e == nil {
		if json.Unmarshal(raw, &i.State) != nil {
			return errors.New("invalid private installation state")
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if command != "install" && i.State.Credential == "" {
		return errors.New("install and enroll first")
	}
	if i.State.InstallationID == "" {
		entries, readErr := os.ReadDir(rootPath)
		if readErr != nil {
			return readErr
		}
		for _, entry := range entries {
			if entry.Name() != "operation.lock" {
				return errors.New("first installation requires an empty dedicated root; existing files were retained")
			}
		}
		if command != "install" || *bootstrap == "" {
			return errors.New("first install requires trusted --bootstrap file")
		}
		if os.Geteuid() != 0 {
			return errors.New("initial provisioning requires root; use sudo after verifying this binary")
		}
		raw, e := os.ReadFile(*bootstrap)
		if e != nil {
			return e
		}
		if len(raw) > 16<<10 || json.Unmarshal(raw, &i.State.Bootstrap) != nil {
			return errors.New("invalid bootstrap")
		}
		i.State.InstallationID = randomToken()
		i.State.Proof = randomToken()
		i.State.Proxy = *proxy
		i.State.ProxyImage = *proxyImage
		i.State.Port = *port
		i.State.UID = 10001
		i.State.Development = *development
		if err = i.validate(); err != nil {
			return err
		}
		preflightCtx, stopPreflight := context.WithTimeout(context.Background(), 30*time.Minute)
		err = i.preflight(preflightCtx, *dockerInstall)
		stopPreflight()
		if err != nil {
			return err
		}
		if err = i.save(); err != nil {
			return err
		}
	} else {
		if *bootstrap != "" {
			raw, e := os.ReadFile(*bootstrap)
			if e != nil {
				return e
			}
			var b Bootstrap
			if len(raw) > 16<<10 || json.Unmarshal(raw, &b) != nil || b.DomainID != i.State.Bootstrap.DomainID || b.Hostname != i.State.Bootstrap.Hostname || b.Key != i.State.Bootstrap.Key || b.ControlURL != i.State.Bootstrap.ControlURL || b.Method != "cli" || !artifacts.ValidVersion(b.Version) {
				return errors.New("bootstrap does not match existing installation; URL migration belongs to B18")
			}
			if i.State.Image == "" {
				i.State.Bootstrap.Version = b.Version
				if err = i.save(); err != nil {
					return err
				}
			}
		}
		if err = i.validate(); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if host := os.Getenv("DOCKER_HOST"); host != "" && !strings.HasPrefix(host, "unix://") {
		return errors.New("installer requires a local Docker engine")
	}
	endpoint, e := i.Run(ctx, "docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
	if e != nil || !strings.HasPrefix(strings.TrimSpace(string(endpoint)), "unix://") {
		return errors.New("installer requires a local Unix-socket Docker context")
	}
	if _, err = i.Run(ctx, "docker", "info", "--format", "{{.OSType}}/{{.Architecture}}"); err != nil {
		return errors.New("Docker engine unavailable; start Docker and check access")
	}
	if _, err = i.Run(ctx, "docker", "compose", "version"); err != nil {
		return errors.New("Docker Compose plugin required")
	}
	if command == "status" {
		if err = i.check(ctx, false); err != nil {
			return err
		}
		fmt.Println("Runtime ready (signed snapshot and required GeoLite). Public readiness is checked separately by B11.")
		return nil
	}
	if os.Geteuid() != 0 {
		return errors.New("provisioning/update/rollback requires root")
	}
	if command == "rollback" {
		return i.rollback(ctx)
	}
	if i.State.Credential == "" {
		fmt.Fprint(os.Stderr, "Enrollment token (hidden): ")
		token, e := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if e != nil {
			return errors.New("token requires an interactive terminal")
		}
		defer clear(token)
		if err = i.enroll(ctx, string(token)); err != nil {
			return err
		}
	}
	return i.provision(ctx, command == "update")
}
func (i *Installer) validate() error {
	b := i.State.Bootstrap
	key, e := base64.RawURLEncoding.DecodeString(b.Key)
	if e != nil || len(key) != ed25519.PublicKeySize || b.DomainID == "" || len(b.Hostname) > 253 || !hostPattern.MatchString(b.Hostname) || b.Method != "cli" || !artifacts.ValidVersion(b.Version) {
		return errors.New("bootstrap requires CLI domain, valid release and pinned public key")
	}
	if i.State.Development && i.State.Proxy != "external" {
		return errors.New("development requires --proxy external")
	}
	cfg := runtime.Config{Listen: "127.0.0.1:8091", ControlURL: b.ControlURL, DomainID: b.DomainID, Hostname: b.Hostname, Credential: randomToken(), VerificationKey: b.Key, DataDir: "/var/lib/shortlink-agent", Development: i.State.Development}
	if e = cfg.Validate(); e != nil {
		return e
	}
	if i.State.Port < 1024 || i.State.Port > 65535 || i.State.UID != 10001 || (i.State.Proxy != "external" && i.State.Proxy != "caddy") {
		return errors.New("invalid installation state")
	}
	if i.State.ProxyImage != "" && !proxyPattern.MatchString(i.State.ProxyImage) {
		return errors.New("invalid proxy image")
	}
	return nil
}
func secureDirectory(path string) error {
	// Reject symlink ancestors before root-owned credential writes.
	for p := path; p != "/"; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("installation path contains symlink")
		}
	}
	info, e := os.Stat(path)
	if e == nil {
		if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return errors.New("installation root must be a dedicated directory with mode 0700")
		}
		return nil
	}
	if !os.IsNotExist(e) {
		return e
	}
	return os.MkdirAll(path, 0700)
}
func privateRead(path string, limit int64) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > limit {
		return nil, errors.New("private file must be regular, bounded and mode 0600")
	}
	return os.ReadFile(path)
}
func atomic(path string, raw []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".pending-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
func (i *Installer) save() error {
	raw, e := json.MarshalIndent(i.State, "", "  ")
	if e != nil {
		return e
	}
	return atomic(filepath.Join(i.Root, "installation.json"), raw)
}
func randomToken() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	c.Stdout = &out
	c.Stderr = &out
	err := c.Run()
	if err != nil {
		return nil, fmt.Errorf("%s operation failed; inspect service/package status locally", name)
	}
	return out.Bytes(), nil
}
func (i *Installer) request(ctx context.Context, method, path string, body any, auth bool) (*http.Response, error) {
	var input io.Reader
	if body != nil {
		raw, e := json.Marshal(body)
		if e != nil {
			return nil, e
		}
		input = bytes.NewReader(raw)
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(i.State.Bootstrap.ControlURL, "/")+path, input)
	if e != nil {
		return nil, e
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+i.State.Credential)
	}
	res, e := i.Client.Do(req)
	if e != nil {
		return nil, errors.New("center connection failed")
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return nil, fmt.Errorf("center returned HTTP %d; check token, domain verification, credential or release", res.StatusCode)
	}
	return res, nil
}
func (i *Installer) enroll(ctx context.Context, token string) error {
	contextResponse, e := i.request(ctx, "POST", "/v1/agents/install-context", map[string]string{"token": token}, false)
	if e != nil {
		return e
	}
	var installContext struct {
		InstallationID string    `json:"installationId"`
		Bootstrap      Bootstrap `json:"bootstrap"`
	}
	e = json.NewDecoder(io.LimitReader(contextResponse.Body, 32<<10)).Decode(&installContext)
	contextResponse.Body.Close()
	b := i.State.Bootstrap
	id, decodeErr := base64.RawURLEncoding.DecodeString(installContext.InstallationID)
	if e != nil || decodeErr != nil || len(id) != 32 || installContext.Bootstrap.DomainID != b.DomainID || installContext.Bootstrap.Hostname != b.Hostname || installContext.Bootstrap.Key != b.Key || installContext.Bootstrap.ControlURL != b.ControlURL || installContext.Bootstrap.Method != "cli" {
		return errors.New("installation context does not match pinned domain/origin/key")
	}
	i.State.InstallationID = installContext.InstallationID
	if e = i.save(); e != nil {
		return e
	}
	res, e := i.request(ctx, "POST", "/v1/agents/enroll", map[string]any{"routingVersion": 4, "token": token, "installationId": i.State.InstallationID, "installationProof": i.State.Proof, "hostname": i.State.Bootstrap.Hostname, "method": "cli", "version": runtime.Version}, false)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	var v struct {
		Credential string    `json:"credential"`
		Bootstrap  Bootstrap `json:"bootstrap"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 32<<10)).Decode(&v) != nil {
		return errors.New("invalid enrollment response")
	}
	if v.Bootstrap.DomainID != b.DomainID || v.Bootstrap.Hostname != b.Hostname || v.Bootstrap.Key != b.Key || v.Bootstrap.ControlURL != b.ControlURL || v.Bootstrap.Method != "cli" {
		return errors.New("enrollment bootstrap mismatch")
	}
	secret, e := base64.RawURLEncoding.DecodeString(v.Credential)
	if e != nil || len(secret) != 32 {
		return errors.New("invalid enrollment credential")
	}
	i.State.Credential = v.Credential
	return i.save()
}
func (i *Installer) manifest(ctx context.Context) (artifacts.Manifest, error) {
	res, e := i.request(ctx, "GET", "/v1/assets/agent/manifest", nil, true)
	if e != nil {
		return artifacts.Manifest{}, e
	}
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
	if e != nil || len(raw) > 64<<10 {
		return artifacts.Manifest{}, errors.New("invalid manifest size")
	}
	key, _ := base64.RawURLEncoding.DecodeString(i.State.Bootstrap.Key)
	envelope, err := artifacts.Verify(raw, key)
	if err == nil {
		dir := filepath.Join(i.Root, "releases", envelope.Version)
		if e := secureDirectory(dir); e != nil {
			return artifacts.Manifest{}, e
		}
		if e := atomic(filepath.Join(dir, "manifest.json"), raw); e != nil {
			return artifacts.Manifest{}, e
		}
	}
	return envelope.Manifest, err
}
func (i *Installer) download(ctx context.Context, meta artifacts.File) (string, error) {
	dir := filepath.Join(i.Root, "releases", meta.Version)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	path := filepath.Join(dir, meta.Name)
	if artifacts.CheckFile(path, meta) == nil {
		return path, nil
	}
	var disk syscall.Statfs_t
	if e := syscall.Statfs(dir, &disk); e != nil {
		return "", e
	}
	if disk.Bavail*uint64(disk.Bsize) < uint64(meta.Size)+(32<<20) {
		return "", errors.New("insufficient free disk space for verified artifact")
	}
	res, e := i.request(ctx, "GET", meta.DownloadURL, nil, true)
	if e != nil {
		return "", e
	}
	defer res.Body.Close()
	f, e := os.CreateTemp(dir, ".download-")
	if e != nil {
		return "", e
	}
	defer os.Remove(f.Name())
	sum := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, sum), io.LimitReader(res.Body, meta.Size+1))
	closeErr := f.Close()
	if e != nil || closeErr != nil || n != meta.Size || hex.EncodeToString(sum.Sum(nil)) != meta.SHA256 {
		return "", errors.New("artifact download checksum/size rejected")
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return "", e
	}
	return path, nil
}
func (i *Installer) provision(ctx context.Context, update bool) error {
	m, e := i.manifest(ctx)
	if e != nil {
		return e
	}
	if !m.InstallerReady {
		return errors.New("selected release has no automatic installer; publish a B10-capable release")
	}
	if !update && i.State.Version == "" && m.Version != i.State.Bootstrap.Version {
		return errors.New("release changed since bootstrap; download a fresh bootstrap before enrollment")
	}
	if i.State.Image != "" && !update {
		if e = i.writeRuntime(); e != nil {
			return e
		}
		if e = i.compose(ctx, "up", "-d"); e != nil {
			return e
		}
		if e = i.check(ctx, true); e != nil {
			return e
		}
		return i.installBinary(filepath.Join(i.Root, "releases", i.State.Version, "agent-linux-"+goruntime.GOARCH))
	}
	if update && i.State.Version == m.Version {
		return i.check(ctx, true)
	}
	var meta artifacts.File
	for _, a := range m.Artifacts {
		if a.Name == "image-linux-"+goruntime.GOARCH+".tar" {
			meta = a
		}
	}
	if meta.ImageID == nil {
		return errors.New("platform image unavailable")
	}
	archive, e := i.download(ctx, meta)
	if e != nil {
		return e
	}
	if _, e = i.Run(ctx, "docker", "load", "--input", archive); e != nil {
		return e
	}
	image := ""
	for _, id := range []string{*meta.ImageID, *meta.ImageConfigDigest} {
		raw, err := i.Run(ctx, "docker", "image", "inspect", id, "--format", "{{.Id}} {{.Os}}/{{.Architecture}}")
		if err == nil && strings.TrimSpace(string(raw)) == id+" linux/"+goruntime.GOARCH {
			image = id
			break
		}
	}
	if image == "" {
		return errors.New("loaded image identity/platform mismatch")
	}
	var native string
	for _, a := range m.Artifacts {
		if a.Name == "agent-linux-"+goruntime.GOARCH {
			native, e = i.download(ctx, a)
			if e != nil {
				return e
			}
		}
	}
	if i.State.Proxy == "caddy" && i.State.ProxyImage == "" {
		if _, e = i.Run(ctx, "docker", "pull", "caddy:2"); e != nil {
			return e
		}
		raw, err := i.Run(ctx, "docker", "image", "inspect", "caddy:2", "--format", "{{json .RepoDigests}}")
		if err != nil {
			return err
		}
		var refs []string
		if json.Unmarshal(raw, &refs) != nil {
			return errors.New("invalid Caddy image metadata")
		}
		for _, ref := range refs {
			if proxyPattern.MatchString(ref) {
				i.State.ProxyImage = ref
				break
			}
		}
		if i.State.ProxyImage == "" {
			return errors.New("official Caddy immutable digest unavailable")
		}
	}
	if i.State.Proxy == "caddy" {
		if _, e = i.Run(ctx, "docker", "pull", i.State.ProxyImage); e != nil {
			return e
		}
	}
	old := i.State
	i.State.PreviousImage = old.Image
	i.State.PreviousVersion = old.Version
	i.State.Image = image
	i.State.Version = m.Version
	if e = i.writeRuntime(); e != nil {
		i.State = old
		return e
	}
	if e = i.save(); e != nil {
		i.State = old
		return e
	}
	if e = i.compose(ctx, "up", "-d", "--remove-orphans"); e == nil {
		e = i.check(ctx, true)
	}
	if e != nil && old.Image != "" {
		i.State = old
		if restoreErr := i.writeRuntime(); restoreErr == nil {
			restoreErr = i.save()
			if restoreErr == nil {
				restoreErr = i.compose(ctx, "up", "-d")
			}
			if restoreErr != nil {
				return fmt.Errorf("update failed; restoring previous runtime also failed: %w", restoreErr)
			}
		} else {
			return restoreErr
		}
		return fmt.Errorf("update failed; previous image restored, data retained: %w", e)
	}
	if e != nil {
		return e
	}
	if native != "" {
		if e = i.installBinary(native); e != nil {
			return e
		}
	}
	fmt.Println("Agent installed:", i.State.Version, "— signed config/GeoLite and runtime health verified.")
	return nil
}
func (i *Installer) writeRuntime() error {
	for _, dir := range []string{"config", "data", "caddy-data", "caddy-config"} {
		if e := secureDirectory(filepath.Join(i.Root, dir)); e != nil {
			return e
		}
	}
	data := filepath.Join(i.Root, "data")
	if e := os.Chown(data, i.State.UID, i.State.UID); e != nil {
		return e
	}
	cfg := runtime.Config{Listen: fmt.Sprintf("127.0.0.1:%d", i.State.Port), ControlURL: i.State.Bootstrap.ControlURL, DomainID: i.State.Bootstrap.DomainID, Hostname: i.State.Bootstrap.Hostname, Credential: i.State.Credential, VerificationKey: i.State.Bootstrap.Key, DataDir: "/var/lib/shortlink-agent", GeoIPRequired: true, Development: i.State.Development}
	cfg.TrustedProxies = []string{"127.0.0.1/32", "::1/128"}
	raw, e := json.MarshalIndent(cfg, "", "  ")
	if e != nil {
		return e
	}
	if e = atomic(filepath.Join(i.Root, "config", "agent.json"), raw); e != nil {
		return e
	}
	if e = os.Chown(filepath.Join(i.Root, "config", "agent.json"), i.State.UID, i.State.UID); e != nil {
		return e
	}
	caddy := fmt.Sprintf("{\n admin off\n}\n%s {\n reverse_proxy 127.0.0.1:%d {\n header_up X-Forwarded-For {remote_host}\n }\n}\n", i.State.Bootstrap.Hostname, i.State.Port)
	// Existing proxy sites and operator settings belong to the operator.
	caddyPath := filepath.Join(i.Root, "Caddyfile")
	if _, err := os.Lstat(caddyPath); errors.Is(err, os.ErrNotExist) {
		if e = atomic(caddyPath, []byte(caddy)); e != nil {
			return e
		}
	} else if err != nil {
		return err
	}
	composePath := filepath.Join(i.Root, "compose.yml")
	content := []byte(i.composeFile())
	if existing, err := privateRead(composePath, 128<<10); err == nil {
		content, e = updateComposeImage(existing, i.State.Image, i.State.ProxyImage)
		if e != nil {
			return e
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return atomic(composePath, content)
}
func quoted(s string) string {
	raw, _ := json.Marshal(strings.ReplaceAll(s, "$", "$$"))
	return string(raw)
}
func (i *Installer) composeFile() string {
	result := fmt.Sprintf("services:\n  agent:\n    image: %s\n    pull_policy: never\n    network_mode: host\n    restart: unless-stopped\n    user: \"10001:10001\"\n    read_only: true\n    cap_drop: [ALL]\n    security_opt: [\"no-new-privileges:true\"]\n    volumes:\n      - type: bind\n        source: %s\n        target: /run/shortlink/agent.json\n        read_only: true\n      - type: bind\n        source: %s\n        target: /var/lib/shortlink-agent\n    logging:\n      driver: json-file\n      options: {max-size: \"10m\", max-file: \"3\"}\n", quoted(i.State.Image), quoted(filepath.Join(i.Root, "config/agent.json")), quoted(filepath.Join(i.Root, "data")))
	if i.State.Proxy == "caddy" {
		result += fmt.Sprintf("  proxy:\n    image: %s\n    pull_policy: never\n    network_mode: host\n    restart: unless-stopped\n    volumes:\n      - type: bind\n        source: %s\n        target: /etc/caddy/Caddyfile\n        read_only: true\n      - type: bind\n        source: %s\n        target: /data\n      - type: bind\n        source: %s\n        target: /config\n    logging:\n      driver: json-file\n      options: {max-size: \"10m\", max-file: \"3\"}\n", quoted(i.State.ProxyImage), quoted(filepath.Join(i.Root, "Caddyfile")), quoted(filepath.Join(i.Root, "caddy-data")), quoted(filepath.Join(i.Root, "caddy-config")))
	}
	return result
}
func (i *Installer) compose(ctx context.Context, args ...string) error {
	sum := sha256.Sum256([]byte(i.Root))
	prefix := []string{"compose", "--project-name", "sl-" + hex.EncodeToString(sum[:6]), "--file", filepath.Join(i.Root, "compose.yml")}
	_, e := i.Run(ctx, "docker", append(prefix, args...)...)
	return e
}
func (i *Installer) check(ctx context.Context, wait bool) error {
	deadline := time.Now().Add(i.Timeout)
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for {
		containerReady := false
		sum := sha256.Sum256([]byte(i.Root))
		ids, e := i.Run(ctx, "docker", "compose", "--project-name", "sl-"+hex.EncodeToString(sum[:6]), "--file", filepath.Join(i.Root, "compose.yml"), "ps", "--quiet", "agent")
		if e == nil && len(strings.Fields(string(ids))) == 1 {
			out, err := i.Run(ctx, "docker", "inspect", strings.TrimSpace(string(ids)), "--format", "{{.State.Running}} {{.Config.Image}}")
			containerReady = err == nil && strings.TrimSpace(string(out)) == "true "+i.State.Image
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://127.0.0.1:%d/health/ready", i.State.Port), nil)
		req.Host = i.State.Bootstrap.Hostname
		res, e := client.Do(req)
		ready := containerReady && e == nil && res.StatusCode == 200
		if e == nil {
			res.Body.Close()
		}
		if ready && i.State.Proxy == "caddy" {
			req, _ = http.NewRequestWithContext(ctx, "GET", "https://"+i.State.Bootstrap.Hostname+"/health/ready", nil)
			res, e = client.Do(req)
			ready = e == nil && res.StatusCode == 200
			if e == nil {
				res.Body.Close()
			}
		}
		if ready {
			return nil
		}
		if !wait || time.Now().After(deadline) {
			return errors.New("readiness pending: check center credential/snapshot/GeoLite; Caddy also requires DNS, public ports 80/443 and valid TLS")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
func (i *Installer) rollback(ctx context.Context) error {
	if i.State.PreviousImage == "" {
		return errors.New("no previous installed image; persistent data has not been removed")
	}
	old := i.State
	previousBinary := filepath.Join(i.Root, "releases", old.PreviousVersion, "agent-linux-"+goruntime.GOARCH)
	if _, e := i.binaryBytes(previousBinary, old.PreviousVersion); e != nil {
		return e
	}
	i.State.Image, i.State.PreviousImage = old.PreviousImage, old.Image
	i.State.Version, i.State.PreviousVersion = old.PreviousVersion, old.Version
	if e := i.writeRuntime(); e != nil {
		return e
	}
	if e := i.save(); e != nil {
		return e
	}
	e := i.compose(ctx, "up", "-d")
	if e == nil {
		e = i.check(ctx, true)
	}
	if e != nil {
		i.State = old
		if restoreErr := i.writeRuntime(); restoreErr != nil {
			return restoreErr
		}
		if restoreErr := i.save(); restoreErr != nil {
			return restoreErr
		}
		if restoreErr := i.compose(ctx, "up", "-d"); restoreErr != nil {
			return fmt.Errorf("rollback failed and restoring current image failed: %w", restoreErr)
		}
		return fmt.Errorf("rollback failed; current image restored and data retained: %w", e)
	}
	return i.installBinary(filepath.Join(i.Root, "releases", i.State.Version, "agent-linux-"+goruntime.GOARCH))
}
func (i *Installer) binaryBytes(source, version string) ([]byte, error) {
	manifestRaw, e := privateRead(filepath.Join(filepath.Dir(source), "manifest.json"), 64<<10)
	if e != nil {
		return nil, e
	}
	key, _ := base64.RawURLEncoding.DecodeString(i.State.Bootstrap.Key)
	manifest, e := artifacts.Verify(manifestRaw, key)
	if e != nil {
		return nil, e
	}
	if manifest.Version != version {
		return nil, errors.New("cached CLI manifest version mismatch")
	}
	var meta artifacts.File
	for _, file := range manifest.Artifacts {
		if file.Name == filepath.Base(source) {
			meta = file
		}
	}
	if meta.Name == "" {
		return nil, errors.New("cached CLI missing signed metadata")
	}
	raw, e := privateRead(source, meta.Size)
	if e != nil {
		return nil, e
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != meta.Size || hex.EncodeToString(sum[:]) != meta.SHA256 {
		return nil, errors.New("cached CLI checksum rejected")
	}
	return raw, nil
}
func (i *Installer) installBinary(source string) error {
	raw, e := i.binaryBytes(source, i.State.Version)
	if e != nil {
		return e
	}
	target := filepath.Join(i.Root, "agent")
	if e = atomic(target, raw); e != nil {
		return e
	}
	return os.Chmod(target, 0700)
}
func (i *Installer) preflight(ctx context.Context, installDocker bool) error {
	var disk syscall.Statfs_t
	if e := syscall.Statfs(i.Root, &disk); e != nil {
		return e
	}
	if disk.Bavail*uint64(disk.Bsize) < 512<<20 {
		return errors.New("installation requires at least 512 MiB free disk space")
	}
	if _, e := i.Run(ctx, "docker", "info"); e != nil {
		if !installDocker {
			return errors.New("Docker unavailable; start existing Docker, or on clean Ubuntu/Debian pass --install-docker")
		}
		if e = i.installDocker(ctx); e != nil {
			return e
		}
	}
	ports := []int{i.State.Port}
	if i.State.Proxy == "caddy" {
		ports = append(ports, 80, 443)
	}
	for _, port := range ports {
		l, e := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if e != nil {
			return fmt.Errorf("port %d is occupied; choose another runtime port or --proxy external; no existing service was stopped", port)
		}
		l.Close()
	}
	if i.State.Proxy == "caddy" {
		ips, e := net.DefaultResolver.LookupHost(ctx, i.State.Bootstrap.Hostname)
		if e != nil || len(ips) == 0 {
			return errors.New("hostname DNS must resolve before automatic HTTPS provisioning")
		}
	}
	return nil
}
