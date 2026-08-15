package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

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
	InviteToken string `json:"inviteToken" example:"6f1c5e0e-..."`
	URL         string `json:"url" example:"https://armonic.example?invite=6f1c5e0e-..."`
}

type InviteStatusResponse struct {
	ServerID  string    `json:"serverId" example:"6f1c5e0e-..."`
	ExpiresAt time.Time `json:"expiresAt" example:"2026-08-13T10:00:00Z"`
}

type InviteSignupRequest struct {
	Token    string `json:"token" example:"6f1c5e0e-..."`
	Username string `json:"username" example:"member"`
	Password string `json:"password" example:"s3cr3t-p4ss"`
}

type InviteSignupResponse struct {
	Token string `json:"token" example:"eyJhbG..."`
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

		base := baseURL
		if base == "" {
			base = requestBaseURL(r)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(InviteCreatedResponse{
			InviteToken: token,
			URL:         fmt.Sprintf("%s?invite=%s", base, token),
		})
	}
}

// requestBaseURL rebuilds the address the caller reached this instance at, so
// the invite link is shareable as-is (the client derives the instance base URL
// from the link). Proxy headers win over the connection itself; see the invite
// section of docs/ai/http-api.md for why they're trusted here.
func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := firstHeaderValue(r, "X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	host := r.Host
	if h := firstHeaderValue(r, "X-Forwarded-Host"); h != "" {
		host = h
	}
	return scheme + "://" + host
}

// X-Forwarded-* accumulate one entry per hop ("a, b"); the first is the client-facing one.
func firstHeaderValue(r *http.Request, name string) string {
	v := r.Header.Get(name)
	if v == "" {
		return ""
	}
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// @Summary      Invite status
// @Description  Validate an invite link before showing the signup form.
// @Tags         Invite
// @Produce      json
// @Param        token query string true "Invite token"
// @Success      200 {object} InviteStatusResponse
// @Failure      410 {string} string "invalid or expired invite"
// @Router       /invite/status [get]
func InviteStatusHandler(invites InviteLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		json.NewEncoder(w).Encode(InviteStatusResponse{
			ServerID:  inv.ServerID,
			ExpiresAt: inv.ExpiresAt,
		})
	}
}

// @Summary      Invite signup
// @Description  Redeem a single-use invite into a brand-new account; no prior JWT needed.
// @Tags         Invite
// @Accept       json
// @Produce      json
// @Param        signup body InviteSignupRequest true "Invite token + credentials"
// @Success      200 {object} InviteSignupResponse
// @Failure      400 {string} string "invalid body"
// @Failure      403 {string} string "server not claimed yet"
// @Failure      409 {string} string "username taken"
// @Failure      410 {string} string "invalid or expired invite"
// @Router       /invite/signup [post]
func InviteSignupHandler(invites InviteRepo, auth RegisterAuthenticator, members MemberAdder, claimed func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !claimed() {
			http.Error(w, "server not claimed yet", http.StatusForbidden)
			return
		}

		var req InviteSignupRequest
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
			http.Error(w, err.Error(), signupStatus(err))
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
