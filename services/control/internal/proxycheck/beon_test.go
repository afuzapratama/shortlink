package proxycheck

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

func TestBEONContractFlagsAndFailures(t *testing.T) {
	for _, test := range []struct {
		name, body string
		code       int
		status     string
		detected   bool
	}{
		{"clean", `{"ip":"1.1.1.1","proxy":false,"vpn":false,"tor":false,"botnet":false,"datacenter":true,"risky":true,"score":90,"spam":false,"malware":false,"attacker":false,"data_version":"generation"}`, 200, "known", false},
		{"proxy", `{"ip":"1.1.1.1","proxy":true,"vpn":false,"tor":false,"botnet":false,"spam":false,"malware":false,"attacker":false,"datacenter":false,"data_version":"generation"}`, 200, "known", true},
		{"vpn", `{"ip":"1.1.1.1","proxy":false,"vpn":true,"tor":false,"botnet":false,"spam":false,"malware":false,"attacker":false,"datacenter":false,"data_version":"generation"}`, 200, "known", true},
		{"tor", `{"ip":"1.1.1.1","proxy":false,"vpn":false,"tor":true,"botnet":false,"spam":false,"malware":false,"attacker":false,"datacenter":false,"data_version":"generation"}`, 200, "known", true},
		{"botnet", `{"ip":"1.1.1.1","proxy":false,"vpn":false,"tor":false,"botnet":true,"spam":false,"malware":false,"attacker":false,"datacenter":false,"data_version":"generation"}`, 200, "known", true},
		{"missing", `{"ip":"1.1.1.1","proxy":false}`, 200, "invalid_response", false},
		{"wrong-ip", `{"ip":"8.8.8.8","proxy":true,"vpn":false,"tor":false,"botnet":false,"spam":false,"malware":false,"attacker":false,"datacenter":false,"data_version":"generation"}`, 200, "invalid_response", false},
		{"stale", `{"error":"stale"}`, 503, "unavailable", false},
		{"quota", `{}`, 429, "denied", false},
		{"redirect", `{}`, 302, "unavailable", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := New(func(context.Context) (Config, error) {
				return Config{Revision: 1, Enabled: true, Key: "private-test", Provider: "beon", BaseURL: "https://ipq.example.com"}, nil
			}, func(context.Context, int64) (bool, error) { return true, nil }, &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://ipq.example.com/api/v1/check/1.1.1.1" || r.Header.Get("X-API-Key") != "private-test" || r.URL.RawQuery != "" {
					t.Fatal("wrong request contract")
				}
				return &http.Response{StatusCode: test.code, Body: io.NopCloser(strings.NewReader(test.body)), Header: http.Header{"Location": []string{"https://evil.example/"}}}, nil
			})})
			result := c.Lookup(context.Background(), netip.MustParseAddr("1.1.1.1"))
			if result.Status != test.status || result.Detected != test.detected {
				t.Fatal(result)
			}
		})
	}
}
func TestBEONOriginValidation(t *testing.T) {
	for _, raw := range []string{"http://ipq.example.com", "https://user:key@ipq.example.com", "https://ipq.example.com/api/v1", "https://ipq.example.com?key=x", "https://ipq.example.com#x", "https://localhost", "https://127.0.0.1", "https://10.0.0.1", "https://[::1]"} {
		if _, ok := ValidateBaseURL(raw); ok {
			t.Fatalf("accepted %s", raw)
		}
	}
	if v, ok := ValidateBaseURL("https://ipq.example.com/"); !ok || v != "https://ipq.example.com" {
		t.Fatal(v, ok)
	}
	_, err := publicTransport().DialContext(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil {
		t.Fatal("private dial accepted")
	}
}
func TestBEONRecipientChangeInvalidatesCache(t *testing.T) {
	revision := int64(1)
	host := "https://first.example.com"
	calls := 0
	c := New(func(context.Context) (Config, error) {
		return Config{Revision: revision, Enabled: true, Key: "test", Provider: "beon", BaseURL: host}, nil
	}, func(context.Context, int64) (bool, error) { return true, nil }, &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		positive := strings.Contains(r.URL.Host, "first")
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"ip":"1.1.1.1","proxy":%t,"vpn":false,"tor":false,"botnet":false,"spam":false,"malware":false,"attacker":false,"datacenter":false,"data_version":"generation"}`, positive)))}, nil
	})})
	ip := netip.MustParseAddr("1.1.1.1")
	if !c.Lookup(context.Background(), ip).Detected {
		t.Fatal("initial lookup")
	}
	revision++
	host = "https://second.example.com"
	c.Invalidate()
	if c.Lookup(context.Background(), ip).Detected || calls != 2 {
		t.Fatal("old recipient result reused")
	}
}
