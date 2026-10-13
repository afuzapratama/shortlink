package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"shortlink.local/policy"
	"sync"
	"time"
)

type proxyResult struct {
	Flags     *policy.IPQualityFlags `json:"flags,omitempty"`
	Status    string                 `json:"status"`
	Detected  bool                   `json:"detected"`
	ExpiresAt time.Time              `json:"expiresAt"`
}
type proxyFlight struct {
	done   chan struct{}
	result proxyResult
}
type proxyCache struct {
	mu      sync.Mutex
	entries map[string]proxyResult
	flights map[string]*proxyFlight
}

func (a *Agent) lookupProxy(parent context.Context, ip netip.Addr) proxyResult {
	key := ip.Unmap().String()
	c := &a.proxy
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]proxyResult{}
		c.flights = map[string]*proxyFlight{}
	}
	if cached, ok := c.entries[key]; ok && time.Now().Before(cached.ExpiresAt) {
		c.mu.Unlock()
		return cached
	}
	if active := c.flights[key]; active != nil {
		c.mu.Unlock()
		select {
		case <-active.done:
			return active.result
		case <-parent.Done():
			return proxyResult{Status: "unavailable"}
		}
	}
	if len(c.flights) >= 8 {
		c.mu.Unlock()
		return proxyResult{Status: "busy"}
	}
	f := &proxyFlight{done: make(chan struct{})}
	c.flights[key] = f
	c.mu.Unlock()
	result := proxyResult{Status: "unavailable", ExpiresAt: time.Now().Add(10 * time.Second)}
	ctx, cancel := context.WithTimeout(parent, 750*time.Millisecond)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"ip": key})
	response, err := a.request(ctx, http.MethodPost, "/v1/agents/proxycheck", bytes.NewReader(body))
	if err == nil {
		defer response.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(response.Body, 4097))
		var checked proxyResult
		if e == nil && len(raw) <= 4096 && response.StatusCode == 200 && json.Unmarshal(raw, &checked) == nil && !checked.ExpiresAt.IsZero() {
			switch checked.Status {
			case "known", "disabled", "busy", "unavailable", "invalid_ip", "provider_limited", "denied", "budget_exhausted", "invalid_response":
				if checked.Status == "known" && checked.Flags == nil {
					checked.Status = "invalid_response"
				}
				if checked.Status != "known" {
					checked.Detected = false
				}
				if checked.ExpiresAt.After(time.Now().Add(time.Minute)) {
					checked.ExpiresAt = time.Now().Add(time.Minute)
				}
				if checked.Status != "known" && checked.ExpiresAt.After(time.Now().Add(10*time.Second)) {
					checked.ExpiresAt = time.Now().Add(10 * time.Second)
				}
				result = checked
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= 10000 {
		c.entries = map[string]proxyResult{}
	}
	c.entries[key] = result
	f.result = result
	delete(c.flights, key)
	close(f.done)
	return result
}
