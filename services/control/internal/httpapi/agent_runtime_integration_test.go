package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"shortlink.local/artifacts"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"shortlink.local/geoip"
	"shortlink.local/geoip/testutil"
	"shortlink.local/policy"
	"shortlink.local/risk"
)

// Exercises publishing and shared policy through a real Go agent and isolated PostgreSQL.
func TestGoAgentWithRealControlPlane(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	domain, err := db.CreateDomain(ctx, security.Token(), "agent.example.test", "local")
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := db.Challenge(ctx, domain.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Verify(ctx, domain.ID, challenge.Value, "test"); err != nil {
		t.Fatal(err)
	}
	link, err := db.CreateLink(ctx, security.Token(), domain.ID, "hello", "https://example.org/real-control-test")
	if err != nil {
		t.Fatal(err)
	}
	token, err := db.IssueToken(ctx, domain.ID, "cli", "test")
	if err != nil {
		t.Fatal(err)
	}
	agentBinding, credential, err := db.Enroll(ctx, token.Token, token.InstallationID, security.Token(), domain.Hostname, "cli", "test")
	if err != nil {
		t.Fatal(err)
	}
	published, err := db.PublishLink(ctx, link.ID, link.Revision, 0, "test")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := db.UpdateLink(ctx, link.ID, domain.ID, link.Slug, "https://example.org/new", published.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishLink(ctx, link.ID, draft.Revision, 0, "test"); err != nil {
		t.Fatal(err)
	}
	newer, err := db.Link(ctx, link.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishLink(ctx, link.ID, newer.Revision, published.Revision, "test"); err != nil {
		t.Fatal(err)
	}
	rules, err := db.CreateRuleSet(ctx, "link", &link.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	rule := policy.Rule{ID: "ios-block", Name: "iPhone rule", Priority: 10, Country: "*", Device: "mobile", OS: "ios", Browser: "safari", Risk: "*", Action: "block", Enabled: true}
	rules, err = db.UpdateRuleSet(ctx, rules.ID, rules.Revision, []policy.Rule{rule}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishRules(ctx, rules.ID, rules.Revision, 0, "test"); err != nil {
		t.Fatal(err)
	}
	// Install B13 risk rules in observation mode before starting the real process.
	preRisk, e := db.Domain(ctx, domain.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Heartbeat(ctx, agentBinding, preRisk.DesiredRevision, "b13-fixture-capability", 0, 0, 0, nil, nil, store.QueueHealth{ProtectionVersion: 1}); e != nil {
		t.Fatal(e)
	}
	riskSet, e := db.CreateRuleSet(ctx, "domain", &domain.ID, "test")
	if e != nil {
		t.Fatal(e)
	}
	riskRule := policy.Rule{ID: "crawler-observe", Name: "Crawler observation", Priority: 5, Country: "*", Device: "*", OS: "*", Browser: "*", Risk: "known_bot", Action: "block", Enabled: true}
	riskSet, e = db.UpdateRuleSet(ctx, riskSet.ID, riskSet.Revision, []policy.Rule{riskRule}, "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.PublishRules(ctx, riskSet.ID, riskSet.Revision, 0, "test"); e != nil {
		t.Fatal(e)
	}
	desired, err := db.Domain(ctx, domain.ID)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := db.Seed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.cfg.GeoIPRoot = t.TempDir()
	if err := testutil.Staging(s.cfg.GeoIPRoot, time.Now().Add(-time.Hour).Unix(), "ID"); err != nil {
		t.Fatal(err)
	}
	if _, err := geoip.Publish(s.cfg.GeoIPRoot, seed, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.cfg.RiskFeedRoot = t.TempDir()
	if e := risk.AtomicFile(filepath.Join(s.cfg.RiskFeedRoot, "staging", "tor.txt"), []byte("8.8.8.8\n")); e != nil {
		t.Fatal(e)
	}
	if _, _, e := risk.Publish(s.cfg.RiskFeedRoot, seed, time.Now()); e != nil {
		t.Fatal(e)
	}
	center := httptest.NewServer(s)
	defer center.Close()
	temp := t.TempDir()
	binary := filepath.Join(temp, "agent")
	build := exec.Command("go", "build", "-o", binary, "./cmd/agent")
	build.Dir = "../../../agent"
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build agent: %v %s", err, output)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	config := map[string]any{"listen": addr, "controlUrl": center.URL, "domainId": domain.ID, "hostname": domain.Hostname, "credential": credential, "verificationKey": base64.RawURLEncoding.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)), "dataDir": filepath.Join(temp, "state"), "trustedProxies": []string{}, "development": true, "geoipRequired": true}
	raw, _ := json.Marshal(config)
	configPath := filepath.Join(temp, "agent.json")
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	start := func() *exec.Cmd {
		if runner := os.Getenv("CONTROL_TEST_INSTALLER_RUNNER"); runner != "" {
			input := os.Getenv("AGENT_TEST_BUILD_INPUT")
			if input == "" {
				t.Fatal("installer test requires real AGENT_TEST_BUILD_INPUT")
			}
			s.cfg.AgentAssetRoot = t.TempDir()
			release, err := artifacts.Publish(s.cfg.AgentAssetRoot, input, seed, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(temp, "installation")
			if err = os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			_, portString, _ := net.SplitHostPort(addr)
			port, _ := strconv.Atoi(portString)
			state := map[string]any{"bootstrap": map[string]any{"domainId": domain.ID, "hostname": domain.Hostname, "controlPlaneUrl": center.URL, "manifestVerificationKey": config["verificationKey"], "artifactVersion": release.Version, "method": "cli"}, "installationId": token.InstallationID, "installationProof": security.Token(), "credential": credential, "proxy": "external", "port": port, "uid": 10001, "development": true}
			raw, _ := json.Marshal(state)
			if err = os.WriteFile(filepath.Join(root, "installation.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			mounts := []string{"run", "--rm", "--network", "host", "--user", "0:0", "--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock", "--mount", "type=bind,source=" + temp + ",target=" + temp}
			t.Cleanup(func() {
				// Compose project name is derived from the installation root, not the directory basename.
				sum := sha256.Sum256([]byte(root))
				args := append(append([]string{}, mounts...), "--entrypoint", "docker", runner, "compose", "--project-name", "sl-"+hex.EncodeToString(sum[:6]), "--file", filepath.Join(root, "compose.yml"), "down")
				exec.Command("docker", args...).Run()
				args = append(append([]string{}, mounts...), "--entrypoint", "chown", runner, "-R", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), root)
				exec.Command("docker", args...).Run()
			})
			for step, command := range []string{"install", "install", "status"} {
				entrypoint := binary
				if step > 0 {
					entrypoint = filepath.Join(root, "agent")
				}
				args := append(append([]string{}, mounts...), "--entrypoint", entrypoint, runner, command, "--root", root, "--timeout", "30s")
				if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
					t.Fatalf("installer %s: %v %s", command, err, out)
				}
			}
			if _, err = os.Stat(filepath.Join(root, "agent")); err != nil {
				t.Fatal("trusted installed CLI absent", err)
			}
			return nil
		}
		cmd := exec.Command(binary, "serve", "--config", configPath)
		if image := os.Getenv("CONTROL_TEST_AGENT_IMAGE"); image != "" {
			name := "shortlink-agent-test-" + security.Token()[:12]
			cmd = exec.Command("docker", "run", "--rm", "--name", name, "--network", "host", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--mount", "type=bind,source="+temp+",target="+temp, image, "serve", "--config", configPath)
			t.Cleanup(func() { exec.Command("docker", "stop", "--time", "2", name).Run() })
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cmd.Process.Signal(os.Interrupt); cmd.Wait() })
		return cmd
	}
	start()
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	waitRedirect := func() {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			req, _ := http.NewRequest("GET", "http://"+addr+"/hello?ignored=1", nil)
			req.Host = domain.Hostname
			req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36")
			resp, err := client.Do(req)
			if err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == 302 && resp.Header.Get("Location") == link.Destination && resp.Header.Get("Cache-Control") == "no-store" {
					return
				}
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal("agent did not apply real signed configuration")
	}
	waitRedirect()
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, err := db.Domain(ctx, domain.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.AppliedRevision != nil && *current.AppliedRevision == desired.DesiredRevision {
			if current.Readiness == "ready" {
				t.Fatal("agent falsely marked production ready")
			}
			var reported *string
			if err := db.DB.QueryRow(ctx, "SELECT geo_release FROM agents WHERE domain_id=$1 AND revoked_at IS NULL", domain.ID).Scan(&reported); err != nil {
				t.Fatal(err)
			}
			if reported != nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("real heartbeat not recorded")
		}
		time.Sleep(25 * time.Millisecond)
	}
	ua := "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
	simulated, err := db.Simulate(ctx, link.ID, policy.Detect(ua), "", nil)
	if err != nil || simulated.Action != "block" {
		t.Fatalf("simulation: %+v %v", simulated, err)
	}
	req, _ := http.NewRequest("GET", "http://"+addr+"/hello", nil)
	req.Host = domain.Hostname
	req.Header.Set("User-Agent", ua)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("agent/simulator policy mismatch", resp.StatusCode)
	}
	// iPad uses iOS too, but must not match the mobile-only block.
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatal("iPad incorrectly blocked", resp.StatusCode)
	}
	// Real UA classifier observes without blocking, then follows a published enforcement mode.
	req.Header.Set("User-Agent", "Googlebot/2.1")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatal("crawler observation changed redirect", resp.StatusCode)
	}
	protection, e := db.Protection(ctx, domain.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.SetProtection(ctx, domain.ID, protection.Revision, policy.Protection{Mode: "enforce", RatePerSecond: 20, Burst: 40}, "test"); e != nil {
		t.Fatal(e)
	}
	deadline = time.Now().Add(40 * time.Second)
	for {
		resp, e = client.Do(req)
		if e == nil {
			resp.Body.Close()
			if resp.StatusCode == 403 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("real agent did not enforce published crawler rule")
		}
		time.Sleep(250 * time.Millisecond)
	}
	// The real runtime authenticates readiness on the private local test transport.
	nonce := security.Token()
	digest := policy.ProbeDigest(credential)
	probeRequest, _ := http.NewRequest("GET", "http://"+addr+"/.well-known/shortlink-ready", nil)
	probeRequest.Header.Set("X-Shortlink-Nonce", nonce)
	probeRequest.Header.Set("X-Shortlink-Proof", policy.ProbeMAC(digest, "request", []byte(nonce)))
	probeResponse, err := client.Do(probeRequest)
	if err != nil {
		t.Fatal(err)
	}
	probeRaw, _ := io.ReadAll(probeResponse.Body)
	probeResponse.Body.Close()
	if probeResponse.StatusCode != 200 || !policy.CheckProbeMAC(policy.ProbeMAC(digest, "response", probeRaw), probeResponse.Header.Get("X-Shortlink-Proof")) {
		t.Fatal("real runtime probe authentication failed")
	}
	var probeState policy.ProbeState
	json.Unmarshal(probeRaw, &probeState)
	if !probeState.Ready || probeState.Nonce != nonce || probeState.DomainID != domain.ID || probeState.GeoRelease == nil {
		t.Fatal("real runtime readiness missing", probeState)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		var n int
		if e := db.DB.QueryRow(ctx, "SELECT count(*) FROM traffic_events WHERE domain_id=$1", domain.ID).Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real agent events not ingested")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, e := db.AggregateEvents(ctx, 1000); e != nil {
		t.Fatal(e)
	}
	var actions int
	if e := db.DB.QueryRow(ctx, "SELECT count(DISTINCT action) FROM traffic_aggregates WHERE domain_id=$1 AND grain='minute'", domain.ID).Scan(&actions); e != nil || actions != 2 {
		t.Fatal("real agent block/redirect aggregates", actions, e)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		var n int
		if e := db.DB.QueryRow(ctx, "SELECT count(*) FROM traffic_events WHERE domain_id=$1 AND payload->>'feedVersion' IS NOT NULL AND payload->'risk'->>'candidateRuleId'='crawler-observe'", domain.ID).Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real signed feed/risk evidence missing")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, e := db.AggregateEvents(ctx, 1000); e != nil {
		t.Fatal(e)
	}
	summary, e := db.RiskSummary(ctx, domain.ID)
	if e != nil || len(summary) == 0 {
		t.Fatal("real risk aggregates missing", e)
	}
	center.Close()
	waitRedirect() // redirect survives central API outage within its lease
}
