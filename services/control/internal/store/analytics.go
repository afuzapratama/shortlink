package store

import (
	"context"
	"encoding/json"
	"shortlink.local/policy"
	"time"
)

type TrafficFilter struct {
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	Domain  string    `json:"domainId"`
	Link    string    `json:"linkId"`
	Action  string    `json:"action"`
	Country string    `json:"country"`
	Device  string    `json:"device"`
	OS      string    `json:"os"`
	Browser string    `json:"browser"`
}
type EventRecord struct {
	ID         int64               `json:"id"`
	ReceivedAt time.Time           `json:"receivedAt"`
	AgentID    string              `json:"agentId"`
	Processed  bool                `json:"processed"`
	Event      policy.TrafficEvent `json:"event"`
}

const trafficWhere = ` occurred_at >= $1 AND occurred_at < $2 AND ($3='' OR domain_id=$3) AND ($4='' OR link_id=$4) AND ($5='' OR payload->>'action'=$5) AND ($6='' OR payload->>'country'=$6) AND ($7='' OR payload->>'device'=$7) AND ($8='' OR payload->>'os'=$8) AND ($9='' OR payload->>'browser'=$9)`

func (f TrafficFilter) args() []any {
	return []any{f.From, f.To, f.Domain, f.Link, f.Action, f.Country, f.Device, f.OS, f.Browser}
}

// Snapshot is the largest visible ingest ID, preventing new arrivals entering later pages.
func (s *Store) EventSnapshot(ctx context.Context) (int64, error) {
	var id int64
	err := s.DB.QueryRow(ctx, `SELECT coalesce(max(id),0) FROM traffic_events`).Scan(&id)
	return id, err
}
func (s *Store) ReadEvents(ctx context.Context, f TrafficFilter, snapshot int64, before time.Time, beforeID int64, limit int) ([]EventRecord, error) {
	args := append(f.args(), snapshot, before, beforeID, limit)
	rows, err := s.DB.Query(ctx, `SELECT id,received_at,agent_id,processed,payload FROM traffic_events WHERE `+trafficWhere+` AND id<=$10 AND ($12=0 OR (received_at,id)<($11,$12)) ORDER BY received_at DESC,id DESC LIMIT $13`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EventRecord{}
	for rows.Next() {
		var r EventRecord
		var raw []byte
		if err = rows.Scan(&r.ID, &r.ReceivedAt, &r.AgentID, &r.Processed, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &r.Event); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type TrafficBucket struct {
	BotEvents int64     `json:"botEvents"`
	Bucket    time.Time `json:"bucket"`
	Action    string    `json:"action"`
	Events    int64     `json:"events"`
}

func (s *Store) TrafficSummary(ctx context.Context, f TrafficFilter) ([]TrafficBucket, error) {
	// Full hours use hourly rollups; minute rollups cover partial boundary hours.
	startHour := f.From.UTC().Truncate(time.Hour)
	if !startHour.Equal(f.From) {
		startHour = startHour.Add(time.Hour)
	}
	endHour := f.To.UTC().Truncate(time.Hour)
	args := append(f.args(), startHour, endHour)
	rows, err := s.DB.Query(ctx, `SELECT date_trunc('hour',bucket,'UTC'),action,sum(events)::bigint,sum(bot_events)::bigint FROM traffic_aggregates WHERE ((grain='hour' AND bucket >= $10 AND bucket < $11) OR (grain='minute' AND (bucket < $10 OR bucket >= $11))) AND bucket >= $1 AND bucket < $2 AND ($3='' OR domain_id=$3) AND ($4='' OR link_id=$4) AND ($5='' OR action=$5) AND ($6='' OR country=$6) AND ($7='' OR device=$7) AND ($8='' OR os=$8) AND ($9='' OR browser=$9) GROUP BY 1,2 ORDER BY 1,2`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrafficBucket{}
	for rows.Next() {
		var b TrafficBucket
		if err = rows.Scan(&b.Bucket, &b.Action, &b.Events, &b.BotEvents); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
