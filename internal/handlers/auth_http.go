package handlers

import (
	"context"
	"encoding/json"
	"net/http"
)

type AuthService interface {
	Signup(ctx context.Context, username, password string) (string, error)
	Login(ctx context.Context, username, password string) (string, error)
}

func credentialsRequest(r *http.Request) (username, password string, ok bool) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", "", false
	}
	return req.Username, req.Password, true
}

func LoginHandler(svc AuthService, claimed func() bool) http.HandlerFunc {
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

		username, password, ok := credentialsRequest(r)
		if !ok {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}

		token, err := svc.Login(r.Context(), username, password)
		if err != nil {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"token": token})
	}
}
