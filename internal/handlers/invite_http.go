package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	authpkg "github.com/armonic-tech/armonic-backend/internal/auth"
	"github.com/armonic-tech/armonic-backend/internal/models/invite"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
)

type InviteLookup interface {
	Get(ctx context.Context, token string) (*invite.Invite, error)
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
