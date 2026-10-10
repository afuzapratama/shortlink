package risk

import (
	"container/list"
	"net/netip"
	"sync"
	"time"
)

type bucket struct {
	ip     netip.Addr
	tokens float64
	last   time.Time
}
type Limiter struct {
	mu       sync.Mutex
	capacity int
	rate     float64
	burst    float64
	items    map[netip.Addr]*list.Element
	order    *list.List
}

func NewLimiter(rate, burst, capacity int) *Limiter {
	return &Limiter{capacity: capacity, rate: float64(rate), burst: float64(burst), items: map[netip.Addr]*list.Element{}, order: list.New()}
}

// Allow is local to this process/domain. IPv6 groups /64; rotating IPs and restart can reset buckets.
func (l *Limiter) Allow(ip netip.Addr, now time.Time) bool {
	ip = ip.Unmap()
	if ip.Is6() {
		ip = netip.PrefixFrom(ip, 64).Masked().Addr()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	element := l.items[ip]
	if element == nil {
		if len(l.items) >= l.capacity {
			old := l.order.Back()
			delete(l.items, old.Value.(*bucket).ip)
			l.order.Remove(old)
		}
		element = l.order.PushFront(&bucket{ip: ip, tokens: l.burst, last: now})
		l.items[ip] = element
	}
	b := element.Value.(*bucket)
	l.order.MoveToFront(element)
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens = min(l.burst, b.tokens+elapsed*l.rate)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
