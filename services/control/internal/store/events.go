package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"shortlink.local/policy"
	"time"
)

var ErrEventConflict = errors.New("event ID reused with different payload")

const eventsLock = int64(718204012)

func eventPartition(t time.Time) string { return "traffic_events_" + t.UTC().Format("20060102") }
func ensureEventPartition(ctx context.Context, tx pgx.Tx, t time.Time) error {
	day := t.UTC().Truncate(24 * time.Hour)
	_, err := tx.Exec(ctx, fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s PARTITION OF traffic_events FOR VALUES FROM ('%s') TO ('%s')", pgx.Identifier{eventPartition(day)}.Sanitize(), day.Format(time.RFC3339), day.Add(24*time.Hour).Format(time.RFC3339)))
	return err
}
func (s *Store) IngestEvents(ctx context.Context, a Agent, events []policy.TrafficEvent) (int, int, error) {
	if a.Method != "cli" || len(events) < 1 || len(events) > 500 {
		return 0, 0, ErrBinding
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL synchronous_commit=on"); err != nil {
		return 0, 0, err
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", eventsLock); err != nil {
		return 0, 0, err
	}
	var active bool
	if err = tx.QueryRow(ctx, "SELECT revoked_at IS NULL FROM agents WHERE id=$1 AND domain_id=$2 FOR SHARE", a.ID, a.DomainID).Scan(&active); err != nil {
		return 0, 0, err
	}
	if !active {
		return 0, 0, ErrUsed
	}
	var desired int64
	if err = tx.QueryRow(ctx, "SELECT desired_revision FROM domains WHERE id=$1", a.DomainID).Scan(&desired); err != nil {
		return 0, 0, err
	}
	accepted, duplicates := 0, 0
	days := map[string]bool{}
	for _, e := range events {
		if e.ConfigRevision < 1 || e.ConfigRevision > desired || e.DomainID != a.DomainID || e.Source != "go" || e.OccurredAt.Before(time.Now().Add(-48*time.Hour)) || e.OccurredAt.After(time.Now().Add(5*time.Minute)) {
			return 0, 0, ErrBinding
		}
		var bound bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM links WHERE id=$1 AND domain_id=$2)", e.LinkID, a.DomainID).Scan(&bound); err != nil {
			return 0, 0, err
		}
		if !bound {
			return 0, 0, ErrBinding
		}
		raw, er := json.Marshal(e)
		if er != nil {
			return 0, 0, er
		}
		hash := sha256.Sum256(raw)
		digest := hex.EncodeToString(hash[:])
		tag, er := tx.Exec(ctx, "INSERT INTO event_keys(agent_id,event_id,payload_hash) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", a.ID, e.EventID, digest)
		if er != nil {
			return 0, 0, er
		}
		if tag.RowsAffected() == 0 {
			var old string
			if er = tx.QueryRow(ctx, "SELECT payload_hash FROM event_keys WHERE agent_id=$1 AND event_id=$2", a.ID, e.EventID).Scan(&old); er != nil {
				return 0, 0, er
			}
			if old != digest {
				return 0, 0, ErrEventConflict
			}
			duplicates++
			continue
		}
		day := eventPartition(e.OccurredAt)
		if !days[day] {
			if er = ensureEventPartition(ctx, tx, e.OccurredAt); er != nil {
				return 0, 0, er
			}
			days[day] = true
		}
		if _, er = tx.Exec(ctx, "INSERT INTO traffic_events(occurred_at,agent_id,domain_id,link_id,event_id,payload) VALUES($1,$2,$3,$4,$5,$6)", e.OccurredAt, a.ID, a.DomainID, e.LinkID, e.EventID, raw); er != nil {
			return 0, 0, er
		}
		accepted++
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return accepted, duplicates, nil
}

// AggregateEvents updates rollups, link counters and processed markers in one transaction.
func (s *Store) AggregateEvents(ctx context.Context, limit int) (int, error) {
	defer Measure(ctx, "worker_total")()
	if limit < 1 || limit > 5000 {
		return 0, errors.New("invalid worker batch")
	}
	poolDone := Measure(ctx, "worker_pool_begin")
	tx, err := s.DB.Begin(ctx)
	poolDone()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	// These small, frequent rollups favor predictable latency over JIT compilation.
	if _, err = tx.Exec(ctx, "SET LOCAL jit=off"); err != nil {
		return 0, err
	}

	// A try-lock can starve aggregation under continuous ingest. Bound the wait
	// even for callers without a deadline; rollups and processed markers remain atomic.
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout='250ms'"); err != nil {
		return 0, err
	}
	waitDone := Measure(ctx, "worker_lock_wait")
	_, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", eventsLock)
	waitDone()
	if err != nil {
		return 0, err
	}
	defer Measure(ctx, "worker_lock_hold")()
	rows, err := tx.Query(ctx, "SELECT id FROM traffic_events WHERE NOT processed ORDER BY id LIMIT $1 FOR UPDATE", limit)
	if err != nil {
		return 0, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	for _, grain := range []string{"minute", "hour"} {
		_, err = tx.Exec(ctx, `INSERT INTO traffic_aggregates(grain,bucket,domain_id,link_id,country,device,os,browser,action,reason,events,bot_events) SELECT $2,date_trunc($2,occurred_at,'UTC'),domain_id,link_id,payload->>'country',payload->>'device',payload->>'os',payload->>'browser',payload->>'action',payload->>'reasonCode',count(*),count(*) FILTER (WHERE (payload->'risk'->'signals') ? 'known_bot') FROM traffic_events WHERE id=ANY($1) GROUP BY 2,3,4,5,6,7,8,9,10 ON CONFLICT(grain,bucket,domain_id,link_id,country,device,os,browser,action,reason) DO UPDATE SET events=traffic_aggregates.events+excluded.events,bot_events=traffic_aggregates.bot_events+excluded.bot_events`, ids, grain)
		if err != nil {
			return 0, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO risk_observations SELECT date_trunc('hour',occurred_at,'UTC'),domain_id,link_id,payload->'risk'->>'mode',signal,payload->'risk'->>'candidateAction',payload->'risk'->>'candidateReasonCode',coalesce(payload->'risk'->>'candidateRuleId',''),count(*) FROM traffic_events CROSS JOIN LATERAL jsonb_array_elements_text(payload->'risk'->'signals') signal WHERE id=ANY($1) GROUP BY 1,2,3,4,5,6,7,8 ON CONFLICT(bucket,domain_id,link_id,mode,signal,candidate_action,candidate_reason,candidate_rule_id) DO UPDATE SET events=risk_observations.events+excluded.events`, ids); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `UPDATE links l SET clicks=l.clicks+g.n FROM (SELECT link_id,count(*) n FROM traffic_events WHERE id=ANY($1) AND payload->>'action' IN ('redirect','fallback') GROUP BY link_id) g WHERE l.id=g.link_id`, ids); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, "UPDATE traffic_events SET processed=true WHERE id=ANY($1)", ids); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, "UPDATE event_worker_state SET watermark=now() WHERE id=true"); err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(ids), nil
}
func (s *Store) RetainEvents(ctx context.Context, rawDays, aggregateDays int) error {
	if rawDays < 3 || rawDays > 30 || aggregateDays < rawDays || aggregateDays > 365 {
		return errors.New("invalid event retention")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", eventsLock); err != nil {
		return err
	}
	cutoff := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -rawDays)
	rows, err := tx.Query(ctx, `SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid=i.inhrelid WHERE i.inhparent='traffic_events'::regclass`)
	if err != nil {
		return err
	}
	names := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, name := range names {
		if len(name) != len("traffic_events_20060102") {
			continue
		}
		day, e := time.Parse("20060102", name[len("traffic_events_"):])
		if e != nil || !day.Before(cutoff) {
			continue
		}
		var pending bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+pgx.Identifier{name}.Sanitize()+" WHERE NOT processed)").Scan(&pending); err != nil {
			return err
		}
		if pending {
			continue
		}
		if _, err = tx.Exec(ctx, "DROP TABLE "+pgx.Identifier{name}.Sanitize()); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, "DELETE FROM api_decisions WHERE created_at<$1", cutoff); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM event_keys WHERE received_at<$1", cutoff); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM risk_observations WHERE bucket<$1", time.Now().UTC().AddDate(0, 0, -aggregateDays)); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM traffic_aggregates WHERE bucket<$1", time.Now().UTC().AddDate(0, 0, -aggregateDays)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type EventHealth struct {
	StaleAgents  int64      `json:"staleAgents"`
	ActiveAgents int64      `json:"activeAgents"`
	Pending      int64      `json:"pendingEvents"`
	Oldest       *time.Time `json:"oldestPendingReceivedAt"`
	Watermark    *time.Time `json:"watermark"`
	QueueDepth   int64      `json:"queueDepth"`
	QueueBytes   int64      `json:"queueBytes"`
	Dropped      int64      `json:"droppedEvents"`
	Buffered     int64      `json:"bufferedEvents"`
	WriteErrors  int64      `json:"queueWriteErrors"`
}

func (s *Store) EventHealth(ctx context.Context) (EventHealth, error) {
	var h EventHealth
	err := s.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM traffic_events WHERE NOT processed),(SELECT min(received_at) FROM traffic_events WHERE NOT processed),(SELECT watermark FROM event_worker_state WHERE id=true),coalesce(sum(queue_depth),0),coalesce(sum(queue_bytes),0),coalesce(sum(dropped_events),0),coalesce(sum(buffered_events),0),coalesce(sum(queue_write_errors),0),count(*),count(*) FILTER (WHERE last_seen_at IS NULL OR last_seen_at < now()-interval '90 seconds') FROM agents JOIN domains ON domains.id=agents.domain_id WHERE agents.revoked_at IS NULL AND agents.method='cli'`).Scan(&h.Pending, &h.Oldest, &h.Watermark, &h.QueueDepth, &h.QueueBytes, &h.Dropped, &h.Buffered, &h.WriteErrors, &h.ActiveAgents, &h.StaleAgents)
	return h, err
}
