package httpapi

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"strings"
	"testing"
)

func TestOperatorRotationMigrationAndRevoke(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	owner, err := db.OwnerByEmail(ctx, "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.CreateDomain(ctx, security.Token(), "maintenance.example.com", "local")
	if err != nil {
		t.Fatal(err)
	}
	raw := security.Token()
	a := store.Agent{ID: security.Token(), DomainID: d.ID, Method: "cli", Version: "test"}
	if _, err = db.DB.Exec(ctx, "INSERT INTO agents(id,domain_id,installation_id,credential_hash,method,version) VALUES($1,$2,$3,$4,$5,$6)", a.ID, a.DomainID, security.Token(), security.Digest(raw), a.Method, a.Version); err != nil {
		t.Fatal(err)
	}
	c, csrf := login(t, s, nil)
	input, _ := json.Marshal(map[string]string{"password": testPassword, "agentId": a.ID})
	status(t, request(s, "POST", "/v1/admin/control/rotations", string(input), testOrigin, "", c), 403)
	r := request(s, "POST", "/v1/admin/control/rotations", string(input), testOrigin, csrf, c)
	status(t, r, 200)
	if strings.Contains(r.Body.String(), raw) || strings.Contains(r.Body.String(), "credential") {
		t.Fatal("admin credential leak")
	}
	status(t, request(s, "POST", "/v1/admin/control/rotations", string(input), testOrigin, csrf, c), 409)
	// Before persistence, old credential cannot acknowledge the rotation.
	if err = db.AckControl(ctx, a, raw, "", ""); err != store.ErrFactor {
		t.Fatal("old credential ack accepted", err)
	}
	s.cfg.Development = true
	target := httptest.NewServer(s)
	defer target.Close()
	s.cfg.PublicURL = target.URL
	input, _ = json.Marshal(map[string]string{"password": testPassword, "origin": target.URL})
	r = request(s, "POST", "/v1/admin/control/migrations", string(input), testOrigin, csrf, c)
	status(t, r, 200)
	directory := t.TempDir()
	migration := filepath.Join(directory, "migration.json")
	os.WriteFile(migration, r.Body.Bytes(), 0600)
	seed, _ := db.Seed(ctx)
	key := base64.RawURLEncoding.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	config := filepath.Join(directory, "config.json")
	payload, _ := json.Marshal(map[string]any{"controlUrl": "http://127.0.0.1:1", "domainId": d.ID, "hostname": d.Hostname, "credential": raw, "development": true, "verificationKey": key, "dataDir": "keep-data-path", "listen": "127.0.0.1:8099"})
	os.WriteFile(config, payload, 0600)
	run := func() {
		t.Helper()
		command := exec.Command("python3", "../../../../scripts/maintain-agent.py", "--config", config, "--migration", migration, "--runtime-stopped")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("maintenance failed: %v %s", err, output)
		}
	}
	// A signed migration alone must not leak a bearer to a destination lacking the pinned key.
	malicious := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("bearer leaked before identity proof")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"payload":"invalid","signature":"invalid"}`))
	}))
	defer malicious.Close()
	evilDoc, err := db.StageMigration(ctx, owner.ID, "", malicious.URL)
	if err != nil {
		t.Fatal(err)
	}
	evilPath := filepath.Join(directory, "evil.json")
	evilPayload, _ := json.Marshal(evilDoc)
	os.WriteFile(evilPath, evilPayload, 0600)
	evilCommand := exec.Command("python3", "../../../../scripts/maintain-agent.py", "--config", config, "--migration", evilPath, "--runtime-stopped")
	if err = evilCommand.Run(); err == nil {
		t.Fatal("unproven control identity accepted")
	}
	// A tampered document must not rewrite config or send credentials to a different origin.
	original, _ := os.ReadFile(migration)
	var envelope map[string]string
	json.Unmarshal(original, &envelope)
	envelope["signature"] = "invalid"
	tampered, _ := json.Marshal(envelope)
	os.WriteFile(migration, tampered, 0600)
	command := exec.Command("python3", "../../../../scripts/maintain-agent.py", "--config", config, "--migration", migration, "--runtime-stopped")
	if err = command.Run(); err == nil {
		t.Fatal("tampered migration accepted")
	}
	unchanged, _ := os.ReadFile(config)
	if string(unchanged) != string(payload) {
		t.Fatal("config mutated before verification")
	}
	os.WriteFile(migration, original, 0600)
	run()
	run() // Lost acknowledgment may be retried with saved credential.
	var saved map[string]any
	bytes, _ := os.ReadFile(config)
	json.Unmarshal(bytes, &saved)
	next := saved["credential"].(string)
	if next == raw || saved["dataDir"] != "keep-data-path" {
		t.Fatal("rotation or paths incorrect")
	}
	if _, err = db.AuthenticateAgent(ctx, raw); err == nil {
		t.Fatal("old credential still valid")
	}
	if _, err = db.AuthenticateAgent(ctx, next); err != nil {
		t.Fatal(err)
	}
	states, err := db.ControlStates(ctx)
	if err != nil || len(states) != 1 || states[0].RotationPending || states[0].MigrationID == "" {
		t.Fatal("missing acknowledgment", err)
	}
	phpPath := filepath.Join(directory, "php-config.json")
	phpPayload, _ := json.Marshal(map[string]any{"controlPlaneUrl": "http://127.0.0.1:1", "domainId": d.ID, "agentId": a.ID, "hostname": d.Hostname, "credential": next, "developmentLoopback": true, "trustedProxies": []string{}, "version": "v0.17.0-b17"})
	os.WriteFile(phpPath, phpPayload, 0600)
	phpBootstrap := filepath.Join(directory, "bootstrap.json")
	bootstrapPayload, _ := json.Marshal(map[string]any{"controlPlaneUrl": "http://127.0.0.1:1", "hostname": d.Hostname, "publicUrl": "https://" + d.Hostname, "developmentLoopback": true, "trustedProxies": []string{}})
	os.WriteFile(phpBootstrap, bootstrapPayload, 0600)
	keyFile := filepath.Join(directory, "public-key.txt")
	os.WriteFile(keyFile, []byte(key), 0600)
	if err = db.StageRotation(ctx, owner.ID, "", a.ID); err != nil {
		t.Fatal(err)
	}
	command = exec.Command("python3", "../../../../scripts/maintain-agent.py", "--config", phpPath, "--migration", migration, "--key-file", keyFile, "--runtime-stopped")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("PHP config maintenance: %v %s", err, output)
	}
	phpBytes, _ := os.ReadFile(phpPath)
	var phpSaved map[string]any
	json.Unmarshal(phpBytes, &phpSaved)
	next = phpSaved["credential"].(string)
	bootstrapBytes, _ := os.ReadFile(phpBootstrap)
	var bootstrapSaved map[string]any
	json.Unmarshal(bootstrapBytes, &bootstrapSaved)
	if bootstrapSaved["controlPlaneUrl"] != phpSaved["controlPlaneUrl"] || bootstrapSaved["publicUrl"] != "https://"+d.Hostname {
		t.Fatal("PHP bootstrap paths/binding lost")
	}
	if phpSaved["version"] != "v0.17.0-b17" {
		t.Fatal("PHP config fields lost")
	}
	if err = db.Revoke(ctx, a.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AuthenticateAgent(ctx, next); err == nil {
		t.Fatal("revoked agent accepted")
	}
	// Private file and public admin output remain secret-free except the private file itself.
	if mode, err := os.Stat(config); err != nil || mode.Mode().Perm() != 0600 {
		t.Fatal("private config permissions")
	}
}
