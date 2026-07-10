package handlers

import (
	"fmt"
	"github.com/armonic-tech/armonic-backend/internal/models/signal"
	"github.com/armonic-tech/armonic-backend/internal/models/user"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
	"log/slog"
)

func (s *connSession) handleAuth(msg signal.Message) error {
	claims, err := s.h.auth.Validate(s.ctx, msg.Token)
	if err != nil {
		slog.WarnContext(s.ctx, "auth failed", "error", err)
		s.conn.SendJSON(map[string]any{"type": "error", "message": "unauthorized"})
		return fmt.Errorf("unauthorized")
	}

	if msg.Name != "" {
		if err := s.h.userRepo.Upsert(s.ctx, claims.Sub, msg.Name); err != nil {
			slog.ErrorContext(s.ctx, "auth: error saving display name", logger.User(claims.Sub), "error", err)
		}
	}

	displayName, err := s.h.userRepo.GetName(s.ctx, claims.Sub)
	if err != nil {
		slog.ErrorContext(s.ctx, "auth: error loading display name", logger.User(claims.Sub), "error", err)
	}

	s.user = &user.User{ID: claims.Sub, DisplayName: displayName, Signaling: s.conn}

	serverIDs, err := s.h.membershipRepo.GetByUser(s.ctx, claims.Sub)
	if err != nil {
		slog.ErrorContext(s.ctx, "auth: error loading memberships", logger.User(claims.Sub), "error", err)
	}
	for _, sid := range serverIDs {
		s.h.app.GetOrCreateServer(sid).AddConnectedUser(s.user)
	}

	slog.InfoContext(s.ctx, "user authenticated", logger.User(claims.Sub))
	s.conn.SendJSON(map[string]any{"type": "auth-ok", "userId": claims.Sub, "displayName": displayName})
	return nil
}
