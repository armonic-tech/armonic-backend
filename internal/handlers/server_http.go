package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/armonic-tech/armonic-backend/internal/models/server"
)

type MembershipLister interface {
	GetByUser(ctx context.Context, userID string) ([]string, error)
}

type ServerByIDsGetter interface {
	GetByIDs(ctx context.Context, ids []string) ([]server.ServerInfo, error)
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
