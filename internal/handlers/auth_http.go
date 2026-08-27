package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	_ "github.com/armonic-tech/armonic-backend/docs"
	"github.com/armonic-tech/armonic-backend/pkg/ratelimit"
)

type AuthService interface {
	Signup(ctx context.Context, username, password string) (string, error)
	Login(ctx context.Context, username, password string) (string, error)
}

type LoginRequest struct {
	Username string `json:"username" example:"admin"`
	Password string `json:"password" example:"s3cr3t-p4ss"`
	Altcha   string `json:"altcha,omitempty" example:"eyJhbGdvcml0aG0iOi..."`
}

type LoginResponse struct {
	Token string `json:"token" example:"eyJhbG..."`
}

// Login godoc
// @Summary      Login user
// @Description  Login user in already claimed server
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        credentials  body      LoginRequest   true  "User credentials"
// @Success      200          {object}  LoginResponse
// @Failure      400          {string}  string  "invalid body"
// @Failure      401          {string}  string  "invalid credentials"
// @Failure      403          {string}  string  "server not claimed yet"
// @Failure      409          {string}  string  "proof of work expired or already used"
// @Failure      429          {string}  string  "rate limit exceeded"
// @Router       /auth/login [post]
func LoginHandler(svc AuthService, claimed func() bool, perAccount *ratelimit.Limiter, verifier PowChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !claimed() {
			http.Error(w, "server not claimed yet", http.StatusForbidden)
			return
		}

		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}

		if !checkPow(w, verifier, req.Altcha) {
			return
		}

		if !perAccount.Allow(strings.ToLower(strings.TrimSpace(req.Username))) {
			ratelimit.Reject(w, perAccount)
			return
		}

		token, err := svc.Login(r.Context(), req.Username, req.Password)
		if err != nil {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LoginResponse{Token: token})
	}
}
