package proxycheck

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCacheCoalescingRotationAndUnknown(t *testing.T) {
	var calls atomic.Int64
	var mu sync.Mutex
	key := "test-key-one"
	revision := int64(1)
	c := New(func(context.Context) (Config, error) {
		mu.Lock()
		defer mu.Unlock()
		return Config{Revision: revision, Enabled: true, Key: key, BaseURL: "https://ipq.example.com"}, nil
	}, func(context.Context, int64) (bool, error) { return true, nil }, &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Host != "ipq.example.com" || r.Header.Get("X-API-Key") == "" || r.URL.RawQuery != "" {
			t.Error("unexpected endpoint")
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		positive := r.Header.Get("X-API-Key") == "test-key-one"
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"ip":"1.1.1.1","proxy":%t,"vpn":false,"tor":false,"botnet":false,"datacenter":false,"spam":false,"malware":false,"attacker":false,"data_version":"generation"}`, positive))), Header: make(http.Header)}, nil
	})})
	ip := netip.MustParseAddr("1.1.1.1")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := c.Lookup(context.Background(), ip)
			if r.Status != "known" || !r.Detected {
				t.Error(r)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicate upstream requests", calls.Load())
	}
	mu.Lock()
	key = "test-key-two"
	revision = 2
	mu.Unlock()
	c.Invalidate()
	if r := c.Lookup(context.Background(), ip); r.Status != "known" || r.Detected {
		t.Fatal("rotation did not invalidate", r)
	}
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
	c.Invalidate()
	c.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("private provider body"))}, nil
	})
	if r := c.Lookup(context.Background(), ip); r.Status != "denied" || r.Detected {
		t.Fatal(r)
	}
	if r := c.Lookup(context.Background(), netip.MustParseAddr("8.8.8.8")); r.Status != "provider_limited" {
		t.Fatal(r)
	}
	if r := c.Lookup(context.Background(), netip.MustParseAddr("127.0.0.1")); r.Status != "invalid_ip" {
		t.Fatal(r)
	}
}
func TestInvalidResponseAndBudgetDoNotBecomeClean(t *testing.T) {
	for _, body := range []string{`{"status":"ok","1.1.1.1":{"detections":{}}}`, `{"status":"ok","1.1.1.1":{"detections":{"anonymous":"false"}}}`, `{"status":"error"}`, strings.Repeat("x", 128*1024+1)} {
		c := New(func(context.Context) (Config, error) {
			return Config{Revision: 1, Enabled: true, Key: "test", BaseURL: "https://ipq.example.com"}, nil
		}, func(context.Context, int64) (bool, error) { return true, nil }, &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})})
		if r := c.Lookup(context.Background(), netip.MustParseAddr("1.1.1.1")); r.Status != "invalid_response" || r.Detected {
			t.Fatal(r)
		}
	}
	c := New(func(context.Context) (Config, error) {
		return Config{Revision: 1, Enabled: true, Key: "test", BaseURL: "https://ipq.example.com"}, nil
	}, func(context.Context, int64) (bool, error) { return false, nil }, nil)
	if r := c.Lookup(context.Background(), netip.MustParseAddr("1.1.1.1")); r.Status != "unavailable" {
		t.Fatal(r)
	}
}

func TestTimeoutAndRedirectAreBoundedAndUnknown(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		var calls atomic.Int64
		c := New(func(context.Context) (Config, error) {
			return Config{Revision: 1, Enabled: true, Key: "test-key", BaseURL: "https://ipq.example.com"}, nil
		}, func(context.Context, int64) (bool, error) { return true, nil }, &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			if redirect {
				return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.example/"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			<-r.Context().Done()
			return nil, r.Context().Err()
		})})
		started := time.Now()
		r := c.Lookup(context.Background(), netip.MustParseAddr("1.1.1.1"))
		if r.Status != "unavailable" || r.Detected || calls.Load() != 1 || time.Since(started) > time.Second {
			t.Fatal(r, calls.Load())
		}
	}
}
