// agent-demo is an isolated signed fixture for local testing, never a production control plane.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"shortlink.local/policy"
	"syscall"
	"time"

	"shortlink.local/agent/internal/runtime"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8091", "loopback IP:port for the test agent")
	flag.Parse()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		log.Fatal(err)
	}
	credential := base64.RawURLEncoding.EncodeToString(secret)
	var snapshot runtime.Snapshot
	snapshot.Revision = 1
	snapshot.LeaseExpiresAt = time.Now().Add(24 * time.Hour).UTC()
	snapshot.Domain.ID = "demo-domain"
	snapshot.Domain.Hostname = "agent.example.test"
	snapshot.Domain.Mode = "local"
	snapshot.Domain.OpeningURL = "https://example.org/?source=shortlink-opening-demo"
	snapshot.Links = []runtime.Link{{ID: "hello-link", DomainID: "demo-domain", Slug: "hello", Destination: "https://example.org/?source=shortlink-demo", State: "published"}}
	snapshot.Rules = []policy.ScopedRule{}
	payload, _ := json.Marshal(snapshot)
	envelope, _ := json.Marshal(runtime.Envelope{SignedPayload: base64.RawURLEncoding.EncodeToString(payload), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, payload))})
	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+credential {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/v1/agents/config":
			w.Header().Set("Content-Type", "application/json")
			w.Write(envelope)
		case "/v1/agents/heartbeat":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer center.Close()
	data, err := os.MkdirTemp("", "shortlink-agent-demo-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(data)
	agent, err := runtime.New(runtime.Config{Listen: *listen, ControlURL: center.URL, DomainID: "demo-domain", Hostname: "agent.example.test", Credential: credential, VerificationKey: base64.RawURLEncoding.EncodeToString(public), DataDir: data, Development: true})
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err = agent.Sync(ctx); err != nil {
		log.Fatal(err)
	}
	go agent.Run(ctx)
	server := &http.Server{Addr: *listen, Handler: agent, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	fmt.Printf("LOCAL FIXTURE ONLY — no dashboard/database changes.\nAgent: http://%s\nTest: curl -I -H 'Host: agent.example.test' http://%s/hello\n", *listen, *listen)
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Print(err)
	}
}
