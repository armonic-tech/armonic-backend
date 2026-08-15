package handlers

import (
	"log/slog"

	"github.com/armonic-tech/armonic-backend/internal/models/channel"
	"github.com/armonic-tech/armonic-backend/internal/models/signal"
	"github.com/armonic-tech/armonic-backend/pkg/logger"

	"github.com/google/uuid"
)

func (s *connSession) handleCreateServer(msg signal.Message) {
	serverID := uuid.New().String()
	if err := s.h.serverRepo.Create(s.ctx, serverID, msg.Name, s.user.ID); err != nil {
		slog.ErrorContext(s.ctx, "create-server error", logger.User(s.user.ID), "error", err)
		s.conn.SendJSON(map[string]any{"type": "error", "message": "could not create server"})
		return
	}
	if err := s.h.membershipRepo.Add(s.ctx, s.user.ID, serverID); err != nil {
		slog.ErrorContext(s.ctx, "create-server membership error", logger.User(s.user.ID), logger.Server(serverID), "error", err)
		s.conn.SendJSON(map[string]any{"type": "error", "message": "could not create server"})
		return
	}
	defaultChannels := []channel.ChannelInfo{
		{ID: uuid.New().String(), ServerID: serverID, Name: "general", Type: "text"},
		{ID: uuid.New().String(), ServerID: serverID, Name: "General", Type: "voice"},
	}
	for _, ch := range defaultChannels {
		if err := s.h.channelRepo.Create(s.ctx, ch.ID, ch.ServerID, ch.Name, ch.Type); err != nil {
			slog.ErrorContext(s.ctx, "create-server channel error", logger.Server(serverID), logger.Channel(ch.ID), "error", err)
		}
	}
	s.h.app.GetOrCreateServer(serverID).AddConnectedUser(s.user)
	slog.InfoContext(s.ctx, "server created", logger.User(s.user.ID), logger.Server(serverID))
	s.conn.SendJSON(map[string]any{
		"type":     "server-created",
		"serverId": serverID,
		"name":     msg.Name,
		"channels": defaultChannels,
	})
}

func (s *connSession) handleJoinServer(msg signal.Message) {
	inv, err := s.h.inviteRepo.Get(s.ctx, msg.InviteToken)
	if err != nil || inv == nil {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "invalid invite"})
		return
	}
	if err := s.h.membershipRepo.Add(s.ctx, s.user.ID, inv.ServerID); err != nil {
		slog.ErrorContext(s.ctx, "join-server error", logger.User(s.user.ID), logger.Server(inv.ServerID), "error", err)
		s.conn.SendJSON(map[string]any{"type": "error", "message": "could not join server"})
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
