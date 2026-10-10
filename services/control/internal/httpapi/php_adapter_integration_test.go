package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	"shortlink.local/control/internal/security"
)

// Actual PHP cURL against the actual Go server and disposable PostgreSQL/MMDB.
// The HTTP/browser wizard failure cases live in agent-php/tests/integration.py.
func TestPHPAdapterRealCentral(t *testing.T) {
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("PHP unavailable; run scripts/test-php-agent.sh on a PHP-equipped host")
	}
	s, db, d, a, _, _ := apiFixture(t)
	ctx := context.Background()
	if err := db.Revoke(ctx, a.ID, "test"); err != nil {
		t.Fatal(err)
	}
	tok, err := db.IssueToken(ctx, d.ID, "php_zip", "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(s)
	defer server.Close()
	s.cfg.PublicURL = server.URL
	private := t.TempDir()
	harness, err := filepath.Abs("../../../agent-php/tests/central.php")
	if err != nil {
		t.Fatal(err)
	}
	call := func(action string) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"action": action, "origin": server.URL, "private": private, "hostname": d.Hostname, "token": tok.Token, "ua": apiIphone})
		cmd := exec.Command("php", harness)
		cmd.Stdin = bytes.NewReader(raw)
		output, err := cmd.Output()
		if err != nil {
			t.Fatal("PHP adapter operation failed", action, err)
		}
		var result map[string]any
		if err = json.Unmarshal(output, &result); err != nil {
			t.Fatal("PHP adapter returned invalid JSON")
		}
		return result
	}
	installed := call("activate")
	if installed["installed"] != true || installed["domainId"] != d.ID {
		t.Fatal("PHP enrollment binding failed")
	}
	var before int
	if err = db.DB.QueryRow(ctx, "SELECT count(*) FROM traffic_events WHERE domain_id=$1", d.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	probe := call("probe")
	if probe["ready"] != true || probe["domainId"] != d.ID {
		t.Fatal("PHP probe failed")
	}
	var after int
	if err = db.DB.QueryRow(ctx, "SELECT count(*) FROM traffic_events WHERE domain_id=$1", d.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatal("PHP probe generated clicks")
	}
	decision := call("decision")
	if decision["status"] != float64(302) || decision["location"] != "https://example.org/first" {
		t.Fatal("PHP redirect did not use published Go policy")
	}
	var source, country, device, osName string
	if err = db.DB.QueryRow(ctx, "SELECT payload->>'source',payload->>'country',payload->>'device',payload->>'os' FROM traffic_events WHERE domain_id=$1", d.ID).Scan(&source, &country, &device, &osName); err != nil {
		t.Fatal(err)
	}
	if source != "php" || country != "ID" || device != "mobile" || osName != "ios" {
		t.Fatal("PHP event did not use central classification")
	}
	// Re-enrollment with a new proof is not allowed; the PHP installer uses its persisted proof.
	if _, _, err = db.Enroll(ctx, tok.Token, tok.InstallationID, security.Token(), d.Hostname, "php_zip", "other"); err == nil {
		t.Fatal("Unrelated recovery proof accepted")
	}
}
