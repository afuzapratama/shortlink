package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shortlink.local/control/internal/testdb"
)

func TestCLIAndHTTPStartup(t *testing.T) {
	db := testdb.New(t)
	binary := filepath.Join(t.TempDir(), "control")
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	origin := "http://" + address
	env := append(os.Environ(), "DATABASE_URL="+db.DB.Config().ConnString(), "CONTROL_MODE=development", "CONTROL_LISTEN="+address, "CONTROL_ORIGIN="+origin, "OWNER_EMAIL=cli-owner@example.com", "OWNER_PASSWORD=cli smoke test password")
	invoke := func(command string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, command)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if bytes.Contains(out, []byte("cli smoke test password")) {
			t.Fatal("password leaked by CLI")
		}
		return err
	}
	if err = invoke("migrate"); err != nil {
		t.Fatal("migrate", err)
	}
	if err = invoke("bootstrap-owner"); err != nil {
		t.Fatal("bootstrap", err)
	}
	if err = invoke("bootstrap-owner"); err == nil {
		t.Fatal("CLI overwrote existing owner")
	}
	cmd := exec.Command(binary, "serve")
	cmd.Env = env
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	client := &http.Client{Timeout: time.Second}
	ready := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res, err := client.Get(origin + "/health/ready")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("CLI server failed to become ready")
	}
	req, _ := http.NewRequest("POST", origin+"/v1/auth/login", strings.NewReader(`{"email":"cli-owner@example.com","password":"cli smoke test password"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("HTTP login: %d", res.StatusCode)
	}
	cookies := res.Cookies()
	if len(cookies) != 1 || cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("development cookie flags invalid")
	}
	var payload map[string]any
	if err = json.NewDecoder(res.Body).Decode(&payload); err != nil || payload["status"] != "authenticated" {
		t.Fatal("HTTP login response", err)
	}
	req, _ = http.NewRequest("GET", origin+"/v1/auth/session", nil)
	req.AddCookie(cookies[0])
	res2, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if res2.StatusCode != 200 {
		t.Fatal("HTTP session rejected")
	}
	if err = cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
		waited = true
		if err != nil {
			t.Fatal("graceful shutdown", err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("shutdown timed out")
	}
}
