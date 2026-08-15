package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authpkg "github.com/armonic-tech/armonic-backend/internal/auth"
	"github.com/armonic-tech/armonic-backend/internal/models/invite"
	"github.com/stretchr/testify/require"
)

type fakeInviteCreator struct {
	token                     string
	err                       error
	gotServerID, gotCreatedBy string
}

func (f *fakeInviteCreator) Create(_ context.Context, serverID, createdBy string, _ time.Duration) (string, error) {
	f.gotServerID = serverID
	f.gotCreatedBy = createdBy
	return f.token, f.err
}

func TestCreateInvite_OK(t *testing.T) {
	creator := &fakeInviteCreator{token: "tok-123"}

	req := httptest.NewRequest(http.MethodPost, "/server/"+validUUID+"/invite", nil)
	req.SetPathValue("id", validUUID)
	req = req.WithContext(WithUserID(req.Context(), "owner-1"))
	rec := httptest.NewRecorder()

	CreateInvite(creator, "https://armonic.example").ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp InviteCreatedResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "tok-123", resp.InviteToken)
	require.Equal(t, "https://armonic.example?invite=tok-123", resp.URL)

	require.Equal(t, validUUID, creator.gotServerID)
	require.Equal(t, "owner-1", creator.gotCreatedBy)
}

func TestCreateInvite_URLDerivedFromRequest(t *testing.T) {
	cases := []struct {
		name    string
		host    string
		headers map[string]string
		want    string
	}{
		{
			name: "plain request host",
			host: "armonic.example:8080",
			want: "http://armonic.example:8080?invite=tok-123",
		},
		{
			name:    "behind a TLS-terminating proxy",
			host:    "internal:8080",
			headers: map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "chat.example"},
			want:    "https://chat.example?invite=tok-123",
		},
		{
			name:    "multi-hop proxy headers use the client-facing entry",
			host:    "internal:8080",
			headers: map[string]string{"X-Forwarded-Proto": "https, http", "X-Forwarded-Host": "chat.example, internal"},
			want:    "https://chat.example?invite=tok-123",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/server/"+validUUID+"/invite", nil)
			req.Host = tc.host
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			req.SetPathValue("id", validUUID)
			req = req.WithContext(WithUserID(req.Context(), "owner-1"))
			rec := httptest.NewRecorder()

			CreateInvite(&fakeInviteCreator{token: "tok-123"}, "").ServeHTTP(rec, req)

			require.Equal(t, http.StatusCreated, rec.Code)
			var resp InviteCreatedResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			require.Equal(t, tc.want, resp.URL)
		})
	}
}

func TestCreateInvite_NoUserID_Unauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/server/"+validUUID+"/invite", nil)
	req.SetPathValue("id", validUUID)
	rec := httptest.NewRecorder()

	CreateInvite(&fakeInviteCreator{token: "x"}, "https://armonic.example").ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

type fakeInviteRepo struct {
	inv        *invite.Invite
	err        error
	markedUsed string
}

func (f *fakeInviteRepo) Get(context.Context, string) (*invite.Invite, error) {
	return f.inv, f.err
}

func (f *fakeInviteRepo) MarkUsed(_ context.Context, token string) error {
	f.markedUsed = token
	return nil
}

type fakeSignupAuth struct {
	err error
}

func (f fakeSignupAuth) Signup(context.Context, string, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "jwt-token", nil
}

func (f fakeSignupAuth) Validate(context.Context, string) (*authpkg.Claims, error) {
	return &authpkg.Claims{Sub: "new-user"}, nil
}

type fakeMemberAdder struct {
	gotUserID, gotServerID string
}

func (f *fakeMemberAdder) Add(_ context.Context, userID, serverID string) error {
	f.gotUserID, f.gotServerID = userID, serverID
	return nil
}

func validInvite() *invite.Invite {
	return &invite.Invite{
		Token:     "tok-1",
		ServerID:  validUUID,
		CreatedBy: "owner-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

func signupRequest(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/invite/signup", strings.NewReader(body))
}

func TestInviteStatus_OK(t *testing.T) {
	inv := validInvite()
	req := httptest.NewRequest(http.MethodGet, "/invite/status?token=tok-1", nil)
	rec := httptest.NewRecorder()

	InviteStatusHandler(&fakeInviteRepo{inv: inv}).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp InviteStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, validUUID, resp.ServerID)
	require.WithinDuration(t, inv.ExpiresAt, resp.ExpiresAt, time.Second)
}

func TestInviteStatus_UnknownOrUsed_Gone(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/invite/status?token=tok-1", nil)
	rec := httptest.NewRecorder()

	InviteStatusHandler(&fakeInviteRepo{}).ServeHTTP(rec, req)

	require.Equal(t, http.StatusGone, rec.Code)
}

func TestInviteSignup_OK_AddsMembershipAndConsumesInvite(t *testing.T) {
	invites := &fakeInviteRepo{inv: validInvite()}
	members := &fakeMemberAdder{}
	rec := httptest.NewRecorder()

	InviteSignupHandler(invites, fakeSignupAuth{}, members, func() bool { return true }).
		ServeHTTP(rec, signupRequest(`{"token":"tok-1","username":"member","password":"password123"}`))

	require.Equal(t, http.StatusOK, rec.Code)
	var resp InviteSignupResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "jwt-token", resp.Token)
	require.Equal(t, "new-user", members.gotUserID)
	require.Equal(t, validUUID, members.gotServerID)
	require.Equal(t, "tok-1", invites.markedUsed)
}

func TestInviteSignup_ErrorStatuses(t *testing.T) {
	cases := []struct {
		name    string
		invites *fakeInviteRepo
		auth    fakeSignupAuth
		claimed bool
		body    string
		want    int
	}{
		{
			name:    "not claimed yet",
			invites: &fakeInviteRepo{inv: validInvite()},
			body:    `{"token":"tok-1","username":"member","password":"password123"}`,
			want:    http.StatusForbidden,
		},
		{
			name:    "invalid body",
			invites: &fakeInviteRepo{inv: validInvite()},
			claimed: true,
			body:    `not json`,
			want:    http.StatusBadRequest,
		},
		{
			name:    "invite expired or already used",
			invites: &fakeInviteRepo{},
			claimed: true,
			body:    `{"token":"tok-1","username":"member","password":"password123"}`,
			want:    http.StatusGone,
		},
		{
			name:    "username taken",
			invites: &fakeInviteRepo{inv: validInvite()},
			auth:    fakeSignupAuth{err: authpkg.ErrUsernameTaken},
			claimed: true,
			body:    `{"token":"tok-1","username":"member","password":"password123"}`,
			want:    http.StatusConflict,
		},
		{
			name:    "password too short is a client error, not a 500",
			invites: &fakeInviteRepo{inv: validInvite()},
			auth:    fakeSignupAuth{err: authpkg.ErrInvalidInput},
			claimed: true,
			body:    `{"token":"tok-1","username":"member","password":"short"}`,
			want:    http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			InviteSignupHandler(tc.invites, tc.auth, &fakeMemberAdder{}, func() bool { return tc.claimed }).
				ServeHTTP(rec, signupRequest(tc.body))
			require.Equal(t, tc.want, rec.Code)
			require.Empty(t, tc.invites.markedUsed)
		})
	}
}

func TestCreateInvite_RepoError(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/server/"+validUUID+"/invite", nil)
	req.SetPathValue("id", validUUID)
	req = req.WithContext(WithUserID(req.Context(), "owner-1"))
	rec := httptest.NewRecorder()

	CreateInvite(&fakeInviteCreator{err: errors.New("boom")}, "https://armonic.example").ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}
