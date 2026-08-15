package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/armonic-tech/armonic-backend/internal/models/channel"
	"github.com/armonic-tech/armonic-backend/internal/models/message"
	"github.com/armonic-tech/armonic-backend/internal/models/server"
	"github.com/stretchr/testify/require"
)

// --- fakes for the narrow read interfaces ---

type fakeMemberships struct {
	ids []string
	err error
}

func (f fakeMemberships) GetByUser(context.Context, string) ([]string, error) {
	return f.ids, f.err
}

type fakeServers struct {
	infos []server.ServerInfo
	err   error
}

func (f fakeServers) GetByIDs(context.Context, []string) ([]server.ServerInfo, error) {
	return f.infos, f.err
}

type fakeChannels struct {
	byServer []channel.ChannelInfo
	byID     *channel.ChannelInfo
	err      error
}

func (f fakeChannels) GetChannelByServer(context.Context, string) ([]channel.ChannelInfo, error) {
	return f.byServer, f.err
}

func (f fakeChannels) GetByID(context.Context, string) (*channel.ChannelInfo, error) {
	return f.byID, f.err
}

type fakeMessages struct {
	msgs     []message.Message
	err      error
	gotLimit int // records the limit the handler passed through
}

func (f *fakeMessages) GetByChannel(_ context.Context, _, _ string, limit int) ([]message.Message, error) {
	f.gotLimit = limit
	return f.msgs, f.err
}

const (
	validUUID   = "11111111-1111-1111-1111-111111111111"
	validUUID2  = "22222222-2222-2222-2222-222222222222"
	invalidUUID = "not-a-uuid"
)

// --- GetMyServers ---

func TestGetMyServers_ScopedToCaller(t *testing.T) {
	memberships := fakeMemberships{ids: []string{validUUID, validUUID2}}
	servers := fakeServers{infos: []server.ServerInfo{{ID: validUUID, Name: "A"}, {ID: validUUID2, Name: "B"}}}

	req := httptest.NewRequest(http.MethodGet, "/server", nil)
	req = req.WithContext(WithUserID(req.Context(), "user-1"))
	rec := httptest.NewRecorder()

	GetMyServers(memberships, servers).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var got []server.ServerInfo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 2)
}

func TestGetMyServers_ExposesOwnerID(t *testing.T) {
	memberships := fakeMemberships{ids: []string{validUUID, validUUID2}}
	servers := fakeServers{infos: []server.ServerInfo{
		{ID: validUUID, Name: "A", OwnerID: "user-1"},
		{ID: validUUID2, Name: "B"},
	}}

	req := httptest.NewRequest(http.MethodGet, "/server", nil)
	req = req.WithContext(WithUserID(req.Context(), "user-1"))
	rec := httptest.NewRecorder()

	GetMyServers(memberships, servers).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var got []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "user-1", got[0]["ownerId"])
	require.NotContains(t, got[1], "ownerId")
}

func TestGetMyServers_NoUserID_Unauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/server", nil) // no WithUserID
	rec := httptest.NewRecorder()

	GetMyServers(fakeMemberships{}, fakeServers{}).ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetMyServers_EmptyIsJSONArrayNotNull(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/server", nil)
	req = req.WithContext(WithUserID(req.Context(), "user-1"))
	rec := httptest.NewRecorder()

	GetMyServers(fakeMemberships{ids: nil}, fakeServers{infos: nil}).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `[]`, rec.Body.String())
}

func TestGetMyServers_RepoError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/server", nil)
	req = req.WithContext(WithUserID(req.Context(), "user-1"))
	rec := httptest.NewRecorder()

	GetMyServers(fakeMemberships{err: errors.New("boom")}, fakeServers{}).ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

// --- GetByServer ---

func TestGetByServer_InvalidUUID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/server/"+invalidUUID, nil)
	req.SetPathValue("id", invalidUUID)
	rec := httptest.NewRecorder()

	GetByServer(fakeChannels{}).ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetByServer_EmptyIsNotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/server/"+validUUID, nil)
	req.SetPathValue("id", validUUID)
	rec := httptest.NewRecorder()

	GetByServer(fakeChannels{byServer: nil}).ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetByServer_OK(t *testing.T) {
	channels := fakeChannels{byServer: []channel.ChannelInfo{{ID: validUUID, ServerID: validUUID2, Name: "general", Type: "text"}}}

	req := httptest.NewRequest(http.MethodGet, "/server/"+validUUID2, nil)
	req.SetPathValue("id", validUUID2)
	rec := httptest.NewRecorder()

	GetByServer(channels).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var got []channel.ChannelInfo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 1)
}

// --- GetChannelMessages ---

func TestGetChannelMessages_InvalidUUID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/channel/"+invalidUUID+"/messages", nil)
	req.SetPathValue("id", invalidUUID)
	rec := httptest.NewRecorder()

	GetChannelMessages(fakeChannels{}, &fakeMessages{}).ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetChannelMessages_UnknownChannelIsNotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/channel/"+validUUID+"/messages", nil)
	req.SetPathValue("id", validUUID)
	rec := httptest.NewRecorder()

	GetChannelMessages(fakeChannels{byID: nil}, &fakeMessages{}).ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetChannelMessages_DefaultLimit(t *testing.T) {
	channels := fakeChannels{byID: &channel.ChannelInfo{ID: validUUID, ServerID: validUUID2, Type: "text"}}
	msgs := &fakeMessages{}

	req := httptest.NewRequest(http.MethodGet, "/channel/"+validUUID+"/messages", nil)
	req.SetPathValue("id", validUUID)
	rec := httptest.NewRecorder()

	GetChannelMessages(channels, msgs).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, defaultMessageLimit, msgs.gotLimit)
	require.JSONEq(t, `[]`, rec.Body.String())
}

func TestGetChannelMessages_LimitCapped(t *testing.T) {
	channels := fakeChannels{byID: &channel.ChannelInfo{ID: validUUID, ServerID: validUUID2, Type: "text"}}
	msgs := &fakeMessages{}

	req := httptest.NewRequest(http.MethodGet, "/channel/"+validUUID+"/messages?limit=99999", nil)
	req.SetPathValue("id", validUUID)
	rec := httptest.NewRecorder()

	GetChannelMessages(channels, msgs).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, maxMessageLimit, msgs.gotLimit)
}

func TestGetChannelMessages_BadLimitFallsBackToDefault(t *testing.T) {
	channels := fakeChannels{byID: &channel.ChannelInfo{ID: validUUID, ServerID: validUUID2, Type: "text"}}
	msgs := &fakeMessages{}

	req := httptest.NewRequest(http.MethodGet, "/channel/"+validUUID+"/messages?limit=abc", nil)
	req.SetPathValue("id", validUUID)
	rec := httptest.NewRecorder()

	GetChannelMessages(channels, msgs).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, defaultMessageLimit, msgs.gotLimit)
}
