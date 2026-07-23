package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	authpkg "github.com/armonic-tech/armonic-backend/internal/auth"
	"github.com/armonic-tech/armonic-backend/internal/claim"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
)

type RegisterAuthenticator interface {
	Signup(ctx context.Context, username, password string) (string, error)
	Validate(ctx context.Context, token string) (*authpkg.Claims, error)
}

type ServerOwnerSetter interface {
	SetOwner(ctx context.Context, serverID, ownerID string) error
}

func ClaimPasswordHandler(mgr *claim.Manager, claimed func() bool) http.HandlerFunc {
	type request struct {
		Password string `json:"password"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if claimed() {
			http.Error(w, "server already claimed", http.StatusConflict)
			return
		}

		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}

		ticket, expiresAt, ok := mgr.VerifyPassword(req.Password)
		if !ok {
			http.Error(w, "invalid password", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ticket":    ticket,
			"expiresAt": expiresAt,
		})
	}
}

func ClaimRegisterHandler(mgr *claim.Manager, auth RegisterAuthenticator, settings OwnerSetter, servers ServerOwnerSetter, members MemberAdder, serverID string, claimed func() bool) http.HandlerFunc {
	type request struct {
		Ticket   string `json:"ticket"`
		Username string `json:"username"`
		Password string `json:"password"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if claimed() {
			http.Error(w, "server already claimed", http.StatusConflict)
			return
		}

		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}

		if !mgr.ValidateTicket(req.Ticket) {
			http.Error(w, "invalid or expired ticket", http.StatusUnauthorized)
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
			slog.ErrorContext(r.Context(), "claim: error validating freshly issued token", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		if err := settings.Set(r.Context(), "owner", claims.Sub); err != nil {
			slog.ErrorContext(r.Context(), "claim: error saving owner", logger.User(claims.Sub), "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := servers.SetOwner(r.Context(), serverID, claims.Sub); err != nil {
			slog.ErrorContext(r.Context(), "claim: error setting server owner", logger.User(claims.Sub), logger.Server(serverID), "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := members.Add(r.Context(), claims.Sub, serverID); err != nil {
			slog.ErrorContext(r.Context(), "claim: error adding membership", logger.User(claims.Sub), logger.Server(serverID), "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		slog.InfoContext(r.Context(), "server claimed", logger.User(claims.Sub))
		mgr.InvalidateTicket()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"token": token})
	}
}
