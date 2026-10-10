package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"shortlink.local/agent/internal/installer"
	"shortlink.local/agent/internal/runtime"
	"shortlink.local/artifacts"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "install" || os.Args[1] == "update" || os.Args[1] == "rollback" || os.Args[1] == "status") {
		if err := installer.Run(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(runtime.Version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "verify-release" {
		if err := verifyRelease(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) < 2 || os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: agent serve --config /private/agent.json")
		os.Exit(2)
	}
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	path := flags.String("config", "", "private configuration file (0600)")
	flags.Parse(os.Args[2:])
	cfg, err := runtime.LoadConfig(*path)
	if err != nil {
		slog.Error("agent configuration rejected", "error", err)
		os.Exit(1)
	}
	agent, err := runtime.New(cfg)
	if err != nil {
		slog.Error("agent initialization failed", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	defer agent.Close()
	eventsDone := make(chan struct{})
	go func() { defer close(eventsDone); agent.RunEvents(ctx) }()
	defer func() { stop(); <-eventsDone }()
	go agent.Run(ctx)
	go agent.RunGeo(ctx)
	go agent.RunFeeds(ctx)
	server := &http.Server{Addr: cfg.Listen, Handler: agent, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	slog.Info("agent listening", "listen", cfg.Listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("agent stopped", "error", err)
		os.Exit(1)
	}
}

func verifyRelease(args []string) error {
	f := flag.NewFlagSet("verify-release", flag.ContinueOnError)
	path := f.String("manifest", "", "signed manifest path")
	key := f.String("key", "", "pinned Ed25519 public key")
	version := f.String("version", "", "expected release version")
	name := f.String("artifact", "", "allowlisted artifact name")
	file := f.String("file", "", "downloaded file path")
	if err := f.Parse(args); err != nil {
		return err
	}
	public, err := base64.RawURLEncoding.DecodeString(*key)
	if err != nil || len(public) != ed25519.PublicKeySize || !artifacts.ValidVersion(*version) {
		return fmt.Errorf("pinned public key and expected version required")
	}
	raw, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	manifest, err := artifacts.Verify(raw, public)
	if err != nil {
		return err
	}
	if manifest.Version != *version {
		return fmt.Errorf("unexpected release version")
	}
	for _, meta := range manifest.Artifacts {
		if meta.Name == *name {
			if err = artifacts.CheckFile(*file, meta); err != nil {
				return err
			}
			fmt.Println("Verified", manifest.Version, meta.Name)
			return nil
		}
	}
	return fmt.Errorf("artifact is not listed")
}
