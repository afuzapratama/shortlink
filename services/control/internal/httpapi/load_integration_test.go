package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Opt-in benchmark: real HTTP + disposable PostgreSQL + shared GeoLite/rules engine.
// Not run by ordinary correctness tests. Full durations are explicit production gates.
func TestCapacityHTTP(t *testing.T) {
	mode := os.Getenv("SHORTLINK_LOAD_MODE")
	if mode == "" {
		t.Skip("opt-in capacity test")
	}
	if mode != "smoke" && mode != "full" {
		t.Fatal("invalid load mode")
	}
	s, db, d, _, _, credential := apiFixture(t)
	var timingMu sync.Mutex
	timings := map[string][]float64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := store.WithTiming(r.Context(), func(phase string, d time.Duration) {
			timingMu.Lock()
			timings[phase] = append(timings[phase], float64(d)/float64(time.Millisecond))
			timingMu.Unlock()
		})
		s.ServeHTTP(w, r.WithContext(ctx))
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(store.WithTiming(context.Background(), func(phase string, d time.Duration) {
		timingMu.Lock()
		timings[phase] = append(timings[phase], float64(d)/float64(time.Millisecond))
		timingMu.Unlock()
	}))
	defer cancel()
	var worker sync.WaitGroup
	worker.Add(1)
	go func() {
		defer worker.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				task, done := context.WithTimeout(ctx, 10*time.Second)
				_, err := db.AggregateEvents(task, 100)
				done()
				if err != nil && ctx.Err() == nil {
					t.Errorf("worker failed: %v", err)
					return
				}
			}
		}
	}()
	defer worker.Wait()
	defer cancel()
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{MaxIdleConns: 128, MaxIdleConnsPerHost: 128, MaxConnsPerHost: 128}}
	defer client.CloseIdleConnections()
	type result struct {
		Rate                                                   int     `json:"rate"`
		Seconds                                                float64 `json:"seconds"`
		Scheduled, Completed, Errors, Rejected, GeneratorDrops int
		StatusCounts                                           map[int]int    `json:"statusCounts"`
		ErrorCauses                                            map[string]int `json:"errorCauses"`
		P50MS, P95MS, P99MS                                    float64
		HeapBytes, AllocBytes, NumGC                           uint64
		Events, Pending                                        int64
		ApplicationGate                                        bool
	}
	report := struct {
		Mode, Path, Environment                             string
		Results                                             []result
		DrainSeconds                                        float64
		PendingAfterDrain, UniqueEvents, SuccessfulRequests int64
		DrainComplete                                       bool
		RestoreMatches                                      bool
		RestoreRequested                                    bool
		BackupSeconds, RestoreSeconds                       float64
		BackupBytes                                         int64
		PhaseTimings                                        map[string]struct {
			Samples                    int
			P50MS, P95MS, P99MS, MaxMS float64
		}
	}{Mode: mode, Path: "central-decision-http", Environment: "loopback Linux; fixture GeoLite; uncapped shared developer host; excludes PHP/provider/TLS/network"}
	for index, rate := range []int{50, 300, 600} {
		if selected := os.Getenv("SHORTLINK_LOAD_RATE"); selected != "" {
			n, err := strconv.Atoi(selected)
			if err != nil || (n != 50 && n != 300 && n != 600) {
				t.Fatal("invalid selected rate")
			}
			if rate != n {
				continue
			}
		}
		duration := []time.Duration{30 * time.Minute, 15 * time.Minute, 5 * time.Minute}[index]
		if mode == "smoke" {
			duration = 10 * time.Second
			if v := os.Getenv("SHORTLINK_LOAD_SECONDS"); v != "" {
				n, err := strconv.Atoi(v)
				if err != nil || n < 1 || n > 120 {
					t.Fatal("invalid smoke duration")
				}
				duration = time.Duration(n) * time.Second
			}
		}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		r := result{Rate: rate, Seconds: duration.Seconds(), StatusCounts: map[int]int{}, ErrorCauses: map[string]int{}}
		var mutex sync.Mutex
		latencies := []float64{}
		slots := make(chan struct{}, 128)
		var requests sync.WaitGroup
		start := time.Now()
		total := int(duration.Seconds() * float64(rate))
		r.Scheduled = total
		for n := 0; n < total; n++ {
			if n > 0 && n%(rate*30) == 0 {
				mutex.Lock()
				t.Logf("progress rate=%d elapsed=%.0fs scheduled=%d completed=%d errors=%d generatorDrops=%d", rate, time.Since(start).Seconds(), n, r.Completed, r.Errors, r.GeneratorDrops)
				mutex.Unlock()
			}
			target := start.Add(time.Duration(float64(n) * float64(time.Second) / float64(rate)))
			if delay := time.Until(target); delay > 0 {
				time.Sleep(delay)
			}
			select {
			case slots <- struct{}{}:
			default:
				mutex.Lock()
				r.GeneratorDrops++
				mutex.Unlock()
				continue
			}
			requests.Add(1)
			go func(n int) {
				defer requests.Done()
				defer func() { <-slots }()
				ua := apiIphone
				if n%5 == 0 {
					ua = fmt.Sprintf("hostile-unique-%s %s", security.Token(), ua)
				}
				in := decisionBody(d.ID, ua)
				if n%2 == 0 {
					in.Signals.IP = "2606:4700:4700::1111"
				}
				raw, _ := json.Marshal(in)
				req, _ := http.NewRequest("POST", server.URL+"/v1/decisions", bytes.NewReader(raw))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+credential)
				cause := "transport_error"
				began := time.Now()
				response, err := client.Do(req)
				if err == nil {
					body, readErr := io.ReadAll(io.LimitReader(response.Body, 32<<10))
					if response.StatusCode >= 400 {
						var failure struct{ Error struct{ Code string } }
						if json.Unmarshal(body, &failure) == nil && failure.Error.Code != "" {
							cause = failure.Error.Code
						}
					}
					response.Body.Close()
					if readErr != nil {
						err = readErr
					}
				}
				elapsed := float64(time.Since(began)) / float64(time.Millisecond)
				mutex.Lock()
				defer mutex.Unlock()
				r.Completed++
				latencies = append(latencies, elapsed)
				if err != nil {
					r.Errors++
					r.ErrorCauses[cause]++
					return
				}
				r.StatusCounts[response.StatusCode]++
				if response.StatusCode == 429 {
					r.Rejected++
				} else if response.StatusCode != 200 {
					r.Errors++
					r.ErrorCauses[cause]++
				}
			}(n)
		}
		requests.Wait()
		runtime.ReadMemStats(&after)
		sort.Float64s(latencies)
		percentile := func(p float64) float64 {
			if len(latencies) == 0 {
				return 0
			}
			return latencies[int(float64(len(latencies)-1)*p)]
		}
		r.P50MS = percentile(.50)
		r.P95MS = percentile(.95)
		r.P99MS = percentile(.99)
		r.HeapBytes = after.HeapAlloc
		r.AllocBytes = after.TotalAlloc - before.TotalAlloc
		r.NumGC = uint64(after.NumGC - before.NumGC)
		if err := db.DB.QueryRow(ctx, "SELECT count(*),count(*) FILTER(WHERE NOT processed) FROM traffic_events").Scan(&r.Events, &r.Pending); err != nil {
			t.Fatal(err)
		}
		r.ApplicationGate = float64(r.Errors+r.Rejected+r.GeneratorDrops)/float64(total) < .001 && r.P95MS < 50
		report.Results = append(report.Results, r)
		t.Logf("rate=%d completed=%d errors=%d rejected=%d generatorDrops=%d p95=%.2fms p99=%.2fms pending=%d", rate, r.Completed, r.Errors, r.Rejected, r.GeneratorDrops, r.P95MS, r.P99MS, r.Pending)
	}
	// Stop incoming traffic but keep aggregation running, then verify catch-up.
	drainStart := time.Now()
	deadline := drainStart.Add(15 * time.Second)
	for {
		if err := db.DB.QueryRow(ctx, "SELECT count(*) FILTER(WHERE NOT processed),count(DISTINCT event_id) FROM traffic_events").Scan(&report.PendingAfterDrain, &report.UniqueEvents); err != nil {
			t.Fatal(err)
		}
		if report.PendingAfterDrain == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	report.DrainSeconds = time.Since(drainStart).Seconds()
	report.DrainComplete = report.PendingAfterDrain == 0
	for _, r := range report.Results {
		report.SuccessfulRequests += int64(r.StatusCounts[200])
	}
	var counter int64
	if err := db.DB.QueryRow(ctx, "SELECT clicks FROM links WHERE domain_id=$1", d.ID).Scan(&counter); err != nil {
		t.Fatal(err)
	}
	if !report.DrainComplete || report.UniqueEvents != report.SuccessfulRequests || counter != report.SuccessfulRequests {
		t.Errorf("catch-up mismatch: successful=%d unique=%d clicks=%d pending=%d", report.SuccessfulRequests, report.UniqueEvents, counter, report.PendingAfterDrain)
	}
	t.Logf("catchup=%.2fs pending=%d unique=%d successful=%d clicks=%d", report.DrainSeconds, report.PendingAfterDrain, report.UniqueEvents, report.SuccessfulRequests, counter)
	cancel()
	worker.Wait()
	report.PhaseTimings = map[string]struct {
		Samples                    int
		P50MS, P95MS, P99MS, MaxMS float64
	}{}
	timingMu.Lock()
	for phase, values := range timings {
		sort.Float64s(values)
		n := len(values)
		report.PhaseTimings[phase] = struct {
			Samples                    int
			P50MS, P95MS, P99MS, MaxMS float64
		}{n, values[int(float64(n-1)*.5)], values[int(float64(n-1)*.95)], values[int(float64(n-1)*.99)], values[n-1]}
	}
	timingMu.Unlock()
	if os.Getenv("SHORTLINK_LOAD_RESTORE") == "1" {
		report.RestoreRequested = true
		target := os.Getenv("SHORTLINK_RESTORE_DATABASE_URL")
		if target == "" {
			t.Fatal("isolated restore target required")
		}
		backupDir := filepath.Join(t.TempDir(), "backup")
		run := func(script string) {
			t.Helper()
			cmd := exec.Command("bash", script, backupDir)
			cmd.Env = append(os.Environ(), "DATABASE_URL="+os.Getenv("CONTROL_TEST_DATABASE_URL"), "RESTORE_DATABASE_URL="+target)
			if err := cmd.Run(); err != nil {
				t.Fatalf("isolated backup/restore failed: %v", err)
			}
		}
		began := time.Now()
		run("../../../../scripts/backup-control.sh")
		report.BackupSeconds = time.Since(began).Seconds()
		info, err := os.Stat(filepath.Join(backupDir, "control.dump"))
		if err != nil {
			t.Fatal(err)
		}
		report.BackupBytes = info.Size()
		began = time.Now()
		run("../../../../scripts/restore-control.sh")
		report.RestoreSeconds = time.Since(began).Seconds()
		var schema string
		if err = db.DB.QueryRow(context.Background(), "SELECT current_schema()").Scan(&schema); err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(target)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		restored, err := pgxpool.New(context.Background(), u.String())
		if err != nil {
			t.Fatal(err)
		}
		defer restored.Close()
		fingerprint := `SELECT md5(jsonb_build_object('owner',(SELECT jsonb_agg(to_jsonb(o)) FROM owners o),'keys',(SELECT jsonb_agg(to_jsonb(k)) FROM control_keys k),'links',(SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM links l),'events',(SELECT count(*) FROM traffic_events),'dedup',(SELECT count(*) FROM event_keys),'replay',(SELECT count(*) FROM api_decisions),'rollups',(SELECT jsonb_agg(to_jsonb(a) ORDER BY grain,bucket,domain_id,link_id,country,device,os,browser,action,reason) FROM traffic_aggregates a))::text)`
		var before, after string
		if err = db.DB.QueryRow(context.Background(), fingerprint).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if err = restored.QueryRow(context.Background(), fingerprint).Scan(&after); err != nil {
			t.Fatal(err)
		}
		report.RestoreMatches = before == after
		if !report.RestoreMatches {
			t.Fatal("load dataset changed during restore")
		}
		t.Logf("load backup=%.2fs restore=%.2fs bytes=%d fingerprint preserved", report.BackupSeconds, report.RestoreSeconds, report.BackupBytes)
	}
	if output := os.Getenv("SHORTLINK_LOAD_REPORT"); output != "" {
		raw, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(output, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "full" {
		for _, r := range report.Results {
			if !r.ApplicationGate {
				t.Errorf("capacity gate failed at %d RPS: see report", r.Rate)
			}
		}
	}

}
