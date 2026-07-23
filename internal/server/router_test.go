package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/armonic-tech/armonic-backend/internal/auth"
	"github.com/armonic-tech/armonic-backend/internal/handlers"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret"

type fakeAuthz struct {
	member, memberByChannel, owner bool
	err                            error
}

func (f fakeAuthz) IsMember(context.Context, string, string) (bool, error) {
	return f.member, f.err
}
func (f fakeAuthz) IsMemberByChannel(context.Context, string, string) (bool, error) {
	return f.memberByChannel, f.err
}
func (f fakeAuthz) IsOwner(context.Context, string, string) (bool, error) {
	return f.owner, f.err
}

func mintToken(t *testing.T, userID string) string {
	t.Helper()
	claims := auth.Claims{
		Sub: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	require.NoError(t, err)
	return signed
}

func echoUserID(w http.ResponseWriter, r *http.Request) {
	id, _ := handlers.UserID(r.Context())
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(id))
}

func newTestRouter(fake fakeAuthz) *Router {
	authSvc := auth.NewService(testSecret, nil) // Validate needs only the secret
	r := NewRouter(authSvc, fake, fake)
	r.Member("GET /server/{id}", echoUserID)
	r.MemberByChannel("GET /channel/{id}", echoUserID)
	r.Owner("GET /own/{id}", echoUserID)
	return r
}

func doReq(r *Router, method, target, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.mux.ServeHTTP(rec, req)
	return rec
}

func TestMember_MissingToken_401(t *testing.T) {
	rec := doReq(newTestRouter(fakeAuthz{member: true}), http.MethodGet, "/server/abc", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMember_BadToken_401(t *testing.T) {
	rec := doReq(newTestRouter(fakeAuthz{member: true}), http.MethodGet, "/server/abc", "garbage")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMember_IsMember_200AndUserIDPopulated(t *testing.T) {
	rec := doReq(newTestRouter(fakeAuthz{member: true}), http.MethodGet, "/server/abc", mintToken(t, "user-42"))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "user-42", rec.Body.String()) // requireJWT stashed it, handler read it
}

func TestMember_NotMember_403(t *testing.T) {
	rec := doReq(newTestRouter(fakeAuthz{member: false}), http.MethodGet, "/server/abc", mintToken(t, "user-42"))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestMember_CheckError_500(t *testing.T) {
	rec := doReq(newTestRouter(fakeAuthz{err: errors.New("db down")}), http.MethodGet, "/server/abc", mintToken(t, "user-42"))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestMemberByChannel_Allowed_403(t *testing.T) {
	router := newTestRouter(fakeAuthz{memberByChannel: true})
	require.Equal(t, http.StatusOK, doReq(router, http.MethodGet, "/channel/abc", mintToken(t, "u")).Code)

	router = newTestRouter(fakeAuthz{memberByChannel: false})
	require.Equal(t, http.StatusForbidden, doReq(router, http.MethodGet, "/channel/abc", mintToken(t, "u")).Code)
}

func TestOwner_AllowedAndDenied(t *testing.T) {
	router := newTestRouter(fakeAuthz{owner: true})
	require.Equal(t, http.StatusOK, doReq(router, http.MethodGet, "/own/abc", mintToken(t, "u")).Code)

	router = newTestRouter(fakeAuthz{owner: false})
	require.Equal(t, http.StatusForbidden, doReq(router, http.MethodGet, "/own/abc", mintToken(t, "u")).Code)
}
