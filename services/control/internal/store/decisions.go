package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	lru "github.com/hashicorp/golang-lru"
	"github.com/jackc/pgx/v5"
	"shortlink.local/policy"
	"sync"
	"time"
)

type decisionCache struct {
	mu         sync.Mutex
	items      *lru.Cache
	partitions map[string]bool
}
type Routing struct {
	Revision   int64
	Protection policy.Protection
	Engine     *policy.Engine
	Links      map[string]Link
}

func (s *Store) routing(ctx context.Context, tx pgx.Tx, domain string, revision int64) (*Routing, error) {
	s.routingCache.mu.Lock()
	defer s.routingCache.mu.Unlock()
	if s.routingCache.items == nil {
		s.routingCache.items, _ = lru.New(128)
	}
	if entry, ok := s.routingCache.items.Get(domain); ok {
		r := entry.(*Routing)
		if r.Revision == revision {
			return r, nil
		}
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT payload FROM snapshots WHERE domain_id=$1 AND revision=$2", domain, revision).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("API configuration unavailable")
		}
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}
	if snap.Domain.ID != domain || snap.Revision != revision {
		return nil, ErrConflict
	}
	ids := map[string]bool{}
	links := map[string]Link{}
	for _, l := range snap.Links {
		ids[l.ID] = true
		links[l.Slug] = l
	}
	engine, err := policy.Compile(snap.Rules, domain, snap.Domain.Hostname, ids)
	if err != nil {
		return nil, err
	}
	r := &Routing{revision, policy.NormalizeProtection(snap.Protection), engine, links}
	s.routingCache.items.Add(domain, r)
	return r, nil
}

// MakeDecision commits replay response and classified event together. No visitor IP/UA is persisted.
func (s *Store) MakeDecision(ctx context.Context, a Agent, eventID, hash, slug string, at time.Time, evaluate func(*Routing, Link) (policy.TrafficEvent, error)) (policy.Decision, bool, error) {
	defer Measure(ctx, "transaction_total")()
	var result policy.Decision
	if a.Method != "php_zip" {
		return result, false, ErrBinding
	}
	poolDone := Measure(ctx, "pool_begin")
	tx, err := s.DB.Begin(ctx)
	poolDone()
	if err != nil {
		return result, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL synchronous_commit=on"); err != nil {
		return result, false, err
	}
	lockDone := Measure(ctx, "event_lock")
	_, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", eventsLock)
	lockDone()
	if err != nil {
		return result, false, err
	}
	// Domain before agent matches revoke/publish lock ordering.
	var revision int64
	var mode string
	domainDone := Measure(ctx, "domain_lock")
	err = tx.QueryRow(ctx, "SELECT desired_revision,mode FROM domains WHERE id=$1 FOR UPDATE", a.DomainID).Scan(&revision, &mode)
	domainDone()
	if err != nil {
		return result, false, err
	}
	if mode != "api" {
		return result, false, ErrBinding
	}
	var active bool
	if err = tx.QueryRow(ctx, "SELECT revoked_at IS NULL AND method='php_zip' FROM agents WHERE id=$1 AND domain_id=$2 FOR UPDATE", a.ID, a.DomainID).Scan(&active); err != nil {
		return result, false, err
	}
	if !active {
		return result, false, ErrUsed
	}
	var oldHash string
	var raw []byte
	err = tx.QueryRow(ctx, "SELECT request_hash,decision FROM api_decisions WHERE agent_id=$1 AND event_id=$2", a.ID, eventID).Scan(&oldHash, &raw)
	if err == nil {
		if oldHash != hash {
			return result, false, ErrEventConflict
		}
		if err = json.Unmarshal(raw, &result); err != nil {
			return result, false, err
		}
		return result, true, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, false, err
	}
	routing, err := s.routing(ctx, tx, a.DomainID, revision)
	if err != nil {
		return result, false, err
	}
	link, ok := routing.Links[slug]
	if !ok {
		return result, false, pgx.ErrNoRows
	}
	evaluateDone := Measure(ctx, "evaluate")
	event, err := evaluate(routing, link)
	evaluateDone()
	if err != nil {
		return result, false, err
	}
	event.EventID = eventID
	event.ConfigRevision = revision
	event.OccurredAt = at
	event.DomainID = a.DomainID
	event.LinkID = link.ID
	event.Source = "php"
	result = event.Decision
	payload, err := json.Marshal(event)
	if err != nil {
		return result, false, err
	}
	digest := sha256.Sum256(payload)
	day := eventPartition(at)
	s.routingCache.mu.Lock()
	known := s.routingCache.partitions[day]
	s.routingCache.mu.Unlock()
	if !known {
		if err = ensureEventPartition(ctx, tx, at); err != nil {
			return result, false, err
		}
	}
	response, err := json.Marshal(result)
	if err != nil {
		return result, false, err
	}
	// One protocol batch reduces round trips while keeping ordered statements and atomic commit.
	batch := &pgx.Batch{}
	batch.Queue("INSERT INTO event_keys(agent_id,event_id,payload_hash) VALUES($1,$2,$3)", a.ID, eventID, hex.EncodeToString(digest[:]))
	batch.Queue("INSERT INTO traffic_events(occurred_at,agent_id,domain_id,link_id,event_id,payload) VALUES($1,$2,$3,$4,$5,$6)", at, a.ID, a.DomainID, link.ID, eventID, payload)
	batch.Queue("INSERT INTO api_decisions(agent_id,event_id,request_hash,decision) VALUES($1,$2,$3,$4)", a.ID, eventID, hash, response)
	batch.Queue("UPDATE domains SET connection='online',last_seen_at=now(),applied_revision=$2 WHERE id=$1", a.DomainID, revision)
	batch.Queue("UPDATE agents SET geo_release=$2,feed_version=$3,protection_version=1 WHERE id=$1", a.ID, result.GeoRelease, result.FeedVersion)
	writeDone := Measure(ctx, "write_batch")
	results := tx.SendBatch(ctx, batch)
	for range 5 {
		if _, err = results.Exec(); err != nil {
			results.Close()
			writeDone()
			return result, false, err
		}
	}
	err = results.Close()
	writeDone()
	if err != nil {
		return result, false, err
	}
	commitDone := Measure(ctx, "commit")
	err = tx.Commit(ctx)
	commitDone()
	if err == nil {
		s.routingCache.mu.Lock()
		if s.routingCache.partitions == nil {
			s.routingCache.partitions = map[string]bool{}
		}
		s.routingCache.partitions[day] = true
		for name := range s.routingCache.partitions {
			if name < eventPartition(time.Now().Add(-48*time.Hour)) {
				delete(s.routingCache.partitions, name)
			}
		}
		s.routingCache.mu.Unlock()
	}
	return result, false, err
}
func (s *Store) AdapterProbe(ctx context.Context, a Agent, hostname, version, nonce string, geo string) (policy.ProbeState, error) {
	var p policy.ProbeState
	if a.Method != "php_zip" {
		return p, ErrBinding
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return p, err
	}
	defer tx.Rollback(ctx)
	var host, mode string
	var revision int64
	if err = tx.QueryRow(ctx, "SELECT hostname,mode,desired_revision FROM domains WHERE id=$1 FOR UPDATE", a.DomainID).Scan(&host, &mode, &revision); err != nil {
		return p, err
	}
	if hostname != host || mode != "api" {
		return p, ErrBinding
	}
	var active bool
	if err = tx.QueryRow(ctx, "SELECT revoked_at IS NULL FROM agents WHERE id=$1 AND domain_id=$2 FOR UPDATE", a.ID, a.DomainID).Scan(&active); err != nil {
		return p, err
	}
	if !active {
		return p, ErrUsed
	}
	if _, err = s.routing(ctx, tx, a.DomainID, revision); err != nil {
		return p, err
	}
	if _, err = tx.Exec(ctx, "UPDATE domains SET connection='online',last_seen_at=now(),applied_revision=$2 WHERE id=$1", a.DomainID, revision); err != nil {
		return p, err
	}
	if _, err = tx.Exec(ctx, "UPDATE agents SET version=$2,geo_release=$3,protection_version=1 WHERE id=$1", a.ID, version, geo); err != nil {
		return p, err
	}
	p = policy.ProbeState{Nonce: nonce, DomainID: a.DomainID, Revision: revision, GeoRelease: &geo, Ready: true, Version: version}
	return p, tx.Commit(ctx)
}
