package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	authpkg "github.com/armonic-tech/armonic-backend/internal/auth"
	"github.com/armonic-tech/armonic-backend/internal/models/invite"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
)

const inviteTTL = 24 * time.Hour

type InviteLookup interface {
	Get(ctx context.Context, token string) (*invite.Invite, error)
}

// InviteCreator mints a single-use invite token for a server.
type InviteCreator interface {
	Create(ctx context.Context, serverID, createdBy string, expiresIn time.Duration) (string, error)
}

type InviteCreatedResponse struct {
	InviteToken string `json:"inviteToken"`
	URL         string `json:"url"`
}

// CreateInvite mints a single-use invite for server {id}. Replaces the old
// `create-invite` WS message. Registered with router.Owner, so ownership of
// {id} is already verified before this runs; the invite's creator is the
// authenticated caller (UserID(ctx)).
//
// @Summary      Create invite
// @Description  Owner-only. Mint a single-use invite link for a server.
// @Tags         Invite
// @Produce      json
// @Param        id path string true "Server ID"
// @Success      201 {object} InviteCreatedResponse
// @Failure      401 {string} string "unauthorized"
// @Failure      403 {string} string "forbidden"
// @Security     BearerAuth
// @Router       /server/{id}/invite [post]
func CreateInvite(invites InviteCreator, baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		serverID := r.PathValue("id")
		userID, ok := UserID(ctx)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		token, err := invites.Create(ctx, serverID, userID, inviteTTL)
		if err != nil {
			slog.ErrorContext(ctx, "create-invite error", logger.User(userID), logger.Server(serverID), "error", err)
			http.Error(w, "error creating invite", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(InviteCreatedResponse{
			InviteToken: token,
			URL:         fmt.Sprintf("%s?invite=%s", baseURL, token),
		})
	}
}

func InviteStatusHandler(invites InviteLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		token := r.URL.Query().Get("token")
		inv, err := invites.Get(r.Context(), token)
		if err != nil {
			slog.ErrorContext(r.Context(), "invite-status error", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if inv == nil {
			http.Error(w, "invalid or expired invite", http.StatusGone)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"serverId":  inv.ServerID,
			"expiresAt": inv.ExpiresAt,
		})
	}
}

func InviteSignupHandler(invites InviteRepo, auth RegisterAuthenticator, members MemberAdder, claimed func() bool) http.HandlerFunc {
	type request struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Password string `json:"password"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !claimed() {
			http.Error(w, "server not claimed yet", http.StatusForbidden)
			return
		}

		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}

		inv, err := invites.Get(r.Context(), req.Token)
		if err != nil {
			slog.ErrorContext(r.Context(), "invite-signup lookup error", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if inv == nil {
			http.Error(w, "invalid or expired invite", http.StatusGone)
			return
		}

		token, err := auth.Signup(r.Context(), req.Username, req.Password)
		if err != nil {
			status := http.StatusInternalServerError
			if err == authpkg.ErrUsernameTaken {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}

		claims, err := auth.Validate(r.Context(), token)
		if err != nil {
			slog.ErrorContext(r.Context(), "invite-signup error validating freshly issued token", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		if err := members.Add(r.Context(), claims.Sub, inv.ServerID); err != nil {
			slog.ErrorContext(r.Context(), "invite-signup membership error", logger.User(claims.Sub), logger.Server(inv.ServerID), "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := invites.MarkUsed(r.Context(), req.Token); err != nil {
			slog.ErrorContext(r.Context(), "invite-signup error marking invite used", logger.User(claims.Sub), logger.Server(inv.ServerID), "error", err)
		}

		slog.InfoContext(r.Context(), "user joined via invite signup", logger.User(claims.Sub), logger.Server(inv.ServerID))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"token": token})
	}
}
