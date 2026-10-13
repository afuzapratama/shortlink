package proxycheck

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"shortlink.local/policy"
	"strings"
	"time"
)

// Only an HTTPS origin is accepted. Paths and credentials cannot receive the key.
func ValidateBaseURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Opaque != "" || len(raw) > 2048 {
		return "", false
	}
	h := strings.ToLower(u.Hostname())
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || (!strings.Contains(h, ".") && !strings.Contains(h, ":")) {
		return "", false
	}
	if ip, e := netip.ParseAddr(h); e == nil && !Public(ip) {
		return "", false
	}
	u.Path = ""
	u.RawPath = ""
	return u.String(), true
}

// Validate the addresses actually dialed, including DNS changes after saving settings.
func publicTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(addresses) == 0 {
			return nil, &net.AddrError{Err: "empty provider address"}
		}
		for _, ip := range addresses {
			if !Public(ip.Unmap()) {
				return nil, &net.AddrError{Err: "non-public provider address"}
			}
		}
		var last error
		d := net.Dialer{}
		for _, ip := range addresses {
			c, e := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return c, nil
			}
			last = e
		}
		return nil, last
	}
	return t
}

func (c *Client) queryBEON(ctx context.Context, ip netip.Addr, cfg Config) Result {
	base, ok := ValidateBaseURL(cfg.BaseURL)
	if !ok {
		return unknown("unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/check/"+ip.String(), nil)
	if err != nil {
		return unknown("unavailable")
	}
	req.Header.Set("X-API-Key", cfg.Key)
	req.Header.Set("Accept", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return unknown("unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 || response.StatusCode == 429 {
		return unknown("denied")
	}
	if response.StatusCode != 200 {
		return unknown("unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 128*1024+1))
	if err != nil || len(raw) > 128*1024 {
		return unknown("invalid_response")
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil {
		return unknown("invalid_response")
	}
	var flags policy.IPQualityFlags
	if json.Unmarshal(raw, &flags) != nil {
		return unknown("invalid_response")
	}
	for name := range flags.Values() {
		var value *bool
		if json.Unmarshal(envelope[name], &value) != nil || value == nil {
			return unknown("invalid_response")
		}
	}
	var v struct {
		IP          string `json:"ip"`
		DataVersion string `json:"data_version"`
	}
	if json.Unmarshal(raw, &v) != nil || v.DataVersion == "" {
		return unknown("invalid_response")
	}
	returned, err := netip.ParseAddr(v.IP)
	if err != nil || returned.Unmap() != ip.Unmap() {
		return unknown("invalid_response")
	}
	// Legacy detected retains its old meaning; new links select from Flags.
	return Result{Status: "known", Flags: &flags, Detected: flags.Proxy || flags.VPN || flags.Tor || flags.Botnet, ExpiresAt: time.Now().Add(time.Minute)}
}
