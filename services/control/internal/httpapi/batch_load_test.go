package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"shortlink.local/control/internal/security"
	"shortlink.local/policy"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestBatchCapacityHTTP(t *testing.T) {
	mode := os.Getenv("SHORTLINK_LOAD_MODE")
	if mode == "" {
		t.Skip("opt-in batch capacity test")
	}
	rate, err := strconv.Atoi(os.Getenv("SHORTLINK_LOAD_RATE"))
	if err != nil || (rate != 50 && rate != 300 && rate != 600) {
		t.Fatal("select 50/300/600 events per second")
	}
	seconds := 10
	if mode == "full" {
		seconds = map[int]int{50: 1800, 300: 900, 600: 300}[rate]
	} else if value := os.Getenv("SHORTLINK_LOAD_SECONDS"); value != "" {
		seconds, err = strconv.Atoi(value)
		if err != nil || seconds < 1 || seconds > 120 {
			t.Fatal("invalid duration")
		}
	}
	s, db := setup(t)
	ctx := context.Background()
	d, err := db.CreateDomain(ctx, security.Token(), "batch-load.example.test", "local")
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := db.Challenge(ctx, d.ID, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Verify(ctx, d.ID, challenge.Value, "fixture"); err != nil {
		t.Fatal(err)
	}
	token, err := db.IssueToken(ctx, d.ID, "cli", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	_, credential, err := db.Enroll(ctx, token.Token, token.InstallationID, security.Token(), d.Hostname, "cli", "b19-load")
	if err != nil {
		t.Fatal(err)
	}
	link, err := db.CreateLink(ctx, security.Token(), d.ID, "hello", "https://example.org/batch", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishLink(ctx, link.ID, link.Revision, 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	d, err = db.Domain(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(s)
	defer server.Close()
	client := &http.Client{Timeout: 10 * time.Second}
	defer client.CloseIdleConnections()
	workerCtx, cancel := context.WithCancel(ctx)
	var worker sync.WaitGroup
	worker.Add(1)
	go func() {
		defer worker.Done()
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
					t.Errorf("batch worker failed: %v", e)
					return
				}
			}
		}
	}()
	defer func() { cancel(); worker.Wait() }()
	report := struct {
		Path, Scope                                                     string
		Rate, Seconds, ScheduledEvents, HTTPRequests, Errors, Accepted  int
		ElapsedSeconds, AchievedEventsPerSecond, P95BatchMS, P99BatchMS float64
		Unique, Clicks, Pending                                         int64
		AccountingMatches                                               bool
	}{Path: "cli-event-batch-http", Scope: "real CLI enrollment, synthetic classified events, HTTP/PostgreSQL ingest+worker; no agent routing; shared tmpfs host", Rate: rate, Seconds: seconds, ScheduledEvents: rate * seconds}
	latencies := []float64{}
	start := time.Now()
	destination := "https://example.org/batch"
	for generated := 0; generated < report.ScheduledEvents; generated += 100 {
		if delay := time.Until(start.Add(time.Duration(generated) * time.Second / time.Duration(rate))); delay > 0 {
			time.Sleep(delay)
		}
		if generated > 0 && generated%(rate*30) == 0 {
			t.Logf("batch progress eventsPerSecond=%d generated=%d accepted=%d errors=%d", rate, generated, report.Accepted, report.Errors)
		}
		count := 100
		if left := report.ScheduledEvents - generated; left < count {
			count = left
		}
		batch := policy.EventBatch{}
		for i := 0; i < count; i++ {
			batch.Events = append(batch.Events, policy.TrafficEvent{Decision: policy.Decision{EventID: security.Token(), Action: "redirect", ReasonCode: "link_destination", Destination: &destination, ConfigRevision: d.DesiredRevision}, OccurredAt: time.Now().UTC(), DomainID: d.ID, LinkID: link.ID, Country: "ID", Device: "mobile", OS: "ios", Browser: "safari", Source: "go"})
		}
		raw, _ := json.Marshal(batch)
		request, _ := http.NewRequest("POST", server.URL+"/v1/events/batch", bytes.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+credential)
		began := time.Now()
		response, e := client.Do(request)
		report.HTTPRequests++
		if e != nil {
			report.Errors += count
			continue
		}
		body, e := io.ReadAll(io.LimitReader(response.Body, 32<<10))
		response.Body.Close()
		latencies = append(latencies, float64(time.Since(began))/float64(time.Millisecond))
		var ack policy.BatchAccepted
		if e != nil || response.StatusCode != 200 || json.Unmarshal(body, &ack) != nil || ack.Accepted+ack.Duplicates != count {
			report.Errors += count
		} else {
			report.Accepted += count
		}
	}
	report.ElapsedSeconds = time.Since(start).Seconds()
	report.AchievedEventsPerSecond = float64(report.Accepted) / report.ElapsedSeconds
	deadline := time.Now().Add(15 * time.Second)
	for {
		if err = db.DB.QueryRow(ctx, "SELECT count(DISTINCT event_id),count(*) FILTER(WHERE NOT processed) FROM traffic_events").Scan(&report.Unique, &report.Pending); err != nil {
			t.Fatal(err)
		}
		if report.Pending == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err = db.DB.QueryRow(ctx, "SELECT clicks FROM links WHERE id=$1", link.ID).Scan(&report.Clicks); err != nil {
		t.Fatal(err)
	}
	sort.Float64s(latencies)
	if len(latencies) > 0 {
		report.P95BatchMS = latencies[int(float64(len(latencies)-1)*.95)]
		report.P99BatchMS = latencies[int(float64(len(latencies)-1)*.99)]
	}
	report.AccountingMatches = report.Unique == int64(report.Accepted) && report.Clicks == report.Unique && report.Pending == 0
	if output := os.Getenv("SHORTLINK_LOAD_REPORT"); output != "" {
		raw, _ := json.MarshalIndent(report, "", "  ")
		if err = os.WriteFile(output, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("batch events=%d accepted=%d errors=%d p95=%.2fms unique=%d clicks=%d pending=%d", report.ScheduledEvents, report.Accepted, report.Errors, report.P95BatchMS, report.Unique, report.Clicks, report.Pending)
	if !report.AccountingMatches || (mode == "full" && (float64(report.Errors)/float64(report.ScheduledEvents) >= .001 || report.ElapsedSeconds > float64(seconds)*1.01)) {
		t.Fatal("batch capacity/accounting gate failed")
	}
}
