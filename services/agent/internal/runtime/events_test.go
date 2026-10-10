package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"shortlink.local/policy"
	"sync"
	"testing"
	"time"
)

func TestEventOutageStableRetryAndAcknowledgement(t *testing.T) {
	a, _, closeServer := testAgent(t)
	defer closeServer()
	if e := a.Sync(context.Background()); e != nil {
		t.Fatal(e)
	}
	redirect(t, a, a.cfg.Hostname, "/hello?private=not-recorded", 302)
	deadline := time.Now().Add(2 * time.Second)
	for {
		stats, _ := a.queue.Stats(context.Background())
		if stats.Depth == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("event missing")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var first string
	calls := 0
	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+a.cfg.Credential || r.URL.Path != "/v1/events/batch" {
			t.Error("binding/auth")
		}
		var batch policy.EventBatch
		json.NewDecoder(r.Body).Decode(&batch)
		if len(batch.Events) != 1 {
			t.Error("batch size")
			return
		}
		event := batch.Events[0]
		if event.ConfigRevision != 1 || event.LinkID != "hello-link" || event.Country != "unknown" {
			t.Error("classification", event)
		}
		calls++
		if first == "" {
			first = event.EventID
		}
		if first != event.EventID {
			t.Error("retry ID changed")
		}
		if calls == 1 {
			w.WriteHeader(503)
			return
		}
		if calls == 2 {
			w.Write([]byte(`{"accepted":0,"duplicates":0,"requestId":"incomplete"}`))
			return
		}
		json.NewEncoder(w).Encode(policy.BatchAccepted{Duplicates: 1, RequestID: "already-committed"})
	}))
	defer center.Close()
	a.cfg.ControlURL = center.URL
	if a.FlushEvents(context.Background()) == nil {
		t.Fatal("outage acknowledged")
	}
	if a.FlushEvents(context.Background()) == nil {
		t.Fatal("partial ack accepted")
	}
	if e := a.FlushEvents(context.Background()); e != nil {
		t.Fatal(e)
	}
	stats, _ := a.queue.Stats(context.Background())
	if stats.Depth != 0 {
		t.Fatal("ack not cleared")
	}
	r := httptest.NewRequest("HEAD", "/hello", nil)
	r.Host = a.cfg.Hostname
	a.ServeHTTP(httptest.NewRecorder(), r)
	time.Sleep(80 * time.Millisecond)
	stats, _ = a.queue.Stats(context.Background())
	if stats.Depth != 0 {
		t.Fatal("HEAD counted")
	}
}

// A short real-time outage covers queue growth, failed upload and ambiguous ACK retry.
func TestShortOutageCatchupWithoutDuplicateIDs(t *testing.T) {
	a, _, closeServer := testAgent(t)
	defer closeServer()
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	online := false
	lostAck := true
	seen := map[string]bool{}
	duplicates := 0
	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if !online {
			w.WriteHeader(503)
			return
		}
		var batch policy.EventBatch
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		accepted, duplicate := 0, 0
		for _, event := range batch.Events {
			if seen[event.EventID] {
				duplicate++
				duplicates++
			} else {
				seen[event.EventID] = true
				accepted++
			}
		}
		if lostAck {
			lostAck = false
			w.WriteHeader(503)
			return
		} // committed but response unavailable
		json.NewEncoder(w).Encode(policy.BatchAccepted{Accepted: accepted, Duplicates: duplicate, RequestID: "catchup"})
	}))
	defer center.Close()
	a.cfg.ControlURL = center.URL
	start := time.Now()
	count := 0
	for time.Since(start) < 10*time.Second {
		redirect(t, a, a.cfg.Hostname, "/hello", 302)
		count++
		if count%10 == 0 && a.FlushEvents(context.Background()) == nil {
			t.Fatal("offline upload acknowledged")
		}
		time.Sleep(20 * time.Millisecond)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		stats, err := a.queue.Stats(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if stats.Depth == int64(count) && stats.Buffered == 0 {
			if stats.Dropped != 0 || stats.WriteErrors != 0 {
				t.Fatal("event loss during short outage")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("queue did not persist all outage events")
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	online = true
	mu.Unlock()
	if a.FlushEvents(context.Background()) == nil {
		t.Fatal("lost ACK incorrectly acknowledged")
	}
	for {
		if err := a.FlushEvents(context.Background()); err != nil {
			t.Fatal(err)
		}
		stats, _ := a.queue.Stats(context.Background())
		if stats.Depth == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("catchup did not drain queue")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != count || duplicates == 0 {
		t.Fatalf("unique events=%d want=%d retries=%d", len(seen), count, duplicates)
	}
	t.Logf("outage=10s redirected=%d unique=%d duplicates acknowledged=%d queue drained, dropped=0", count, len(seen), duplicates)
}
