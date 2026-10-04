package server

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/armonic-tech/armonic-backend/pkg/logger"
)

const maxReasonBytes = 256

type reqScopeKey struct{}

// reqScope is filled in by the auth middleware further down the chain so the
// access log line, written once the handler returns, knows who called.
type reqScope struct {
	userID  string
	authErr error
}

func scopeOf(ctx context.Context) *reqScope {
	s, _ := ctx.Value(reqScopeKey{}).(*reqScope)
	return s
}

func requestLog(next http.Handler, clientKey func(*http.Request) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := requestID(r)
		w.Header().Set("X-Request-ID", id)

		scope := &reqScope{}
		ctx := logger.With(r.Context(), "req_id", id)
		ctx = context.WithValue(ctx, reqScopeKey{}, scope)
		r = r.WithContext(ctx)
		rec := &recorder{ResponseWriter: w}

		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				logger.LogPanic(ctx, r.Method+" "+r.URL.Path, v)
				if !rec.wroteHeader && !rec.hijacked {
					http.Error(rec, "internal error", http.StatusInternalServerError)
				}
			}

			status := rec.status
			if rec.hijacked {
				status = http.StatusSwitchingProtocols
			} else if status == 0 {
				status = http.StatusOK
			}
			args := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"duration_ms", time.Since(start).Milliseconds(),
				"bytes", rec.bytes,
				"client", clientKey(r),
			}
			if scope.userID != "" {
				args = append(args, logger.User(scope.userID))
			}
			if status >= 400 {
				args = append(args, "reason", strings.TrimSpace(rec.reason.String()))
			}
			if scope.authErr != nil {
				args = append(args, "auth_error", scope.authErr.Error())
			}

			level := slog.LevelInfo
			switch {
			case status >= 500:
				level = slog.LevelError
			case status >= 400:
				level = slog.LevelWarn
			}
			slog.Log(ctx, level, "http request", args...)
		}()

		next.ServeHTTP(rec, r)
	})
}

// A client-supplied id is kept so one id can follow a request across a proxy
// and into the client's own logs; anything odd-looking is replaced.
func requestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" && len(id) <= 64 && isToken(id) {
		return id
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func isToken(s string) bool {
	for _, c := range s {
		if !(c == '-' || c == '_' || c == '.' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}

type recorder struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
	hijacked    bool
	reason      strings.Builder
}

func (r *recorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	if r.status >= 400 && r.reason.Len() < maxReasonBytes {
		r.reason.Write(b[:min(len(b), maxReasonBytes-r.reason.Len())])
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijack not supported")
	}
	conn, rw, err := h.Hijack()
	if err == nil {
		r.hijacked = true
	}
	return conn, rw, err
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
