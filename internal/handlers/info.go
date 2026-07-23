package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/armonic-tech/armonic-backend/config"
)

type MemberCounter interface {
	CountAll(ctx context.Context) (int, error)
}

type MemberAdder interface {
	Add(ctx context.Context, userID, serverID string) error
}

type OwnerSetter interface {
	Set(ctx context.Context, key, value string) error
}

type ServerResponse struct {
	Claimed     bool   `json:"claimed" example:"false"`
	Description string `json:"description" example:""`
	Host        string `json:"host" example:"localhost:8080"`
	ID          string `json:"id" example:"1a2b3c4d5e-..."`
	MemberCount int    `json:"memberCount" example:"0"`
	Name        string `json:"name" example:"Armonic"`
}

// Server godoc
// @Summary      Server info
// @Description  Get status info from server
// @Tags         Server
// @Accept       json
// @Produce      json
// @Success      200  {object}  ServerResponse
// @Router       /info [get]
func InfoHandler(cfg config.Config, members MemberCounter, serverID string, claimed func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		count, err := members.CountAll(r.Context())
		if err != nil {
			count = 0
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ServerResponse{
			ID:          serverID,
			Name:        cfg.ServerName,
			Description: cfg.ServerDescription,
			MemberCount: count,
			Host:        cfg.Host + ":" + cfg.Port,
			Claimed:     claimed(),
		})
	}
}
