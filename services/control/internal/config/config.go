package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL        string
	Listen             string
	Origin             string
	Development        bool
	SessionTTL         time.Duration
	GeoIPRoot          string
	GeoPublisher       bool
	PublicURL          string
	AgentAssetRoot     string
	AgentReleaseInput  string
	PHPAssetRoot       string
	PHPReleaseInput    string
	EventRawDays       int
	EventAggregateDays int
	RiskFeedRoot       string
	RiskTorEnabled     bool
}

func Load() (Config, error) { return Parse(os.Getenv) }
func Parse(get func(string) string) (Config, error) {
	c := Config{DatabaseURL: get("DATABASE_URL"), Listen: get("CONTROL_LISTEN"), Origin: get("CONTROL_ORIGIN"), SessionTTL: 12 * time.Hour}
	c.EventRawDays = 7
	c.EventAggregateDays = 90
	for key, target := range map[string]*int{"EVENT_RAW_DAYS": &c.EventRawDays, "EVENT_AGGREGATE_DAYS": &c.EventAggregateDays} {
		if value := get(key); value != "" {
			n, e := strconv.Atoi(value)
			if e != nil {
				return c, fmt.Errorf("invalid %s", key)
			}
			*target = n
		}
	}
	if c.EventRawDays < 3 || c.EventRawDays > 30 || c.EventAggregateDays < c.EventRawDays || c.EventAggregateDays > 365 {
		return c, fmt.Errorf("event retention must be raw 3–30 days and aggregate raw–365 days")
	}
	c.RiskFeedRoot = get("RISK_FEED_ROOT")
	if v := get("RISK_TOR_ENABLED"); v != "" {
		var e error
		c.RiskTorEnabled, e = strconv.ParseBool(v)
		if e != nil {
			return c, fmt.Errorf("RISK_TOR_ENABLED must be boolean")
		}
	}
	if c.RiskTorEnabled && c.RiskFeedRoot == "" {
		return c, fmt.Errorf("RISK_FEED_ROOT required for Tor refresh")
	}
	c.GeoIPRoot = get("GEOIP_ROOT")
	c.AgentAssetRoot = get("AGENT_ASSET_ROOT")
	c.AgentReleaseInput = get("AGENT_RELEASE_INPUT")
	c.PHPAssetRoot = get("PHP_ASSET_ROOT")
	c.PHPReleaseInput = get("PHP_RELEASE_INPUT")
	if value := get("GEOIP_PUBLISH_ENABLED"); value != "" {
		var err error
		c.GeoPublisher, err = strconv.ParseBool(value)
		if err != nil {
			return c, fmt.Errorf("GEOIP_PUBLISH_ENABLED must be boolean")
		}
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8080"
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return c, fmt.Errorf("CONTROL_LISTEN must be host:port")
	}
	switch get("CONTROL_MODE") {
	case "development":
		c.Development = true
	case "", "production":
	default:
		return c, fmt.Errorf("CONTROL_MODE must be development or production")
	}
	u, err := url.Parse(c.Origin)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Opaque != "" {
		return c, fmt.Errorf("CONTROL_ORIGIN must be an exact origin without path")
	}
	if c.Development {
		host, _, _ := net.SplitHostPort(c.Listen)
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return c, fmt.Errorf("development listener must use a loopback IP")
		}
		originIP := net.ParseIP(u.Hostname())
		if u.Hostname() != "localhost" && (originIP == nil || !originIP.IsLoopback()) {
			return c, fmt.Errorf("development origin must be loopback")
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return c, fmt.Errorf("invalid origin scheme")
		}
	} else if u.Scheme != "https" {
		return c, fmt.Errorf("production requires an HTTPS origin")
	}
	if s := get("SESSION_TTL"); s != "" {
		c.SessionTTL, err = time.ParseDuration(s)
		if err != nil || c.SessionTTL < 5*time.Minute || c.SessionTTL > 24*time.Hour {
			return c, fmt.Errorf("SESSION_TTL must be between 5m and 24h")
		}
	}
	if u.Port() != "" {
		p, e := strconv.Atoi(u.Port())
		if e != nil || p < 1 || p > 65535 {
			return c, fmt.Errorf("invalid origin port")
		}
	}
	c.PublicURL = get("CONTROL_PUBLIC_URL")
	if c.PublicURL == "" {
		c.PublicURL = c.Origin
	}
	public, e := url.Parse(c.PublicURL)
	if e != nil || public.Hostname() == "" || public.User != nil || public.RawQuery != "" || public.Fragment != "" || public.Path != "" || public.Opaque != "" {
		return c, fmt.Errorf("CONTROL_PUBLIC_URL must be an exact origin")
	}
	if public.Port() != "" {
		p, e := strconv.Atoi(public.Port())
		if e != nil || p < 1 || p > 65535 {
			return c, fmt.Errorf("invalid public URL port")
		}
	}
	if public.Scheme != "https" {
		ip := net.ParseIP(public.Hostname())
		if !c.Development || public.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return c, fmt.Errorf("public URL requires HTTPS; development allows loopback HTTP")
		}
	}
	return c, nil
}
