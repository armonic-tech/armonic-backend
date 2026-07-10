package handlers

import (
	"context"
	"encoding/json"
	"github.com/armonic-tech/armonic-backend/config"
	"net/http"
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

func cors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func InfoHandler(cfg config.Config, members MemberCounter, serverID string, claimed func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		count, err := members.CountAll(r.Context())
		if err != nil {
			count = 0
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          serverID,
			"name":        cfg.ServerName,
			"description": cfg.ServerDescription,
			"memberCount": count,
			"host":        cfg.Host + ":" + cfg.Port,
			"claimed":     claimed(),
		})
	}
}
