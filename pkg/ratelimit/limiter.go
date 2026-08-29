package ratelimit

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const idleTTL = 10 * time.Minute

type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	limit   rate.Limit
	burst   int
	swept   time.Time
	now     func() time.Time
}

type bucket struct {
	lim  *rate.Limiter
	seen time.Time
}

func New(perMinute, burst int) *Limiter {
	if perMinute < 1 {
		perMinute = 1
	}
	if burst < 1 {
		burst = 1
	}
	return &Limiter{
		buckets: make(map[string]*bucket),
		limit:   rate.Limit(float64(perMinute) / 60.0),
		burst:   burst,
		now:     time.Now,
	}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweep(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{lim: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[key] = b
	}
	b.seen = now
	return b.lim.AllowN(now, 1)
}

func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.swept) < idleTTL {
		return
	}
	l.swept = now
	for k, b := range l.buckets {
		if now.Sub(b.seen) >= idleTTL {
			delete(l.buckets, k)
		}
	}
}

func (l *Limiter) RetryAfter() int {
	if l.limit <= 0 {
		return 60
	}
	return max(1, int(math.Ceil(1/float64(l.limit))))
}

func (l *Limiter) Middleware(key func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.Allow(key(r)) {
				Reject(w, l)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func Reject(w http.ResponseWriter, l *Limiter) {
	w.Header().Set("Retry-After", strconv.Itoa(l.RetryAfter()))
	http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
}

func Global(key string) func(*http.Request) string {
	return func(*http.Request) string { return key }
}
