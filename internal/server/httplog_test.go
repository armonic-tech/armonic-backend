package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/armonic-tech/armonic-backend/pkg/logger"
	"github.com/stretchr/testify/require"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(logger.NewHandler(&buf, slog.LevelDebug)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var m map[string]any
		require.NoError(t, json.Unmarshal(line, &m))
		out = append(out, m)
	}
	return out
}

func fixedClient(*http.Request) string { return "203.0.113.7" }

func TestRequestLogRecordsRejectionReasonAndCaller(t *testing.T) {
	buf := captureLog(t)
	h := requestLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scopeOf(r.Context()).userID = "u1"
		http.Error(w, "forbidden", http.StatusForbidden)
	}), fixedClient)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/server/abc?token=secret", nil))

	recs := records(t, buf)
	last := recs[len(recs)-1]
	require.Equal(t, "http request", last["msg"])
	require.Equal(t, "WARN", last["level"])
	require.Equal(t, float64(403), last["status"])
	require.Equal(t, "forbidden", last["reason"])
	require.Equal(t, "u1", last["user_id"])
	require.Equal(t, "/server/abc", last["path"], "the query string can carry tokens and must not be logged")
	require.NotEmpty(t, last["req_id"])
	require.Equal(t, last["req_id"], rec.Header().Get("X-Request-ID"))
}

func TestRequestLogPropagatesRequestIDToHandlerLogs(t *testing.T) {
	buf := captureLog(t)
	h := requestLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.InfoContext(r.Context(), "inside")
	}), fixedClient)

	req := httptest.NewRequest(http.MethodGet, "/info", nil)
	req.Header.Set("X-Request-ID", "client-abc-123")
	h.ServeHTTP(httptest.NewRecorder(), req)

	recs := records(t, buf)
	require.Equal(t, "inside", recs[0]["msg"])
	require.Equal(t, "client-abc-123", recs[0]["req_id"])
	require.Equal(t, "INFO", recs[1]["level"])
}

func TestRequestLogReplacesSuspiciousRequestID(t *testing.T) {
	captureLog(t)
	h := requestLog(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), fixedClient)
	req := httptest.NewRequest(http.MethodGet, "/info", nil)
	req.Header.Set("X-Request-ID", "bad id\nforged")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.NotEqual(t, "bad id\nforged", rec.Header().Get("X-Request-ID"))
	require.Len(t, rec.Header().Get("X-Request-ID"), 16)
}

func TestRequestLogRecoversPanicAs500(t *testing.T) {
	buf := captureLog(t)
	h := requestLog(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	}), fixedClient)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/auth/login", nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	recs := records(t, buf)
	require.Equal(t, "panic recovered", recs[0]["msg"])
	require.Equal(t, "kaboom", recs[0]["panic"])
	require.NotEmpty(t, recs[0]["stack"])
	require.Equal(t, "ERROR", recs[1]["level"])
	require.Equal(t, float64(500), recs[1]["status"])
}
