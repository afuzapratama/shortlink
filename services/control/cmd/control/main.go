package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"shortlink.local/artifacts"
	"shortlink.local/geoip"
	"shortlink.local/risk"
	"syscall"
	"time"

	"shortlink.local/control/internal/config"
	"shortlink.local/control/internal/httpapi"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if len(os.Args) > 2 || (command != "serve" && command != "migrate" && command != "bootstrap-owner" && command != "publish-geoip" && command != "watch-geoip" && command != "publish-php" && command != "publish-agent" && command != "agent-key" && command != "publish-feeds" && command != "refresh-feeds") {
		return errors.New("usage: control [serve|migrate|bootstrap-owner|publish-geoip|watch-geoip|publish-agent|publish-php|agent-key|publish-feeds|refresh-feeds]")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	db, err := store.Open(startup, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if command == "migrate" {
		if err = db.Migrate(startup); err != nil {
			return err
		}
		logger.Info("migrations applied")
		return nil
	}
	if command == "bootstrap-owner" {
		email, ok := httpapi.NormalizeEmail(os.Getenv("OWNER_EMAIL"))
		password := os.Getenv("OWNER_PASSWORD")
		_ = os.Unsetenv("OWNER_PASSWORD")
		if !ok || !security.ValidPassword(password) {
			return errors.New("OWNER_EMAIL and OWNER_PASSWORD (12–256 bytes) are required")
		}
		if err = db.Bootstrap(startup, security.Token(), email, security.HashPassword(password)); err != nil {
			return err
		}
		logger.Info("owner initialized")
		return nil
	}
	if err = db.Ready(startup); err != nil {
		return errors.New("database is not initialized; run migrate and bootstrap-owner first")
	}
	if command == "agent-key" {
		seed, err := db.Seed(ctx)
		if err != nil {
			return err
		}
		fmt.Println(base64.RawURLEncoding.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)))
		return nil
	}
	if command == "publish-php" {
		seed, err := db.Seed(ctx)
		if err != nil {
			return err
		}
		release, err := artifacts.PublishPHP(cfg.PHPAssetRoot, cfg.PHPReleaseInput, seed, time.Now())
		if err == nil {
			logger.Info("PHP release published", "version", release.Version)
		}
		return err
	}
	if command == "publish-agent" {
		seed, err := db.Seed(ctx)
		if err != nil {
			return err
		}
		release, err := artifacts.Publish(cfg.AgentAssetRoot, cfg.AgentReleaseInput, seed, time.Now())
		if err == nil {
			logger.Info("agent release published", "version", release.Version)
		}
		return err
	}
	publishFeeds := func(refresh bool) error {
		if cfg.RiskFeedRoot == "" {
			return errors.New("RISK_FEED_ROOT required")
		}
		if refresh && cfg.RiskTorEnabled {
			task, done := context.WithTimeout(ctx, 20*time.Second)
			e := risk.RefreshTor(task, cfg.RiskFeedRoot, time.Now())
			done()
			if e != nil {
				logger.Warn("Tor feed refresh failed") /* Keep last healthy source; freshness is checked locally. */
			}
		}
		seed, e := db.Seed(ctx)
		if e != nil {
			return e
		}
		_, _, e = risk.Publish(cfg.RiskFeedRoot, seed, time.Now())
		return e
	}
	if command == "publish-feeds" || command == "refresh-feeds" {
		return publishFeeds(command == "refresh-feeds")
	}
	publishGeo := func() error {
		if cfg.GeoIPRoot == "" {
			return errors.New("GEOIP_ROOT is required")
		}
		seed, err := db.Seed(ctx)
		if err != nil {
			return err
		}
		_, err = geoip.Publish(cfg.GeoIPRoot, seed, time.Now())
		cleanupErr := geoip.Cleanup(cfg.GeoIPRoot, ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey), time.Now())
		if err != nil {
			return err
		}
		return cleanupErr
	}
	if command == "publish-geoip" {
		return publishGeo()
	}
	if command == "watch-geoip" {
		if cfg.GeoIPRoot == "" {
			return errors.New("GEOIP_ROOT is required")
		}
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			if err := publishGeo(); err != nil {
				logger.Warn("geo publisher", "error", err.Error())
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}
	if cfg.GeoIPRoot != "" && cfg.GeoPublisher {
		if err := publishGeo(); err != nil {
			logger.Warn("geo publisher", "error", err.Error())
		}
	}
	if cfg.RiskFeedRoot != "" {
		if e := publishFeeds(false); e != nil {
			logger.Warn("risk feed publisher failed")
		}
	}
	workerCtx, workerStop := context.WithCancel(ctx)
	defer workerStop()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-tick.C:
				task, done := context.WithTimeout(workerCtx, 10*time.Second)
				_, e := db.AggregateEvents(task, 100)
				done()
				if e != nil && workerCtx.Err() == nil {
					logger.Error("event worker failed")
				}
			}
		}
	}()
	defer func() { workerStop(); <-workerDone }()
	handler := httpapi.New(cfg, db, logger)
	defer handler.Close()
	if cfg.GeoIPRoot != "" {
		warmCtx, warmCancel := context.WithTimeout(ctx, 10*time.Second)
		warmErr := handler.WarmDecisions(warmCtx)
		warmCancel()
		if warmErr != nil {
			logger.Warn("central decision assets unavailable; API decisions will fail closed")
		}
	}
	server := &http.Server{Addr: cfg.Listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	failure := make(chan error, 1)
	go func() { logger.Info("control listening", "address", cfg.Listen); failure <- server.ListenAndServe() }()
	maintenance := time.NewTicker(5 * time.Minute)
	defer maintenance.Stop()
	for {
		select {
		case err := <-failure:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ctx.Done():
			shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
			defer done()
			return server.Shutdown(shutdown)
		case <-maintenance.C:
			if cfg.GeoIPRoot != "" && cfg.GeoPublisher {
				if err := publishGeo(); err != nil {
					logger.Warn("geo publisher", "error", err.Error())
				}
			}
			if cfg.RiskFeedRoot != "" {
				refresh := false
				if cfg.RiskTorEnabled {
					info, e := os.Stat(filepath.Join(cfg.RiskFeedRoot, "staging", "tor.txt"))
					refresh = e != nil || time.Since(info.ModTime()) > 6*time.Hour
				}
				if e := publishFeeds(refresh); e != nil {
					logger.Warn("risk feed publisher failed")
				}
			}
			eventMaintenance, eventDone := context.WithTimeout(ctx, 30*time.Second)
			if err := db.RetainEvents(eventMaintenance, cfg.EventRawDays, cfg.EventAggregateDays); err != nil {
				logger.Error("event retention failed")
			}
			eventDone()
			cleanup, done := context.WithTimeout(ctx, 5*time.Second)
			err := db.Cleanup(cleanup)
			done()
			if err != nil {
				logger.Error("auth cleanup failed")
			}
		}
	}
}
