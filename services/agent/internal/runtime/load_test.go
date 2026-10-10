package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"shortlink.local/policy"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestLocalCapacityHTTP(t *testing.T) {
	mode := os.Getenv("SHORTLINK_LOAD_MODE")
	if mode == "" {
		t.Skip("opt-in capacity test")
	}
	if mode != "smoke" && mode != "full" {
		t.Fatal("invalid local load mode")
	}
	if rate := os.Getenv("SHORTLINK_LOAD_RATE"); rate != "" && rate != "50" && rate != "300" && rate != "600" {
		t.Fatal("invalid local load rate")
	}
	a, center, closeCenter := testAgent(t)
	defer closeCenter()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := fixture(2)
	for i := 1; i < 32; i++ {
		link := snapshot.Links[0]
		link.ID = fmt.Sprintf("link-%d", i)
		link.Slug = fmt.Sprintf("slug-%d", i)
		snapshot.Links = append(snapshot.Links, link)
	}
	center.mu.Lock()
	center.raw = center.sign(snapshot)
	center.etag = "new"
	center.mu.Unlock()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	online := mode == "full" || os.Getenv("SHORTLINK_LOCAL_ONLINE") == "1"
	var receiverMu sync.Mutex
	seen := map[string]bool{}
	if online {
		receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/events/batch" {
				center.ServeHTTP(w, r)
				return
			}
			if r.Header.Get("Authorization") != "Bearer "+a.cfg.Credential {
				w.WriteHeader(401)
				return
			}
			var batch policy.EventBatch
			if json.NewDecoder(r.Body).Decode(&batch) != nil {
				w.WriteHeader(400)
				return
			}
			receiverMu.Lock()
			defer receiverMu.Unlock()
			accepted, duplicates := 0, 0
			for _, event := range batch.Events {
				if seen[event.EventID] {
					duplicates++
				} else {
					seen[event.EventID] = true
					accepted++
				}
			}
			json.NewEncoder(w).Encode(policy.BatchAccepted{Accepted: accepted, Duplicates: duplicates, RequestID: "load-receiver"})
		}))
		defer receiver.Close()
		a.cfg.ControlURL = receiver.URL
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); a.RunEvents(ctx) }()
		defer func() { cancel(); <-done }()
	} else {
		center.mu.Lock()
		center.status = 503
		center.mu.Unlock()
	}
	server := httptest.NewServer(a)
	defer server.Close()
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{MaxIdleConnsPerHost: 64}}
	defer client.CloseIdleConnections()
	type result struct {
		Rate, Requests, Errors                       int
		P95MS, P99MS                                 float64
		QueueDepth, QueueBytes, Dropped, WriteErrors int64
		Buffered                                     int
	}
	report := struct {
		Environment     string
		Results         []result
		DeliveredUnique int
		DrainComplete   bool
	}{Environment: "loopback Go; optional GeoLite; 32 slugs; center 503; no event upload; 10-second diagnostic stages"}
	if online {
		report.Environment = "loopback Go; optional GeoLite; 32 slugs; healthy HTTP receiver stub with durable SQLite upload; shared host"
	}
	for index, rate := range []int{50, 300, 600} {
		if selected := os.Getenv("SHORTLINK_LOAD_RATE"); selected != "" && selected != strconv.Itoa(rate) {
			continue
		}
		seconds := 10
		if mode == "full" {
			seconds = []int{1800, 900, 300}[index]
		} else if value := os.Getenv("SHORTLINK_LOAD_SECONDS"); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 120 {
				t.Fatal("invalid smoke duration")
			}
			seconds = n
		}
		r := result{Rate: rate, Requests: rate * seconds}
		start := time.Now()
		var wg sync.WaitGroup
		var mu sync.Mutex
		latencies := []float64{}
		for n := 0; n < r.Requests; n++ {
			if n > 0 && n%(rate*30) == 0 {
				mu.Lock()
				t.Logf("local progress rate=%d elapsed=%.0fs scheduled=%d errors=%d", rate, time.Since(start).Seconds(), n, r.Errors)
				mu.Unlock()
			}
			if delay := time.Until(start.Add(time.Duration(n) * time.Second / time.Duration(rate))); delay > 0 {
				time.Sleep(delay)
			}
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				slug := "hello"
				if n%32 != 0 {
					slug = fmt.Sprintf("slug-%d", n%32)
				}
				req, _ := http.NewRequest("GET", server.URL+"/"+slug, nil)
				req.Host = a.cfg.Hostname
				req.Header.Set("User-Agent", fmt.Sprintf("unique-agent-%d Mozilla/5.0", n))
				began := time.Now()
				response, err := client.Do(req)
				latency := float64(time.Since(began)) / float64(time.Millisecond)
				bad := err != nil
				if err == nil {
					bad = response.StatusCode != 302
					io.Copy(io.Discard, response.Body)
					response.Body.Close()
				}
				mu.Lock()
				latencies = append(latencies, latency)
				if bad {
					r.Errors++
				}
				mu.Unlock()
			}(n)
		}
		wg.Wait()
		sort.Float64s(latencies)
		r.P95MS = latencies[int(float64(len(latencies)-1)*.95)]
		r.P99MS = latencies[int(float64(len(latencies)-1)*.99)]
		stats, err := a.queue.Stats(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		r.Dropped = stats.Dropped
		r.WriteErrors = stats.WriteErrors
		r.Buffered = stats.Buffered
		r.QueueDepth = stats.Depth
		r.QueueBytes = stats.Bytes
		report.Results = append(report.Results, r)
		t.Logf("local rate=%d errors=%d p95=%.2fms queue=%d", rate, r.Errors, r.P95MS, r.QueueDepth)
	}
	if online {
		deadline := time.Now().Add(15 * time.Second)
		for {
			stats, err := a.queue.Stats(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if stats.Depth == 0 && stats.Buffered == 0 {
				report.DrainComplete = true
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("local queue did not drain")
			}
			time.Sleep(100 * time.Millisecond)
		}
		receiverMu.Lock()
		report.DeliveredUnique = len(seen)
		receiverMu.Unlock()
		successful := 0
		for _, r := range report.Results {
			successful += r.Requests - r.Errors
			if mode == "full" && (float64(r.Errors)/float64(r.Requests) >= .001 || r.P95MS >= 20 || r.Dropped != 0 || r.WriteErrors != 0) {
				t.Errorf("local gate failed at %d RPS", r.Rate)
			}
		}
		if report.DeliveredUnique != successful {
			t.Fatalf("local delivered=%d successful=%d", report.DeliveredUnique, successful)
		}
	}
	if output := os.Getenv("SHORTLINK_LOCAL_LOAD_REPORT"); output != "" {
		raw, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(output, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
