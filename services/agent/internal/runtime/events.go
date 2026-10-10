package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"shortlink.local/agent/internal/eventqueue"
	"shortlink.local/policy"
	"time"
)

func (a *Agent) openQueue() (*Agent, error) {
	q, e := eventqueue.Open(a.cfg.DataDir, eventqueue.Options{MaxBytes: a.cfg.QueueMaxBytes, MaxAge: time.Duration(a.cfg.QueueMaxAgeSeconds) * time.Second})
	if e != nil {
		return nil, fmt.Errorf("event queue unavailable: %w", e)
	}
	a.queue = q
	return a, nil
}
func (a *Agent) Close() error { return a.queue.Close() }

// FlushEvents deletes only a fully acknowledged immutable batch. Lost responses retry the same IDs.
func (a *Agent) FlushEvents(ctx context.Context) error {
	if a.blocked.Load() {
		return fmt.Errorf("credential blocked")
	}
	events, e := a.queue.Batch(ctx, 100)
	if e != nil {
		return e
	}
	if len(events) == 0 {
		return nil
	}
	raw, e := json.Marshal(policy.EventBatch{Events: events})
	if e != nil {
		return e
	}
	response, e := a.request(ctx, http.MethodPost, "/v1/events/batch", bytes.NewReader(raw))
	if e != nil {
		return e
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return a.rejectCredential()
	}
	if response.StatusCode != 200 {
		return fmt.Errorf("event ingest HTTP %d", response.StatusCode)
	}
	var ack policy.BatchAccepted
	data, e := io.ReadAll(io.LimitReader(response.Body, 4097))
	if e != nil || len(data) > 4096 {
		return fmt.Errorf("invalid event acknowledgement")
	}
	if json.Unmarshal(data, &ack) != nil || ack.Accepted < 0 || ack.Duplicates < 0 || ack.Accepted+ack.Duplicates != len(events) || ack.RequestID == "" {
		return fmt.Errorf("incomplete event acknowledgement")
	}
	return a.queue.Ack(ctx, events)
}
func (a *Agent) RunEvents(ctx context.Context) {
	delay := time.Second
	failures := 0
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if a.blocked.Load() {
			return
		}
		if e := a.FlushEvents(ctx); e != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("event batch retry", "error", e.Error())
			failures++
			if failures > 5 {
				failures = 5
			}
			delay = time.Duration(1<<failures) * time.Second
		} else {
			failures = 0
			delay = time.Second
			stats, _ := a.queue.Stats(ctx)
			if stats.Depth >= 100 {
				delay = 10 * time.Millisecond
			}
		}
	}
}
