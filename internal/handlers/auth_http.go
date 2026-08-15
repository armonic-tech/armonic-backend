package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	authpkg "github.com/armonic-tech/armonic-backend/internal/auth"

	_ "github.com/armonic-tech/armonic-backend/docs"
)

func signupStatus(err error) int {
	switch {
	case errors.Is(err, authpkg.ErrUsernameTaken):
		return http.StatusConflict
	case errors.Is(err, authpkg.ErrInvalidInput):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

type AuthService interface {
	Signup(ctx context.Context, username, password string) (string, error)
	Login(ctx context.Context, username, password string) (string, error)
}

type LoginRequest struct {
	Username string `json:"username" example:"admin"`
	Password string `json:"password" example:"s3cr3t-p4ss"`
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
// @Router       /auth/login [post]
func LoginHandler(svc AuthService, claimed func() bool) http.HandlerFunc {
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

		token, err := svc.Login(r.Context(), req.Username, req.Password)
		if err != nil {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LoginResponse{Token: token})
	}
}
