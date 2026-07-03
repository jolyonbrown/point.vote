package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ipLimiter is a per-IP token bucket for room creation (PLAN.md §3:
// 30/hour). In-memory, like everything else here.
type ipLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   float64
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newIPLimiter(perHour int) *ipLimiter {
	return &ipLimiter{
		buckets: make(map[string]*bucket),
		rate:    float64(perHour) / 3600,
		burst:   float64(perHour),
		now:     time.Now,
	}
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	b, ok := l.buckets[ip]
	if !ok {
		l.prune()
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[ip] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// prune drops fully-refilled buckets once the map grows silly, so a scan of
// spoofed source addresses can't grow memory without bound. Called with the
// lock held.
func (l *ipLimiter) prune() {
	if len(l.buckets) < 10_000 {
		return
	}
	now := l.now()
	for ip, b := range l.buckets {
		if min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate) >= l.burst {
			delete(l.buckets, ip)
		}
	}
}

// clientIP prefers the Cloudflare header (the tunnel is the sole ingress in
// production), then X-Forwarded-For, then the socket address.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(first)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
