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

	"github.com/stretchr/testify/require"
)

type fakeInviteCreator struct {
	token                     string
	err                       error
	gotServerID, gotCreatedBy string
	gotBot, gotMultiUse       bool
}

func (f *fakeInviteCreator) Create(_ context.Context, serverID, createdBy string, _ time.Duration) (string, error) {
	f.gotServerID = serverID
	f.gotCreatedBy = createdBy
	return f.token, f.err
}

func (f *fakeInviteCreator) CreateBot(ctx context.Context, serverID, createdBy string, d time.Duration) (string, error) {
	f.gotBot = true
	return f.Create(ctx, serverID, createdBy, d)
}

func (f *fakeInviteCreator) CreateMultiUse(ctx context.Context, serverID, createdBy string, d time.Duration) (string, error) {
	f.gotMultiUse = true
	return f.Create(ctx, serverID, createdBy, d)
}

func TestCreateInvite_MultiUseBody(t *testing.T) {
	creator := &fakeInviteCreator{token: "tok-many"}

	req := httptest.NewRequest(http.MethodPost, "/server/"+validUUID+"/invite", strings.NewReader(`{"multiUse":true}`))
	req.SetPathValue("id", validUUID)
	req = req.WithContext(WithUserID(req.Context(), "owner-1"))
	rec := httptest.NewRecorder()

	CreateInvite(creator, "https://armonic.example").ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	var resp InviteCreatedResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.MultiUse)
	require.True(t, creator.gotMultiUse)
	require.False(t, creator.gotBot)
}

func TestCreateInvite_BotCannotBeMultiUse(t *testing.T) {
	creator := &fakeInviteCreator{token: "x"}
	req := httptest.NewRequest(http.MethodPost, "/server/"+validUUID+"/invite", strings.NewReader(`{"bot":true,"multiUse":true}`))
	req.SetPathValue("id", validUUID)
	req = req.WithContext(WithUserID(req.Context(), "owner-1"))
	rec := httptest.NewRecorder()

	CreateInvite(creator, "https://armonic.example").ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, creator.gotServerID)
}

func TestCreateInvite_BotBody(t *testing.T) {
	creator := &fakeInviteCreator{token: "tok-bot"}

	req := httptest.NewRequest(http.MethodPost, "/server/"+validUUID+"/invite", strings.NewReader(`{"bot":true}`))
	req.SetPathValue("id", validUUID)
	req = req.WithContext(WithUserID(req.Context(), "owner-1"))
	rec := httptest.NewRecorder()

	CreateInvite(creator, "https://armonic.example").ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	var resp InviteCreatedResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Bot)
	require.True(t, creator.gotBot)
}

func TestCreateInvite_BadBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/server/"+validUUID+"/invite", strings.NewReader(`{`))
	req.SetPathValue("id", validUUID)
	req = req.WithContext(WithUserID(req.Context(), "owner-1"))
	rec := httptest.NewRecorder()

	CreateInvite(&fakeInviteCreator{token: "x"}, "https://armonic.example").ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
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

func TestCreateInvite_NoUserID_Unauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/server/"+validUUID+"/invite", nil)
	req.SetPathValue("id", validUUID)
	rec := httptest.NewRecorder()

	CreateInvite(&fakeInviteCreator{token: "x"}, "https://armonic.example").ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCreateInvite_RepoError(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/server/"+validUUID+"/invite", nil)
	req.SetPathValue("id", validUUID)
	req = req.WithContext(WithUserID(req.Context(), "owner-1"))
	rec := httptest.NewRecorder()

	CreateInvite(&fakeInviteCreator{err: errors.New("boom")}, "https://armonic.example").ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func createInviteURL(t *testing.T, publicBaseURL string, edit func(*http.Request)) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/server/"+validUUID+"/invite", nil)
	req.SetPathValue("id", validUUID)
	req = req.WithContext(WithUserID(req.Context(), "owner-1"))
	if edit != nil {
		edit(req)
	}
	rec := httptest.NewRecorder()
	CreateInvite(&fakeInviteCreator{token: "tok"}, publicBaseURL).ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	var resp InviteCreatedResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp.URL
}

func TestCreateInvite_LinkFollowsTheProxy(t *testing.T) {
	url := createInviteURL(t, "", func(r *http.Request) {
		r.Header.Set("X-Forwarded-Proto", "https, http")
		r.Header.Set("X-Forwarded-Host", "api.example.com, internal:8080")
	})
	require.Equal(t, "https://api.example.com?invite=tok", url)
}

func TestCreateInvite_LinkFallsBackToTheConnection(t *testing.T) {
	url := createInviteURL(t, "", func(r *http.Request) { r.Host = "10.0.0.5:8080" })
	require.Equal(t, "http://10.0.0.5:8080?invite=tok", url)
}

func TestCreateInvite_PublicBaseURLWinsOverHeaders(t *testing.T) {
	url := createInviteURL(t, "https://pinned.example", func(r *http.Request) {
		r.Header.Set("X-Forwarded-Host", "spoofed.example")
	})
	require.Equal(t, "https://pinned.example?invite=tok", url)
}
