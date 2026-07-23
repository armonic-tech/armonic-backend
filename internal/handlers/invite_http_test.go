package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
