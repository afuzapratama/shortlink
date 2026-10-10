package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"shortlink.local/control/internal/testdb"
)

func TestBootstrapRaceAndPersistentSessions(t *testing.T) {
	s := testdb.New(t)
	ctx := context.Background()
	if s.Ready(ctx) == nil {
		t.Fatal("ready before owner")
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- s.Bootstrap(ctx, security.Token(), "owner@example.com", "test hash")
		}()
	}
	wg.Wait()
	close(results)
	won := 0
	for err := range results {
		if err == nil {
			won++
		} else if !errors.Is(err, store.ErrOwnerExists) {
			t.Fatal(err)
		}
	}
	if won != 1 {
		t.Fatalf("%d owners created", won)
	}
	if err := s.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("migration rerun", err)
	}
	owner, err := s.OwnerByEmail(ctx, "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	raw := security.Token()
	hash := security.Digest(raw)
	if err = s.CreateSession(ctx, hash, owner.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, raw); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("raw bearer stored as database key")
	}
	if _, err = s.DB.Exec(ctx, "UPDATE sessions SET expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, hash); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("expired session accepted")
	}
	if err = s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.DB.QueryRow(ctx, "SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 0 {
		t.Fatal("expired session not cleaned", err)
	}
	if _, err = s.DB.Exec(ctx, "UPDATE schema_migrations SET checksum='changed'"); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx); err == nil {
		t.Fatal("changed migration silently accepted")
	}
}
func TestLoginBucketsAtomic(t *testing.T) {
	s := testdb.New(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan bool, 20)
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); ok, err := s.AllowLogin(ctx, "test-key", 5); results <- ok; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	allowed := 0
	for ok := range results {
		if ok {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("expected 5 allowed requests, got %d", allowed)
	}
	if _, err := s.DB.Exec(ctx, "UPDATE auth_buckets SET window_started=now()-interval '16 minutes'"); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.AllowLogin(ctx, "test-key", 5); err != nil || !ok {
		t.Fatal("expired bucket not reset", err)
	}
}
