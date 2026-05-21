package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter implements a per-key token bucket rate limiter.
//
// It is intentionally tiny and dependency-free: enough to slow down
// brute-force or scanner traffic against sensitive endpoints
// (e.g. /payment/verify, /admin/login) without pulling in a third-party
// library. For high-volume production use you'd want an LRU-bounded
// bucket map (or Redis) — but for this service the natural turnover of
// client IPs keeps memory bounded in practice, and the janitor below
// trims idle buckets.
type RateLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*bucket
	rate       float64       // tokens added per second
	burst      float64       // max tokens in a bucket
	idleExpiry time.Duration // unused buckets expire after this
	now        func() time.Time
}

type bucket struct {
	tokens   float64
	lastFill time.Time
}

// NewRateLimiter returns a limiter that refills at rate tokens/sec and
// allows bursts of up to burst tokens. Buckets idle for more than
// idleExpiry are discarded by Reap.
func NewRateLimiter(rate, burst float64, idleExpiry time.Duration) *RateLimiter {
	return &RateLimiter{
		buckets:    make(map[string]*bucket),
		rate:       rate,
		burst:      burst,
		idleExpiry: idleExpiry,
		now:        time.Now,
	}
}

// Allow returns true if a request from key may proceed, consuming one token.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &bucket{tokens: l.burst - 1, lastFill: now}
		return true
	}
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.lastFill = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Reap removes buckets idle for longer than idleExpiry.
func (l *RateLimiter) Reap() {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := l.now().Add(-l.idleExpiry)
	for key, b := range l.buckets {
		if b.lastFill.Before(cutoff) {
			delete(l.buckets, key)
		}
	}
}

// Middleware returns an http.Handler that rejects requests exceeding the
// limit with 429 Too Many Requests.
func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := clientKey(r)
		if !l.Allow(key) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientKey returns the best available client identifier for rate limiting.
// X-Forwarded-For wins (we are usually behind a load balancer); otherwise
// the request's RemoteAddr is used, stripped of its port.
func clientKey(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
