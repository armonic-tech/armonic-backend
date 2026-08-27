package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/armonic-tech/armonic-backend/pkg/pow"
	"github.com/armonic-tech/armonic-backend/pkg/ratelimit"
	"github.com/stretchr/testify/require"
)

type stubAuth struct{ token string }

func (s stubAuth) Signup(context.Context, string, string) (string, error) { return s.token, nil }
func (s stubAuth) Login(context.Context, string, string) (string, error)  { return s.token, nil }

func solveChallenge(t *testing.T, c pow.Challenge) string {
	t.Helper()
	for n := int64(0); n <= c.MaxNumber; n++ {
		sum := sha256.Sum256([]byte(c.Salt + strconv.FormatInt(n, 10)))
		if hex.EncodeToString(sum[:]) == c.Challenge {
			raw, err := json.Marshal(map[string]any{
				"algorithm": c.Algorithm, "challenge": c.Challenge,
				"number": n, "salt": c.Salt, "signature": c.Signature,
			})
			require.NoError(t, err)
			return base64.StdEncoding.EncodeToString(raw)
		}
	}
	t.Fatal("challenge had no solution in range")
	return ""
}

func postLogin(t *testing.T, h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(body))
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func TestPowChallengeIsServedWhenEnabled(t *testing.T) {
	mgr := pow.New(true, "secret", 500, time.Minute)

	w := httptest.NewRecorder()
	PowChallenge(mgr)(w, httptest.NewRequest(http.MethodGet, "/pow/challenge", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	var c pow.Challenge
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &c))
	require.Equal(t, pow.Algorithm, c.Algorithm)
	require.NotEmpty(t, c.Challenge)
	require.NotEmpty(t, c.Signature)
	require.Equal(t, int64(500), c.MaxNumber)
}

func TestPowChallenge404sWhenDisabled(t *testing.T) {
	mgr := pow.New(false, "secret", 500, time.Minute)

	w := httptest.NewRecorder()
	PowChallenge(mgr)(w, httptest.NewRequest(http.MethodGet, "/pow/challenge", nil))
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestLoginRequiresPowWhenEnabled(t *testing.T) {
	mgr := pow.New(true, "secret", 500, time.Minute)
	h := LoginHandler(stubAuth{token: "jwt"}, func() bool { return true }, ratelimit.New(60, 10), mgr)

	missing := postLogin(t, h, `{"username":"ada","password":"hunter2hunter2"}`)
	require.Equal(t, http.StatusBadRequest, missing.Code)

	garbage := postLogin(t, h, `{"username":"ada","password":"hunter2hunter2","altcha":"nope"}`)
	require.Equal(t, http.StatusBadRequest, garbage.Code)

	c, err := mgr.Issue()
	require.NoError(t, err)
	body, err := json.Marshal(map[string]string{
		"username": "ada", "password": "hunter2hunter2", "altcha": solveChallenge(t, c),
	})
	require.NoError(t, err)

	ok := postLogin(t, h, string(body))
	require.Equal(t, http.StatusOK, ok.Code)

	// The same solution must not open a second login.
	replay := postLogin(t, h, string(body))
	require.Equal(t, http.StatusConflict, replay.Code)
}

func TestLoginSkipsPowWhenDisabled(t *testing.T) {
	mgr := pow.New(false, "secret", 500, time.Minute)
	h := LoginHandler(stubAuth{token: "jwt"}, func() bool { return true }, ratelimit.New(60, 10), mgr)

	w := postLogin(t, h, `{"username":"ada","password":"hunter2hunter2"}`)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestPowIsCheckedBeforeTheRateLimitBurns(t *testing.T) {
	mgr := pow.New(true, "secret", 500, time.Minute)
	limiter := ratelimit.New(60, 1)
	h := LoginHandler(stubAuth{token: "jwt"}, func() bool { return true }, limiter, mgr)

	for range 5 {
		require.Equal(t, http.StatusBadRequest,
			postLogin(t, h, `{"username":"ada","password":"x","altcha":"bad"}`).Code)
	}

	c, err := mgr.Issue()
	require.NoError(t, err)
	body, err := json.Marshal(map[string]string{
		"username": "ada", "password": "hunter2hunter2", "altcha": solveChallenge(t, c),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, postLogin(t, h, string(body)).Code)
}
