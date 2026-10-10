// Package eventqueue isolates SQLite writes from redirect handlers. Enqueue is bounded and nonblocking.
package eventqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"shortlink.local/policy"
	"sync"
	"sync/atomic"
	"time"
)

type Options struct {
	MaxBytes int64
	MaxAge   time.Duration
	Buffer   int
}
type Stats struct {
	Depth       int64      `json:"queueDepth"`
	Bytes       int64      `json:"queueBytes"`
	Dropped     int64      `json:"droppedEvents"`
	Buffered    int        `json:"bufferedEvents"`
	Oldest      *time.Time `json:"oldestEventAt"`
	WriteErrors int64      `json:"writeErrors"`
}
type Queue struct {
	db          *sql.DB
	input       chan policy.TrafficEvent
	stop        chan struct{}
	done        chan struct{}
	gate        sync.RWMutex
	closed      bool
	once        sync.Once
	dropped     atomic.Int64
	writeErrors atomic.Int64
	pending     atomic.Int64
	opts        Options
}

func Open(dir string, opts Options) (*Queue, error) {
	if opts.MaxBytes == 0 {
		opts.MaxBytes = 256 << 20
	}
	if opts.MaxAge == 0 {
		opts.MaxAge = 47 * time.Hour
	}
	if opts.Buffer == 0 {
		opts.Buffer = 2048
	}
	if opts.MaxBytes < 1 || opts.MaxBytes > 1<<30 || opts.MaxAge < time.Second || opts.Buffer < 1 {
		return nil, errors.New("invalid queue limits")
	}
	path := filepath.Join(dir, "events.sqlite")
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	if e = os.Chmod(path, 0600); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	if _, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=1000; PRAGMA wal_autocheckpoint=256; PRAGMA journal_size_limit=4194304; CREATE TABLE IF NOT EXISTS queue(id TEXT PRIMARY KEY, occurred INTEGER NOT NULL, payload BLOB NOT NULL, size INTEGER NOT NULL); CREATE INDEX IF NOT EXISTS queue_age ON queue(occurred,id); CREATE TABLE IF NOT EXISTS counters(id INTEGER PRIMARY KEY CHECK(id=1), dropped INTEGER NOT NULL); INSERT OR IGNORE INTO counters VALUES(1,0); CREATE TABLE IF NOT EXISTS queue_health(id INTEGER PRIMARY KEY CHECK(id=1),errors INTEGER NOT NULL); INSERT OR IGNORE INTO queue_health VALUES(1,0); CREATE TABLE IF NOT EXISTS queue_meta(id INTEGER PRIMARY KEY CHECK(id=1), bytes INTEGER NOT NULL); INSERT OR IGNORE INTO queue_meta SELECT 1,coalesce(sum(size),0) FROM queue;`); e != nil {
		db.Close()
		return nil, e
	}
	if _, e = db.Exec("PRAGMA max_page_count=" + fmt.Sprint((opts.MaxBytes*2+(16<<20))/4096)); e != nil {
		db.Close()
		return nil, e
	}
	q := &Queue{db: db, input: make(chan policy.TrafficEvent, opts.Buffer), stop: make(chan struct{}), done: make(chan struct{}), opts: opts}
	var dropped int64
	if e = db.QueryRow("SELECT dropped FROM counters WHERE id=1").Scan(&dropped); e != nil {
		db.Close()
		return nil, e
	}
	q.dropped.Store(dropped)
	var writeErrors int64
	if e = db.QueryRow("SELECT errors FROM queue_health WHERE id=1").Scan(&writeErrors); e != nil {
		db.Close()
		return nil, e
	}
	q.writeErrors.Store(writeErrors)
	go q.writer()
	return q, nil
}
func (q *Queue) Enqueue(e policy.TrafficEvent) bool {
	q.gate.RLock()
	defer q.gate.RUnlock()
	if q.closed {
		return false
	}
	q.pending.Add(1)
	select {
	case q.input <- e:
		return true
	default:
		q.pending.Add(-1)
		q.dropped.Add(1)
		return false
	}
}
func (q *Queue) writer() {
	defer close(q.done)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]policy.TrafficEvent, 0, 100)
	lastMaintenance := time.Now()
	flush := func() {
		if len(batch) > 0 {
			q.persist(batch)
			batch = batch[:0]
		} else {
			if time.Since(lastMaintenance) >= time.Second {
				q.persist(nil)
				lastMaintenance = time.Now()
			}
		}
	}
	for {
		select {
		case e := <-q.input:
			batch = append(batch, e)
			if len(batch) >= 100 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-q.stop:
			for {
				select {
				case e := <-q.input:
					batch = append(batch, e)
					if len(batch) >= 100 {
						flush()
					}
				default:
					flush()
					q.persist(nil)
					return
				}
			}
		}
	}
}
func (q *Queue) persist(events []policy.TrafficEvent) {
	defer q.pending.Add(-int64(len(events)))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		q.failed(len(events))
		return
	}
	defer tx.Rollback()
	cutoff := time.Now().Add(-q.opts.MaxAge).UnixMilli()
	var expired, expiredBytes int64
	err = tx.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(size),0) FROM queue WHERE occurred<?", cutoff).Scan(&expired, &expiredBytes)
	if err == nil {
		_, err = tx.ExecContext(ctx, "DELETE FROM queue WHERE occurred<?", cutoff)
	}
	var size int64
	if err == nil {
		err = tx.QueryRowContext(ctx, "SELECT bytes FROM queue_meta WHERE id=1").Scan(&size)
	}
	size -= expiredBytes
	lost := expired
	for _, e := range events {
		if err != nil {
			break
		}
		raw, marshalErr := json.Marshal(e)
		if marshalErr != nil {
			lost++
			continue
		}
		if size+int64(len(raw)) > q.opts.MaxBytes {
			lost++
			continue
		}
		var result sql.Result
		result, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO queue VALUES(?,?,?,?)", e.EventID, e.OccurredAt.UnixMilli(), raw, len(raw))
		if err == nil {
			n, _ := result.RowsAffected()
			if n > 0 {
				size += int64(len(raw))
			}
		}
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE queue_meta SET bytes=? WHERE id=1", size)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE counters SET dropped=? WHERE id=1", q.dropped.Load()+lost)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE queue_health SET errors=? WHERE id=1", q.writeErrors.Load())
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		q.failed(len(events))
		return
	}
	q.dropped.Add(lost)
}
func (q *Queue) failed(n int) { q.dropped.Add(int64(n)); q.writeErrors.Add(1) }
func (q *Queue) Batch(ctx context.Context, limit int) ([]policy.TrafficEvent, error) {
	rows, err := q.db.QueryContext(ctx, "SELECT payload FROM queue ORDER BY occurred,id LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []policy.TrafficEvent{}
	for rows.Next() {
		var raw []byte
		var e policy.TrafficEvent
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
func (q *Queue) Ack(ctx context.Context, events []policy.TrafficEvent) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	removed := int64(0)
	for _, e := range events {
		var size int64
		err = tx.QueryRowContext(ctx, "DELETE FROM queue WHERE id=? RETURNING size", e.EventID).Scan(&size)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			removed += size
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE queue_meta SET bytes=bytes-? WHERE id=1", removed); err != nil {
		return err
	}
	return tx.Commit()
}
func (q *Queue) Stats(ctx context.Context) (Stats, error) {
	s := Stats{Dropped: q.dropped.Load(), Buffered: int(q.pending.Load()), WriteErrors: q.writeErrors.Load()}
	var oldest sql.NullInt64
	err := q.db.QueryRowContext(ctx, "SELECT count(*),(SELECT bytes FROM queue_meta WHERE id=1),min(occurred) FROM queue").Scan(&s.Depth, &s.Bytes, &oldest)
	if oldest.Valid {
		t := time.UnixMilli(oldest.Int64).UTC()
		s.Oldest = &t
	}
	return s, err
}
func (q *Queue) Close() error {
	q.once.Do(func() { q.gate.Lock(); q.closed = true; close(q.stop); q.gate.Unlock(); <-q.done })
	return q.db.Close()
}
