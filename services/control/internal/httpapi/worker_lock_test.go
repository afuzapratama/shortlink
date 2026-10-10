package httpapi

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"shortlink.local/control/internal/store"
	"testing"
	"time"
)

func TestWorkerWaitsForBusyIngestLock(t *testing.T) {
	_, db := setup(t)
	ctx := context.Background()
	tx, err := db.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(718204012)"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := db.AggregateEvents(ctx, 1000); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("worker skipped busy lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not resume")
	}
	tx, err = db.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(718204012)"); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	if _, err = db.AggregateEvents(ctx, 1000); err == nil {
		t.Fatal("held lock should time out")
	}
	if time.Since(began) > time.Second {
		t.Fatal("lock wait unbounded")
	}
}

func TestWorkerJITOverrideIsTransactionLocal(t *testing.T) {
	s, db, d, _, _, credential := apiFixture(t)
	ctx := context.Background()
	status(t, agentCall(s, "/v1/decisions", credential, decisionBody(d.ID, apiIphone)), 200)
	cfg := db.DB.Config().Copy()
	cfg.MaxConns = 1
	cfg.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, "SET jit=on"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `CREATE FUNCTION require_worker_nojit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF current_setting('jit')<>'off' THEN RAISE EXCEPTION 'worker JIT must be off'; END IF; RETURN NEW; END $$; CREATE TRIGGER require_worker_nojit BEFORE UPDATE ON traffic_events FOR EACH ROW EXECUTE FUNCTION require_worker_nojit();`); err != nil {
		t.Fatal(err)
	}
	isolated := &store.Store{DB: pool}
	if n, err := isolated.AggregateEvents(ctx, 100); err != nil || n != 1 {
		t.Fatalf("worker failed with JIT override: n=%d err=%v", n, err)
	}
	var setting string
	if err = pool.QueryRow(ctx, "SHOW jit").Scan(&setting); err != nil || setting != "on" {
		t.Fatal("worker changed pooled session JIT setting", err)
	}
	var pending int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM traffic_events WHERE NOT processed").Scan(&pending); err != nil || pending != 0 {
		t.Fatal("worker did not process fixture", err)
	}
}
