package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/armonic-tech/armonic-backend/internal/models/mls"
	repo "github.com/armonic-tech/armonic-backend/internal/repositories"
	"github.com/stretchr/testify/require"
)

type fakeMLS struct {
	devices   map[string]string // device -> user
	packages  map[string][]mls.KeyPackage
	state     mls.GroupState
	adds      []mls.PendingAdd
	removes   []mls.Member
	welcome   *mls.Welcome
	commits   []mls.CommitRequest
	commitErr error
}

func newFakeMLS() *fakeMLS {
	return &fakeMLS{devices: map[string]string{}, packages: map[string][]mls.KeyPackage{}}
}

func (f *fakeMLS) RegisterDevice(_ context.Context, id, userID, _ string) error {
	if owner, ok := f.devices[id]; ok && owner != userID {
		return repo.ErrDeviceTaken
	}
	f.devices[id] = userID
	return nil
}
func (f *fakeMLS) GetDevice(_ context.Context, id string) (*mls.Device, error) {
	owner, ok := f.devices[id]
	if !ok {
		return nil, nil
	}
	return &mls.Device{ID: id, UserID: owner}, nil
}
func (f *fakeMLS) AddKeyPackages(_ context.Context, deviceID, _ string, kps []mls.KeyPackage) error {
	f.packages[deviceID] = append(f.packages[deviceID], kps...)
	return nil
}
func (f *fakeMLS) CountKeyPackages(_ context.Context, deviceID string) (int, error) {
	return len(f.packages[deviceID]), nil
}
func (f *fakeMLS) GroupState(context.Context, string) (*mls.GroupState, error) {
	st := f.state
	return &st, nil
}
func (f *fakeMLS) PendingAdds(context.Context, string) ([]mls.PendingAdd, error) { return f.adds, nil }
func (f *fakeMLS) PendingRemoves(context.Context, string) ([]mls.Member, error) {
	return f.removes, nil
}
func (f *fakeMLS) Commit(_ context.Context, _ string, req mls.CommitRequest) (int64, error) {
	if f.commitErr != nil {
		return 0, f.commitErr
	}
	f.commits = append(f.commits, req)
	f.state.Epoch++
	return f.state.Epoch, nil
}
func (f *fakeMLS) CommitsAfter(context.Context, string, int64, int) ([]mls.Commit, error) {
	return []mls.Commit{}, nil
}
func (f *fakeMLS) Welcome(context.Context, string, string) (*mls.Welcome, error) {
	return f.welcome, nil
}
func (f *fakeMLS) ConsumeWelcome(context.Context, string, string) error { return nil }

type fakeMLSPresence struct {
	mu        sync.Mutex
	online    []string
	broadcast []map[string]any
}

func (f *fakeMLSPresence) BroadcastToServer(_ string, msg any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broadcast = append(f.broadcast, msg.(map[string]any))
}
func (f *fakeMLSPresence) ConnectedUserIDs(string) []string { return f.online }

type fakeOwners struct{ owner string }

func (f fakeOwners) OwnerID(context.Context, string) (string, error) { return f.owner, nil }

type fakeMLSMemberships struct{ servers []string }

func (f fakeMLSMemberships) GetByUser(context.Context, string) ([]string, error) {
	return f.servers, nil
}

func jsonReq(t *testing.T, method, path, userID string, body any) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.SetPathValue("id", "s1")
	return r.WithContext(WithUserID(r.Context(), userID))
}

func TestRegisterDeviceIsIdempotentButNotTransferable(t *testing.T) {
	store := newFakeMLS()
	h := NewMLS(store, fakeMLSMemberships{}, fakeOwners{}, &fakeMLSPresence{})

	w := httptest.NewRecorder()
	h.RegisterDevice()(w, jsonReq(t, http.MethodPost, "/me/mls/device", "alice", RegisterDeviceRequest{DeviceID: "dev-a", SigningKey: "pk"}))
	require.Equal(t, http.StatusNoContent, w.Code)

	w = httptest.NewRecorder()
	h.RegisterDevice()(w, jsonReq(t, http.MethodPost, "/me/mls/device", "alice", RegisterDeviceRequest{DeviceID: "dev-a", SigningKey: "pk2"}))
	require.Equal(t, http.StatusNoContent, w.Code)

	w = httptest.NewRecorder()
	h.RegisterDevice()(w, jsonReq(t, http.MethodPost, "/me/mls/device", "mallory", RegisterDeviceRequest{DeviceID: "dev-a", SigningKey: "pk"}))
	require.Equal(t, http.StatusConflict, w.Code)

	w = httptest.NewRecorder()
	h.RegisterDevice()(w, jsonReq(t, http.MethodPost, "/me/mls/device", "alice", RegisterDeviceRequest{DeviceID: "not valid!", SigningKey: "pk"}))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPublishKeyPackagesRequiresOwnDeviceAndNudgesServers(t *testing.T) {
	store := newFakeMLS()
	store.devices["dev-a"] = "alice"
	presence := &fakeMLSPresence{}
	h := NewMLS(store, fakeMLSMemberships{servers: []string{"s1", "s2"}}, fakeOwners{}, presence)

	body := PublishKeyPackagesRequest{DeviceID: "dev-a", KeyPackages: []mls.KeyPackage{{ID: "kp1", Payload: "x"}}}
	w := httptest.NewRecorder()
	h.PublishKeyPackages()(w, jsonReq(t, http.MethodPost, "/me/mls/keypackages", "bob", body))
	require.Equal(t, http.StatusForbidden, w.Code)

	w = httptest.NewRecorder()
	h.PublishKeyPackages()(w, jsonReq(t, http.MethodPost, "/me/mls/keypackages", "alice", body))
	require.Equal(t, http.StatusNoContent, w.Code)
	require.Len(t, store.packages["dev-a"], 1)
	require.Len(t, presence.broadcast, 2)
	require.Equal(t, "mls-sync", presence.broadcast[0]["type"])
}

func TestStatePicksTheOwnerAsCoordinatorWhenOnline(t *testing.T) {
	store := newFakeMLS()
	store.devices["dev-a"] = "alice"
	store.state = mls.GroupState{Epoch: 3, Members: []mls.Member{{DeviceID: "dev-a", UserID: "alice"}, {DeviceID: "dev-b", UserID: "bob"}}}
	presence := &fakeMLSPresence{online: []string{"bob", "alice", "carol"}}
	h := NewMLS(store, fakeMLSMemberships{}, fakeOwners{owner: "bob"}, presence)

	r := httptest.NewRequest(http.MethodGet, "/server/s1/mls?deviceId=dev-a", nil)
	r.SetPathValue("id", "s1")
	w := httptest.NewRecorder()
	h.State()(w, r.WithContext(WithUserID(r.Context(), "alice")))
	require.Equal(t, http.StatusOK, w.Code)
	var got GroupStateResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "bob", got.CoordinatorUserID)
	require.Equal(t, int64(3), got.Epoch)

	// Owner offline: the lowest online *group member* — carol is online but
	// not in the group, so she cannot build a commit.
	presence.online = []string{"carol", "alice"}
	w = httptest.NewRecorder()
	h.State()(w, r.WithContext(WithUserID(r.Context(), "alice")))
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "alice", got.CoordinatorUserID)
}

func TestCommitBroadcastsAndMapsRepoErrors(t *testing.T) {
	store := newFakeMLS()
	store.devices["dev-a"] = "alice"
	presence := &fakeMLSPresence{}
	h := NewMLS(store, fakeMLSMemberships{}, fakeOwners{}, presence)

	req := mls.CommitRequest{DeviceID: "dev-a", Epoch: 0, Payload: "opaque"}
	w := httptest.NewRecorder()
	h.Commit()(w, jsonReq(t, http.MethodPost, "/server/s1/mls/commit", "alice", req))
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, presence.broadcast, 1)
	require.Equal(t, "mls-commit", presence.broadcast[0]["type"])
	require.Equal(t, "opaque", presence.broadcast[0]["payload"])

	store.commitErr = repo.ErrStaleEpoch
	w = httptest.NewRecorder()
	h.Commit()(w, jsonReq(t, http.MethodPost, "/server/s1/mls/commit", "alice", req))
	require.Equal(t, http.StatusConflict, w.Code)

	store.commitErr = repo.ErrInvalidAdd
	w = httptest.NewRecorder()
	h.Commit()(w, jsonReq(t, http.MethodPost, "/server/s1/mls/commit", "alice", req))
	require.Equal(t, http.StatusBadRequest, w.Code)

	// A device the caller does not own cannot commit on their behalf.
	w = httptest.NewRecorder()
	h.Commit()(w, jsonReq(t, http.MethodPost, "/server/s1/mls/commit", "bob", req))
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestOversizedMlsBodyIsRejectedNotPanicking(t *testing.T) {
	store := newFakeMLS()
	store.devices["dev-a"] = "alice"
	h := NewMLS(store, fakeMLSMemberships{}, fakeOwners{}, &fakeMLSPresence{})

	// A body past mlsBodyCap: MaxBytesReader is handed a nil ResponseWriter
	// (the handler answers by itself), which must fail the decode rather
	// than panic.
	huge := bytes.NewReader(bytes.Repeat([]byte("x"), mlsBodyCap+1024))
	r := httptest.NewRequest(http.MethodPost, "/server/s1/mls/commit", huge)
	r.SetPathValue("id", "s1")
	w := httptest.NewRecorder()
	h.Commit()(w, r.WithContext(WithUserID(r.Context(), "alice")))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, store.commits)
}
