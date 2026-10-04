package handlers

import (
	"fmt"
	"log/slog"

	"github.com/armonic-tech/armonic-backend/internal/models/signal"
	"github.com/armonic-tech/armonic-backend/internal/models/user"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
)

func (s *connSession) handleAuth(msg signal.Message) error {
	if s.user != nil {
		slog.WarnContext(s.ctx, "ws: auth on an already authenticated connection ignored")
		return nil
	}
	claims, err := s.h.auth.Validate(s.ctx, msg.Token)
	if err != nil {
		slog.WarnContext(s.ctx, "auth failed", "error", err)
		s.send(map[string]any{"type": "error", "message": "unauthorized"})
		return fmt.Errorf("unauthorized")
	}
	s.ctx = logger.With(s.ctx, logger.User(claims.Sub))

	if msg.Name != "" {
		if err := s.h.userRepo.Upsert(s.ctx, claims.Sub, msg.Name); err != nil {
			slog.ErrorContext(s.ctx, "auth: error saving display name", "error", err)
		}
	}

	displayName, avatarID, err := s.h.userRepo.GetProfile(s.ctx, claims.Sub)
	if err != nil {
		slog.ErrorContext(s.ctx, "auth: error loading profile", "error", err)
	}

	s.user = &user.User{ID: claims.Sub, DisplayName: displayName, AvatarID: avatarID, Signaling: s.conn}

	serverIDs, err := s.h.membershipRepo.GetByUser(s.ctx, claims.Sub)
	if err != nil {
		slog.ErrorContext(s.ctx, "auth: error loading memberships", "error", err)
	}
	for _, sid := range serverIDs {
		s.h.app.GetOrCreateServer(sid).AddConnectedUser(s.user)
	}

	slog.InfoContext(s.ctx, "user authenticated", "servers", len(serverIDs))
	s.send(map[string]any{"type": "auth-ok", "userId": claims.Sub, "displayName": displayName, "avatarId": avatarID})
	return nil
}
