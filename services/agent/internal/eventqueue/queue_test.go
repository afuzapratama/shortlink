package eventqueue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"shortlink.local/policy"
	"strings"
	"syscall"
	"testing"
	"time"
)

func event(id string) policy.TrafficEvent {
	return policy.TrafficEvent{Decision: policy.Decision{EventID: id, Action: "redirect"}, OccurredAt: time.Now().UTC(), DomainID: "domain", LinkID: "link"}
}
func waitDepth(t *testing.T, q *Queue, want int64) Stats {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		stats, e := q.Stats(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		if stats.Depth == want {
			return stats
		}
		if time.Now().After(deadline) {
			t.Fatalf("depth %+v want %d", stats, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestRestartRetryAckAndBound(t *testing.T) {
	dir := t.TempDir()
	q, e := Open(dir, Options{MaxBytes: 1000})
	if e != nil {
		t.Fatal(e)
	}
	q.Enqueue(event("one"))
	q.Enqueue(event("two"))
	waitDepth(t, q, 2)
	first, e := q.Batch(context.Background(), 100)
	if e != nil || len(first) != 2 {
		t.Fatal(first, e)
	}
	if e = q.Close(); e != nil {
		t.Fatal(e)
	}
	q, e = Open(dir, Options{MaxBytes: 1000})
	if e != nil {
		t.Fatal(e)
	}
	defer q.Close()
	retry, e := q.Batch(context.Background(), 100)
	if e != nil || retry[0].EventID != first[0].EventID {
		t.Fatal("unstable IDs", e)
	}
	if e = q.Ack(context.Background(), retry); e != nil {
		t.Fatal(e)
	}
	if stats := waitDepth(t, q, 0); stats.Bytes != 0 {
		t.Fatal(stats)
	}
	for i := 0; i < 10; i++ {
		q.Enqueue(event(fmt.Sprint(i)))
	}
	time.Sleep(150 * time.Millisecond)
	stats, _ := q.Stats(context.Background())
	if stats.Bytes > 1000 || stats.Dropped == 0 {
		t.Fatal("unbounded", stats)
	}
	dropped := stats.Dropped
	q.Close()
	q, e = Open(dir, Options{MaxBytes: 1000})
	if e != nil {
		t.Fatal(e)
	}
	defer q.Close()
	stats, _ = q.Stats(context.Background())
	if stats.Dropped < dropped {
		t.Fatal("loss counter reset", stats)
	}
}
func TestExpiryAndDiskWriteFailure(t *testing.T) {
	q, e := Open(t.TempDir(), Options{MaxAge: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	defer q.Close()
	old := event("old")
	old.OccurredAt = time.Now().Add(-2 * time.Second)
	q.Enqueue(old)
	time.Sleep(150 * time.Millisecond)
	// Expiry maintenance happens even with no further clicks.
	deadline := time.Now().Add(3 * time.Second)
	for {
		s, _ := q.Stats(context.Background())
		if s.Dropped >= 1 && s.Depth == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expiry missing", s)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, e = q.db.Exec("PRAGMA query_only=ON"); e != nil {
		t.Fatal(e)
	}
	q.Enqueue(event("disk-full-equivalent"))
	time.Sleep(150 * time.Millisecond)
	s, _ := q.Stats(context.Background())
	if s.WriteErrors == 0 || s.Dropped < 2 {
		t.Fatal("disk failure hidden", s)
	}
	if _, e = q.db.Exec("PRAGMA query_only=OFF"); e != nil {
		t.Fatal(e)
	}
	q.Enqueue(event("recovered"))
	waitDepth(t, q, 1)
}
func TestBoundedBuffer(t *testing.T) {
	q, e := Open(t.TempDir(), Options{Buffer: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer q.Close()
	for i := 0; i < 10000; i++ {
		q.Enqueue(event(fmt.Sprint(i)))
	}
	s, _ := q.Stats(context.Background())
	if s.Dropped == 0 {
		t.Fatal("buffer did not bound writes")
	}
}

// Exercise SQLite's real SQLITE_FULL limit without filling a shared host filesystem.
func TestSQLiteCapacityFailureAndRecovery(t *testing.T) {
	q, err := Open(t.TempDir(), Options{MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	var pages int
	if err = q.db.QueryRow("PRAGMA page_count").Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if _, err = q.db.Exec(fmt.Sprintf("PRAGMA max_page_count=%d", pages)); err != nil {
		t.Fatal(err)
	}
	huge := event("capacity-full")
	destination := strings.Repeat("x", 64<<10)
	huge.Destination = &destination
	q.Enqueue(huge)
	deadline := time.Now().Add(3 * time.Second)
	for {
		stats, err := q.Stats(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if stats.WriteErrors > 0 && stats.Dropped > 0 && stats.Buffered == 0 {
			if stats.Depth != 0 {
				t.Fatal("failed transaction left queued event")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("SQLITE_FULL not surfaced")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = q.db.Exec("PRAGMA max_page_count=4096"); err != nil {
		t.Fatal(err)
	}
	q.Enqueue(event("recovered-after-full"))
	waitDepth(t, q, 1)
}

func TestQueueCrashHelper(t *testing.T) {
	if os.Getenv("SHORTLINK_QUEUE_CRASH_HELPER") != "1" {
		return
	}
	q, err := Open(os.Getenv("SHORTLINK_QUEUE_CRASH_DIR"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		q.Enqueue(event(fmt.Sprintf("durable-crash-%d", i)))
	}
	waitDepth(t, q, 10)
	if err = os.WriteFile(filepath.Join(os.Getenv("SHORTLINK_QUEUE_CRASH_DIR"), "durable-ready"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	select {} // Parent sends SIGKILL; no Close or graceful shutdown runs.
}
func TestSIGKILLPreservesDurableQueueIDs(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestQueueCrashHelper$", "-test.timeout=20s")
	cmd.Env = append(os.Environ(), "SHORTLINK_QUEUE_CRASH_HELPER=1", "SHORTLINK_QUEUE_CRASH_DIR="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "durable-ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("crash fixture did not become durable")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("helper exited gracefully")
	}
	q, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	batch, err := q.Batch(context.Background(), 100)
	if err != nil || len(batch) != 10 {
		t.Fatalf("durable events lost: count=%d err=%v", len(batch), err)
	}
	seen := map[string]bool{}
	for _, e := range batch {
		seen[e.EventID] = true
	}
	for i := 0; i < 10; i++ {
		if !seen[fmt.Sprintf("durable-crash-%d", i)] {
			t.Fatal("event ID changed across SIGKILL")
		}
	}
	if err = q.Ack(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	waitDepth(t, q, 0)
}

func TestActualENOSPCRecovery(t *testing.T) {
	dir := os.Getenv("SHORTLINK_ENOSPC_DIR")
	if dir == "" {
		t.Skip("requires isolated Docker tmpfs")
	}
	if dir != "/capacity" {
		t.Fatal("ENOSPC fixture restricted to /capacity inside disposable container")
	}
	q, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	filler := filepath.Join(dir, "disposable-filler")
	f, err := os.Create(filler)
	if err != nil {
		t.Fatal(err)
	}
	block := make([]byte, 32<<10)
	var written int
	for written < 4<<20 {
		n, e := f.Write(block)
		written += n
		if e != nil {
			if !errors.Is(e, syscall.ENOSPC) {
				t.Fatal(e)
			}
			break
		}
	}
	f.Close()
	if written >= 4<<20 {
		t.Fatal("fixture filesystem was not bounded")
	}
	full := event("enospc-dropped")
	destination := strings.Repeat("x", 64<<10)
	full.Destination = &destination
	q.Enqueue(full)
	deadline := time.Now().Add(3 * time.Second)
	for {
		stats, e := q.Stats(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		if stats.WriteErrors > 0 && stats.Dropped > 0 && stats.Buffered == 0 {
			if stats.Depth != 0 {
				t.Fatal("failed write left a durable event")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ENOSPC did not surface as write failure")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = os.Remove(filler); err != nil {
		t.Fatal(err)
	}
	q.Enqueue(event("recovered-after-enospc"))
	stats := waitDepth(t, q, 1)
	if stats.Dropped == 0 || stats.WriteErrors == 0 {
		t.Fatal("loss counters reset after recovery")
	}
	batch, err := q.Batch(context.Background(), 100)
	if err != nil || len(batch) != 1 || batch[0].EventID != "recovered-after-enospc" {
		t.Fatal("ENOSPC recovery incorrect", err)
	}
}
