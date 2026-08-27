package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLimiterAllowsBurstThenRejects(t *testing.T) {
	l := New(60, 3)
	l.now = func() time.Time { return time.Unix(0, 0) }

	require.True(t, l.Allow("a"))
	require.True(t, l.Allow("a"))
	require.True(t, l.Allow("a"))
	require.False(t, l.Allow("a"))
}

func TestLimiterKeysAreIndependent(t *testing.T) {
	l := New(60, 1)
	l.now = func() time.Time { return time.Unix(0, 0) }

	require.True(t, l.Allow("a"))
	require.False(t, l.Allow("a"))
	require.True(t, l.Allow("b"))
}

func TestLimiterRefillsOverTime(t *testing.T) {
	l := New(60, 1)
	now := time.Unix(0, 0)
	l.now = func() time.Time { return now }

	require.True(t, l.Allow("a"))
	require.False(t, l.Allow("a"))

	now = now.Add(time.Second)
	require.True(t, l.Allow("a"))
}

func TestLimiterEvictsIdleBuckets(t *testing.T) {
	l := New(60, 1)
	now := time.Unix(0, 0)
	l.now = func() time.Time { return now }

	require.True(t, l.Allow("a"))
	require.Len(t, l.buckets, 1)

	now = now.Add(2 * idleTTL)
	require.True(t, l.Allow("b"))
	require.Len(t, l.buckets, 1)
	require.NotContains(t, l.buckets, "a")
}

func TestMiddlewareRejectsWithRetryAfter(t *testing.T) {
	l := New(60, 1)
	l.now = func() time.Time { return time.Unix(0, 0) }

	var served int
	h := l.Middleware(Global("all"))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		served++
	}))

	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, first.Code)

	second := httptest.NewRecorder()
	h.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusTooManyRequests, second.Code)
	require.Equal(t, "1", second.Header().Get("Retry-After"))
	require.Equal(t, 1, served)
}

func TestNewClampsNonPositiveArguments(t *testing.T) {
	l := New(0, 0)
	require.True(t, l.Allow("a"))
	require.Positive(t, l.RetryAfter())
}
