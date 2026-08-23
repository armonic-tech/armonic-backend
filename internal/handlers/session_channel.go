package handlers

import (
	"log/slog"
	"strings"

	"github.com/armonic-tech/armonic-backend/internal/models/channel"
	"github.com/armonic-tech/armonic-backend/internal/models/signal"
	"github.com/armonic-tech/armonic-backend/pkg/logger"

	"github.com/google/uuid"
)

const maxChannelNameLen = 64

func (s *connSession) handleCreateChannel(msg signal.Message) {
	ok, err := s.h.serverRepo.IsOwner(s.ctx, msg.ServerID, s.user.ID)
	if err != nil || !ok {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "unauthorized"})
		return
	}
	name := strings.TrimSpace(msg.Name)
	if name == "" || len(name) > maxChannelNameLen {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "channel name invalid"})
		return
	}
	if msg.ChannelType != "text" && msg.ChannelType != "voice" {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "channel type invalid"})
		return
	}

	taken, err := s.h.channelRepo.NameTaken(s.ctx, msg.ServerID, msg.ChannelType, name)
	if err != nil {
		slog.ErrorContext(s.ctx, "create-channel name check error", logger.User(s.user.ID), logger.Server(msg.ServerID), "error", err)
		s.conn.SendJSON(map[string]any{"type": "error", "message": "could not create channel"})
		return
	}
	if taken {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "channel name taken"})
		return
	}

	ch := channel.ChannelInfo{
		ID:       uuid.New().String(),
		ServerID: msg.ServerID,
		Name:     name,
		Type:     msg.ChannelType,
	}
	if err := s.h.channelRepo.Create(s.ctx, ch.ID, ch.ServerID, ch.Name, ch.Type); err != nil {
		if taken, checkErr := s.h.channelRepo.NameTaken(s.ctx, msg.ServerID, msg.ChannelType, name); checkErr == nil && taken {
			s.conn.SendJSON(map[string]any{"type": "error", "message": "channel name taken"})
			return
		}
		slog.ErrorContext(s.ctx, "create-channel error", logger.User(s.user.ID), logger.Server(msg.ServerID), "error", err)
		s.conn.SendJSON(map[string]any{"type": "error", "message": "could not create channel"})
		return
	}
	slog.InfoContext(s.ctx, "channel created", logger.User(s.user.ID), logger.Server(ch.ServerID), logger.Channel(ch.ID), "type", ch.Type)

	s.h.app.GetOrCreateServer(ch.ServerID).BroadcastMessage("", map[string]any{
		"type":    "channel-created",
		"channel": ch,
	})
}

func (s *connSession) handleDeleteChannel(msg signal.Message) {
	ok, err := s.h.serverRepo.IsOwner(s.ctx, msg.ServerID, s.user.ID)
	if err != nil || !ok {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "unauthorized"})
		return
	}
	ch, err := s.h.channelRepo.GetByID(s.ctx, msg.ChannelID)
	if err != nil {
		slog.ErrorContext(s.ctx, "delete-channel lookup error", logger.User(s.user.ID), logger.Channel(msg.ChannelID), "error", err)
		s.conn.SendJSON(map[string]any{"type": "error", "message": "could not delete channel"})
		return
	}
	if ch == nil || ch.ServerID != msg.ServerID {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "channel not found"})
		return
	}

	deleted, err := s.h.channelRepo.SoftDelete(s.ctx, msg.ServerID, msg.ChannelID)
	if err != nil {
		slog.ErrorContext(s.ctx, "delete-channel error", logger.User(s.user.ID), logger.Server(msg.ServerID), logger.Channel(msg.ChannelID), "error", err)
		s.conn.SendJSON(map[string]any{"type": "error", "message": "could not delete channel"})
		return
	}
	if !deleted {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "channel not found"})
		return
	}

	srv := s.h.app.GetOrCreateServer(msg.ServerID)
	if ch.Type == "voice" {
		if vc := srv.RemoveVoiceChannel(msg.ChannelID); vc != nil {
			vc.CloseAll()
		}
	} else {
		srv.RemoveTextChannel(msg.ChannelID)
	}

	slog.InfoContext(s.ctx, "channel deleted", logger.User(s.user.ID), logger.Server(msg.ServerID), logger.Channel(msg.ChannelID), "type", ch.Type)
	srv.BroadcastMessage("", map[string]any{
		"type":      "channel-deleted",
		"serverId":  msg.ServerID,
		"channelId": msg.ChannelID,
	})
}
