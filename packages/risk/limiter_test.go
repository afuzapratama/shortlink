package risk

import (
	"net/netip"
	"testing"
	"time"
)

func TestLocalTokenBucketBurstRefillIPv6AndBound(t *testing.T) {
	l := NewLimiter(2, 3, 2)
	now := time.Now()
	ip := netip.MustParseAddr("8.8.8.8")
	for i := 0; i < 3; i++ {
		if !l.Allow(ip, now) {
			t.Fatal("burst")
		}
	}
	if l.Allow(ip, now) {
		t.Fatal("burst exceeded")
	}
	if !l.Allow(ip, now.Add(500*time.Millisecond)) {
		t.Fatal("refill")
	}
	l = NewLimiter(1, 1, 2)
	if !l.Allow(netip.MustParseAddr("2001:4860::1"), now) || l.Allow(netip.MustParseAddr("2001:4860::2"), now) {
		t.Fatal("IPv6 rotation bypass")
	}
	l.Allow(ip, now)
	l.Allow(netip.MustParseAddr("9.9.9.9"), now)
	if len(l.items) > 2 {
		t.Fatal("unbounded IP cache")
	}
}
