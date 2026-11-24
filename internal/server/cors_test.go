package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCORSPreflightAllowsEveryRouteMethod(t *testing.T) {
	const origin = "https://app.example.com"
	h := withCORS([]string{origin}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		req := httptest.NewRequest(http.MethodOptions, "/server/s1/logo", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", method)
		req.Header.Set("Access-Control-Request-Headers", "authorization")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, origin, rec.Header().Get("Access-Control-Allow-Origin"), method)
	}

	req := httptest.NewRequest(http.MethodOptions, "/server/s1/logo", nil)
	req.Header.Set("Origin", "https://elsewhere.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodDelete)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"), "an unlisted origin must stay refused")
}
