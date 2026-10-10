package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDecisionAdmissionBoundedWaitCancellationAndRelease(t *testing.T) {
	s := &Server{decisionSlots: make(chan struct{}, 16), decisionWaiting: make(chan struct{}, 16)}
	for i := 0; i < 16; i++ {
		s.decisionSlots <- struct{}{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := httptest.NewRecorder()
	if s.acquireDecision(w, httptest.NewRequest("POST", "/v1/decisions", nil).WithContext(ctx)) || w.Code != 503 || len(s.decisionWaiting) != 0 {
		t.Fatal("canceled admission leaked capacity")
	}
	for i := 0; i < 16; i++ {
		s.decisionWaiting <- struct{}{}
	}
	w = httptest.NewRecorder()
	if s.acquireDecision(w, httptest.NewRequest("POST", "/v1/decisions", nil)) || w.Code != 503 {
		t.Fatal("queue overflow accepted")
	}
	for i := 0; i < 16; i++ {
		<-s.decisionWaiting
	}
	began := time.Now()
	w = httptest.NewRecorder()
	if s.acquireDecision(w, httptest.NewRequest("POST", "/v1/decisions", nil)) || w.Code != 503 || time.Since(began) > time.Second || len(s.decisionWaiting) != 0 {
		t.Fatal("wait was unbounded or leaked capacity")
	}
	done := make(chan bool, 1)
	go func() {
		done <- s.acquireDecision(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/decisions", nil))
	}()
	deadline := time.Now().Add(time.Second)
	for len(s.decisionWaiting) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("request never queued")
		}
		time.Sleep(time.Millisecond)
	}
	<-s.decisionSlots
	select {
	case ok := <-done:
		if !ok || len(s.decisionWaiting) != 0 || len(s.decisionSlots) != 16 {
			t.Fatal("request failed to acquire released capacity")
		}
	case <-time.After(time.Second):
		t.Fatal("admission did not resume")
	}
}
