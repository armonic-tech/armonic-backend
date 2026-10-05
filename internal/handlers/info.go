package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/armonic-tech/armonic-backend/config"
	"github.com/armonic-tech/armonic-backend/internal/models/server"
)

// LogoLookup is the logo half of ServerLogoStore: /info only reads.
type LogoLookup interface {
	LogoID(ctx context.Context, serverID string) (string, error)
}

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
	Claimed      bool   `json:"claimed" example:"false"`
	Description  string `json:"description" example:""`
	Host         string `json:"host" example:"localhost:8080"`
	ID           string `json:"id" example:"1a2b3c4d5e-..."`
	MemberCount  int    `json:"memberCount" example:"0"`
	Name         string `json:"name" example:"Armonic"`
	LogoID       string `json:"logoId,omitempty"`
	LogoURL      string `json:"logoUrl,omitempty"`
	LogoThumbURL string `json:"logoThumbUrl,omitempty"`
}

// Server godoc
// @Summary      Server info
// @Description  Get status info from server
// @Tags         Server
// @Accept       json
// @Produce      json
// @Success      200  {object}  ServerResponse
// @Router       /info [get]
func InfoHandler(cfg config.Config, members MemberCounter, logos LogoLookup, serverID string, claimed func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		count, err := members.CountAll(r.Context())
		if err != nil {
			slog.ErrorContext(r.Context(), "info: error counting members", "error", err)
			count = 0
		}
		logoID, err := logos.LogoID(r.Context(), serverID)
		if err != nil {
			slog.ErrorContext(r.Context(), "info: error reading logo", "error", err)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := ServerResponse{
			ID:          serverID,
			Name:        cfg.ServerName,
			Description: cfg.ServerDescription,
			MemberCount: count,
			Host:        cfg.Host + ":" + cfg.Port,
			Claimed:     claimed(),
		}
		if logoID != "" {
			resp.LogoID = logoID
			resp.LogoURL = server.LogoURLFor(serverID, logoID)
			resp.LogoThumbURL = server.LogoThumbURLFor(serverID, logoID)
		}
		json.NewEncoder(w).Encode(resp)
	}
}
