package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/armonic-tech/armonic-backend/internal/models/attachment"
	"github.com/armonic-tech/armonic-backend/internal/models/server"
	"github.com/google/uuid"
)

type MembershipLister interface {
	GetByUser(ctx context.Context, userID string) ([]string, error)
}

type ServerByIDsGetter interface {
	GetByIDs(ctx context.Context, ids []string) ([]server.ServerInfo, error)
}

type MemberLister interface {
	GetMembers(ctx context.Context, serverID string) ([]server.MemberInfo, error)
}

type PresenceLister interface {
	ConnectedUserIDs(serverID string) []string
}

// @Summary      Server members
// @Description  Member-only. The roster of a server: every member with their display name, avatar and owner flag, plus whether they currently have a live WebSocket connection.
// @Tags         Server
// @Produce      json
// @Param        id path string true "Server ID"
// @Success      200 {array} server.MemberInfo
// @Failure      400 {string} string "invalid server id"
// @Failure      403 {string} string "forbidden"
// @Security     BearerAuth
// @Router       /server/{id}/members [get]
func GetServerMembers(members MemberLister, presence PresenceLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		serverID := r.PathValue("id")

		if _, err := uuid.Parse(serverID); err != nil {
			http.Error(w, "invalid server id", http.StatusBadRequest)
			return
		}

		list, err := members.GetMembers(ctx, serverID)
		if err != nil {
			http.Error(w, "error loading members", http.StatusInternalServerError)
			return
		}
		if list == nil {
			list = []server.MemberInfo{}
		}

		online := make(map[string]struct{})
		for _, id := range presence.ConnectedUserIDs(serverID) {
			online[id] = struct{}{}
		}
		for i := range list {
			if list[i].AvatarID != "" {
				list[i].AvatarURL = attachment.URLFor(list[i].AvatarID)
			}
			_, list[i].Online = online[list[i].ID]
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	}
}

// @Summary      My servers
// @Description  List the servers the authenticated user is a member of
// @Tags         Server
// @Produce      json
// @Success      200 {array} server.ServerInfo
// @Failure      401 {string} string "unauthorized"
// @Security     BearerAuth
// @Router       /server [get]
func GetMyServers(memberships MembershipLister, servers ServerByIDsGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID, ok := UserID(ctx)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		serverIDs, err := memberships.GetByUser(ctx, userID)
		if err != nil {
			http.Error(w, "error loading servers", http.StatusInternalServerError)
			return
		}

		list, err := servers.GetByIDs(ctx, serverIDs)
		if err != nil {
			http.Error(w, "error loading servers", http.StatusInternalServerError)
			return
		}
		if list == nil {
			list = []server.ServerInfo{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	}
}
