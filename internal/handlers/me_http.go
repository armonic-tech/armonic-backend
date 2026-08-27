package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/armonic-tech/armonic-backend/internal/models/attachment"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
)

type ProfileGetter interface {
	GetProfile(ctx context.Context, id string) (displayName, avatarID string, err error)
}

type MeResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	AvatarID    string `json:"avatarId,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
}

// @Summary      Own profile
// @Description  The authenticated caller's id, display name and avatar.
// @Tags         Users
// @Produce      json
// @Success      200 {object} MeResponse
// @Failure      401 {string} string "unauthorized"
// @Security     BearerAuth
// @Router       /me [get]
func GetMe(users ProfileGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID, ok := UserID(ctx)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		displayName, avatarID, err := users.GetProfile(ctx, userID)
		if err != nil {
			slog.ErrorContext(ctx, "me: error loading profile", logger.User(userID), "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		resp := MeResponse{ID: userID, DisplayName: displayName, AvatarID: avatarID}
		if avatarID != "" {
			resp.AvatarURL = attachment.URLFor(avatarID)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
