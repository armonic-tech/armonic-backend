package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"

	"github.com/armonic-tech/armonic-backend/internal/models/mls"
	repo "github.com/armonic-tech/armonic-backend/internal/repositories"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
)

const (
	maxKeyPackagesPerPost = 20
	maxKeyPackageBytes    = 8 << 10
	maxCommitBytes        = 1 << 20
	maxWelcomeBytes       = 1 << 20
	maxCommitsPerPage     = 200
	mlsBodyCap            = 8 << 20
)

type MLSStore interface {
	RegisterDevice(ctx context.Context, id, userID, signingKey string) error
	GetDevice(ctx context.Context, id string) (*mls.Device, error)
	AddKeyPackages(ctx context.Context, deviceID, userID string, packages []mls.KeyPackage) error
	CountKeyPackages(ctx context.Context, deviceID string) (int, error)
	GroupState(ctx context.Context, serverID string) (*mls.GroupState, error)
	PendingAdds(ctx context.Context, serverID string) ([]mls.PendingAdd, error)
	PendingRemoves(ctx context.Context, serverID string) ([]mls.Member, error)
	Commit(ctx context.Context, serverID string, req mls.CommitRequest) (int64, error)
	CommitsAfter(ctx context.Context, serverID string, epoch int64, limit int) ([]mls.Commit, error)
	Welcome(ctx context.Context, serverID, deviceID string) (*mls.Welcome, error)
	ConsumeWelcome(ctx context.Context, id, deviceID string) error
}

type OwnerLookup interface {
	OwnerID(ctx context.Context, serverID string) (string, error)
}

// Broadcaster pushes a frame to every live socket of a server; the WS side
// of the delivery service.
type Broadcaster interface {
	BroadcastToServer(serverID string, msg any)
	ConnectedUserIDs(serverID string) []string
}

type MLS struct {
	store       MLSStore
	memberships MembershipLister
	owners      OwnerLookup
	presence    Broadcaster
}

func NewMLS(store MLSStore, memberships MembershipLister, owners OwnerLookup, presence Broadcaster) *MLS {
	return &MLS{store: store, memberships: memberships, owners: owners, presence: presence}
}

type RegisterDeviceRequest struct {
	DeviceID   string `json:"deviceId"`
	SigningKey string `json:"signingKey"`
}

// @Summary      Register an MLS device
// @Description  Declares this install's device id and Ed25519 signing key. Idempotent for the same user; a device id owned by another account is rejected.
// @Tags         MLS
// @Accept       json
// @Param        body body RegisterDeviceRequest true "Device"
// @Success      204
// @Failure      400 {string} string "invalid device"
// @Failure      409 {string} string "device registered to another user"
// @Security     BearerAuth
// @Router       /me/mls/device [post]
func (m *MLS) RegisterDevice() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _ := UserID(r.Context())
		var req RegisterDeviceRequest
		if err := decodeJSON(r, &req); err != nil || !validID(req.DeviceID) || req.SigningKey == "" || len(req.SigningKey) > 256 {
			http.Error(w, "invalid device", http.StatusBadRequest)
			return
		}
		err := m.store.RegisterDevice(r.Context(), req.DeviceID, userID, req.SigningKey)
		if errors.Is(err, repo.ErrDeviceTaken) {
			http.Error(w, "device registered to another user", http.StatusConflict)
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "mls: error registering device", logger.User(userID), "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type PublishKeyPackagesRequest struct {
	DeviceID    string           `json:"deviceId"`
	KeyPackages []mls.KeyPackage `json:"keyPackages"`
}

// @Summary      Publish key packages
// @Description  Adds single-use key packages for one of the caller's devices. Every server the caller belongs to is nudged (`mls-sync`) so a committer can add the device.
// @Tags         MLS
// @Accept       json
// @Param        body body PublishKeyPackagesRequest true "Key packages"
// @Success      204
// @Failure      400 {string} string "invalid key packages"
// @Failure      403 {string} string "device belongs to another user"
// @Security     BearerAuth
// @Router       /me/mls/keypackages [post]
func (m *MLS) PublishKeyPackages() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID, _ := UserID(ctx)
		var req PublishKeyPackagesRequest
		if err := decodeJSON(r, &req); err != nil || len(req.KeyPackages) == 0 || len(req.KeyPackages) > maxKeyPackagesPerPost {
			http.Error(w, "invalid key packages", http.StatusBadRequest)
			return
		}
		for _, kp := range req.KeyPackages {
			if !validID(kp.ID) || kp.Payload == "" || len(kp.Payload) > maxKeyPackageBytes {
				http.Error(w, "invalid key packages", http.StatusBadRequest)
				return
			}
		}
		if !m.ownsDevice(w, r, userID, req.DeviceID) {
			return
		}
		if err := m.store.AddKeyPackages(ctx, req.DeviceID, userID, req.KeyPackages); err != nil {
			slog.ErrorContext(ctx, "mls: error saving key packages", logger.User(userID), "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		serverIDs, err := m.memberships.GetByUser(ctx, userID)
		if err != nil {
			slog.ErrorContext(ctx, "mls: error listing memberships", logger.User(userID), "error", err)
		}
		for _, sid := range serverIDs {
			m.presence.BroadcastToServer(sid, map[string]any{"type": "mls-sync", "serverId": sid})
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type GroupStateResponse struct {
	Epoch          int64            `json:"epoch"`
	Members        []mls.Member     `json:"members"`
	PendingAdds    []mls.PendingAdd `json:"pendingAdds"`
	PendingRemoves []mls.Member     `json:"pendingRemoves"`
	// The connected member expected to commit pending changes: the owner when
	// online, else the lowest connected user id. Anyone may still commit.
	CoordinatorUserID string       `json:"coordinatorUserId,omitempty"`
	Welcome           *mls.Welcome `json:"welcome,omitempty"`
	KeyPackagesLeft   int          `json:"keyPackagesLeft"`
}

// @Summary      MLS group state
// @Description  Member-only. The group's epoch and roster as the delivery service sees it, the devices waiting to be added or evicted, who is expected to commit, and — for the calling device — any pending welcome and how many key packages it has left.
// @Tags         MLS
// @Produce      json
// @Param        id path string true "Server ID"
// @Param        deviceId query string true "Calling device"
// @Success      200 {object} GroupStateResponse
// @Failure      403 {string} string "forbidden"
// @Security     BearerAuth
// @Router       /server/{id}/mls [get]
func (m *MLS) State() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID, _ := UserID(ctx)
		serverID := r.PathValue("id")
		deviceID := r.URL.Query().Get("deviceId")
		if !m.ownsDevice(w, r, userID, deviceID) {
			return
		}
		st, err := m.store.GroupState(ctx, serverID)
		if err == nil {
			var adds []mls.PendingAdd
			var removes []mls.Member
			var welcome *mls.Welcome
			var left int
			if adds, err = m.store.PendingAdds(ctx, serverID); err == nil {
				if removes, err = m.store.PendingRemoves(ctx, serverID); err == nil {
					if welcome, err = m.store.Welcome(ctx, serverID, deviceID); err == nil {
						left, err = m.store.CountKeyPackages(ctx, deviceID)
					}
				}
			}
			if err == nil {
				resp := GroupStateResponse{
					Epoch: st.Epoch, Members: st.Members, PendingAdds: adds, PendingRemoves: removes,
					Welcome: welcome, KeyPackagesLeft: left,
				}
				resp.CoordinatorUserID = m.coordinator(ctx, serverID, st, adds)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
				return
			}
		}
		slog.ErrorContext(ctx, "mls: error loading group state", logger.User(userID), logger.Server(serverID), "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// coordinator picks who should drive the next commit among the users online
// right now. Only users already in the group (or, for an empty group, any
// member with a device) can build one.
func (m *MLS) coordinator(ctx context.Context, serverID string, st *mls.GroupState, adds []mls.PendingAdd) string {
	online := m.presence.ConnectedUserIDs(serverID)
	if len(online) == 0 {
		return ""
	}
	eligible := map[string]bool{}
	for _, mem := range st.Members {
		eligible[mem.UserID] = true
	}
	if len(st.Members) == 0 {
		for _, a := range adds {
			eligible[a.UserID] = true
		}
	}
	candidates := online[:0:0]
	for _, id := range online {
		if eligible[id] {
			candidates = append(candidates, id)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	owner, err := m.owners.OwnerID(ctx, serverID)
	if err == nil && owner != "" && slices.Contains(candidates, owner) {
		return owner
	}
	return slices.Min(candidates)
}

// @Summary      Commits after an epoch
// @Description  Member-only. The commits a device missed, in epoch order, so an offline device can catch up.
// @Tags         MLS
// @Produce      json
// @Param        id path string true "Server ID"
// @Param        after query int true "Last epoch the device applied"
// @Success      200 {array} mls.Commit
// @Security     BearerAuth
// @Router       /server/{id}/mls/commits [get]
func (m *MLS) Commits() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		serverID := r.PathValue("id")
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		commits, err := m.store.CommitsAfter(ctx, serverID, after, maxCommitsPerPage)
		if err != nil {
			slog.ErrorContext(ctx, "mls: error loading commits", logger.Server(serverID), "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(commits)
	}
}

type CommitResponse struct {
	Epoch int64 `json:"epoch"`
}

// @Summary      Post a commit
// @Description  Member-only. Advances the group one epoch. Rejected with 409 when built against a stale epoch (fetch the missed commits and retry), 400 when it adds a device without an available key package or removes one not in the group. On success the commit is broadcast as `mls-commit` to every connected member.
// @Tags         MLS
// @Accept       json
// @Produce      json
// @Param        id path string true "Server ID"
// @Param        body body mls.CommitRequest true "Commit"
// @Success      200 {object} CommitResponse
// @Failure      400 {string} string "invalid commit"
// @Failure      409 {string} string "stale epoch"
// @Security     BearerAuth
// @Router       /server/{id}/mls/commit [post]
func (m *MLS) Commit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID, _ := UserID(ctx)
		serverID := r.PathValue("id")
		var req mls.CommitRequest
		if err := decodeJSON(r, &req); err != nil || req.Payload == "" || len(req.Payload) > maxCommitBytes || req.Epoch < 0 {
			http.Error(w, "invalid commit", http.StatusBadRequest)
			return
		}
		for _, wl := range req.Welcomes {
			if len(wl) > maxWelcomeBytes {
				http.Error(w, "invalid commit", http.StatusBadRequest)
				return
			}
		}
		if !m.ownsDevice(w, r, userID, req.DeviceID) {
			return
		}
		epoch, err := m.store.Commit(ctx, serverID, req)
		switch {
		case errors.Is(err, repo.ErrStaleEpoch):
			http.Error(w, "stale epoch", http.StatusConflict)
			return
		case errors.Is(err, repo.ErrNotInGroup), errors.Is(err, repo.ErrInvalidAdd),
			errors.Is(err, repo.ErrInvalidRemove), errors.Is(err, repo.ErrMissingWelcome):
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		case err != nil:
			slog.ErrorContext(ctx, "mls: error applying commit", logger.User(userID), logger.Server(serverID), "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		slog.InfoContext(ctx, "mls commit applied", logger.User(userID), logger.Server(serverID),
			"epoch", epoch, "adds", len(req.Adds), "removes", len(req.Removes))
		m.presence.BroadcastToServer(serverID, map[string]any{
			"type":           "mls-commit",
			"serverId":       serverID,
			"epoch":          epoch,
			"senderDeviceId": req.DeviceID,
			"payload":        req.Payload,
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CommitResponse{Epoch: epoch})
	}
}

type ConsumeWelcomeRequest struct {
	ID       string `json:"id"`
	DeviceID string `json:"deviceId"`
}

// @Summary      Acknowledge a welcome
// @Description  Marks a welcome as processed so the state endpoint stops returning it.
// @Tags         MLS
// @Accept       json
// @Param        id path string true "Server ID"
// @Param        body body ConsumeWelcomeRequest true "Welcome"
// @Success      204
// @Security     BearerAuth
// @Router       /server/{id}/mls/welcome/ack [post]
func (m *MLS) AckWelcome() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _ := UserID(r.Context())
		var req ConsumeWelcomeRequest
		if err := decodeJSON(r, &req); err != nil || !validID(req.ID) {
			http.Error(w, "invalid welcome", http.StatusBadRequest)
			return
		}
		if !m.ownsDevice(w, r, userID, req.DeviceID) {
			return
		}
		if err := m.store.ConsumeWelcome(r.Context(), req.ID, req.DeviceID); err != nil {
			slog.ErrorContext(r.Context(), "mls: error consuming welcome", logger.User(userID), "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (m *MLS) ownsDevice(w http.ResponseWriter, r *http.Request, userID, deviceID string) bool {
	if !validID(deviceID) {
		http.Error(w, "invalid device id", http.StatusBadRequest)
		return false
	}
	dev, err := m.store.GetDevice(r.Context(), deviceID)
	if err != nil {
		slog.ErrorContext(r.Context(), "mls: error loading device", logger.User(userID), "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return false
	}
	if dev == nil || dev.UserID != userID {
		http.Error(w, "unknown device", http.StatusForbidden)
		return false
	}
	return true
}

func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, mlsBodyCap)).Decode(v)
}

func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}
