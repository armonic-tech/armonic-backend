package handlers

import (
	"log/slog"

	"github.com/armonic-tech/armonic-backend/internal/models/signal"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
)

func (s *connSession) handleJoinServer(msg signal.Message) {
	inv, err := s.h.inviteRepo.Get(s.ctx, msg.InviteToken)
	if err != nil || inv == nil {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "invalid invite"})
		return
	}
	if err := s.h.membershipRepo.Add(s.ctx, s.user.ID, inv.ServerID); err != nil {
		slog.ErrorContext(s.ctx, "join-server error", logger.User(s.user.ID), logger.Server(inv.ServerID), "error", err)
		return
	}
	if err := s.h.inviteRepo.MarkUsed(s.ctx, msg.InviteToken); err != nil {
		slog.ErrorContext(s.ctx, "join-server error marking invite used", logger.User(s.user.ID), logger.Server(inv.ServerID), "error", err)
	}
	s.h.app.GetOrCreateServer(inv.ServerID).AddConnectedUser(s.user)
	channels, err := s.h.channelRepo.GetChannelByServer(s.ctx, inv.ServerID)
	if err != nil {
		slog.ErrorContext(s.ctx, "join-server channels error", logger.Server(inv.ServerID), "error", err)
	}
	slog.InfoContext(s.ctx, "user joined server", logger.User(s.user.ID), logger.Server(inv.ServerID))
	s.conn.SendJSON(map[string]any{
		"type":     "joined-server",
		"serverId": inv.ServerID,
		"channels": channels,
	})
}
