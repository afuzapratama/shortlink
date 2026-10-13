// Package proxycheck keeps provider credentials and IP checks in the control plane.
package proxycheck

import (
	"context"
	lru "github.com/hashicorp/golang-lru"
	"net/http"
	"net/netip"
	"shortlink.local/policy"
	"sync"
	"time"
)

type Config struct {
	Revision int64
	Enabled  bool
	Key      string
	Provider string
	BaseURL  string
}
type Result struct {
	Flags     *policy.IPQualityFlags `json:"flags,omitempty"`
	Warning   bool                   `json:"warning,omitempty"`
	Status    string                 `json:"status"`
	Detected  bool                   `json:"detected"`
	ExpiresAt time.Time              `json:"expiresAt"`
}
type flight struct {
	done   chan struct{}
	result Result
}
type Client struct {
	mu         sync.Mutex
	load       func(context.Context) (Config, error)
	reserve    func(context.Context, int64) (bool, error)
	http       *http.Client
	config     Config
	checked    time.Time
	epoch      uint64
	cache      *lru.Cache
	flights    map[string]*flight
	slots      chan struct{}
	cooldown   time.Time
	next       time.Time
	lastStatus string
}

func New(load func(context.Context) (Config, error), reserve func(context.Context, int64) (bool, error), client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Transport: publicTransport(), Timeout: 650 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &copyClient
	cache, _ := lru.New(10000)
	return &Client{load: load, reserve: reserve, http: client, cache: cache, flights: map[string]*flight{}, slots: make(chan struct{}, 8)}
}
func (c *Client) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.epoch++
	c.checked = time.Time{}
	c.cache.Purge()
	c.flights = map[string]*flight{}
	c.cooldown = time.Time{}
	c.next = time.Time{}
	c.lastStatus = ""
}
func (c *Client) Status() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastStatus == "" {
		return "not_checked"
	}
	return c.lastStatus
}
func Public(ip netip.Addr) bool {
	return ip.IsValid() && ip.Zone() == "" && ip.IsGlobalUnicast() && !ip.IsPrivate()
}
func unknown(status string) Result {
	return Result{Status: status, ExpiresAt: time.Now().Add(30 * time.Second)}
}
func (c *Client) Lookup(ctx context.Context, ip netip.Addr) Result {
	if !Public(ip) {
		return unknown("invalid_ip")
	}
	ip = ip.Unmap()
	name := ip.String()
	c.mu.Lock()
	if !c.refresh(ctx) {
		c.mu.Unlock()
		return unknown("unavailable")
	}
	cfg, epoch := c.config, c.epoch
	if !cfg.Enabled || cfg.Key == "" {
		c.mu.Unlock()
		return unknown("disabled")
	}
	if value, ok := c.cache.Get(name); ok {
		r := value.(Result)
		if time.Now().Before(r.ExpiresAt) {
			c.mu.Unlock()
			return r
		}
		c.cache.Remove(name)
	}
	if active := c.flights[name]; active != nil {
		c.mu.Unlock()
		select {
		case <-active.done:
			return active.result
		case <-ctx.Done():
			return unknown("unavailable")
		}
	}
	if time.Now().Before(c.cooldown) {
		c.mu.Unlock()
		return unknown("provider_limited")
	}
	if time.Now().Before(c.next) {
		c.mu.Unlock()
		return unknown("busy")
	}
	select {
	case c.slots <- struct{}{}:
	default:
		c.mu.Unlock()
		return unknown("busy")
	}
	c.next = time.Now().Add(100 * time.Millisecond) // At most ten uncached queries/second.
	f := &flight{done: make(chan struct{})}
	c.flights[name] = f
	c.mu.Unlock()
	lookupCtx, cancel := context.WithTimeout(ctx, 650*time.Millisecond)
	defer cancel()
	r := unknown("unavailable")
	if ok, err := c.reserve(lookupCtx, cfg.Revision); err == nil {
		if ok {
			r = c.query(lookupCtx, ip, cfg)
		} else {
			r = unknown("unavailable")
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	<-c.slots
	if epoch == c.epoch {
		c.cache.Add(name, r)
		c.lastStatus = r.Status
		if r.Warning {
			c.lastStatus = "warning"
		}
		if r.Status == "denied" || r.Status == "budget_exhausted" {
			c.cooldown = time.Now().Add(time.Minute)
		}
	}
	f.result = r
	if c.flights[name] == f {
		delete(c.flights, name)
	}
	close(f.done)
	return r
}
func (c *Client) query(ctx context.Context, ip netip.Addr, cfg Config) Result {
	return c.queryBEON(ctx, ip, cfg)
}

func (c *Client) refresh(ctx context.Context) bool {
	if time.Since(c.checked) > 5*time.Second {
		cfg, err := c.load(ctx)
		if err != nil {
			return false
		}
		if cfg.Revision != c.config.Revision {
			c.epoch++
			c.cache.Purge()
			c.flights = map[string]*flight{}
			c.cooldown = time.Time{}
		}
		c.config = cfg
		c.checked = time.Now()
	}
	return true
}
func (c *Client) Enabled(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.refresh(ctx) && c.config.Enabled && c.config.Key != ""
}
