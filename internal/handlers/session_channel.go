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
		s.reject(msg.Type, "unauthorized", err, logger.Server(msg.ServerID))
		return
	}
	name := strings.TrimSpace(msg.Name)
	if name == "" || len(name) > maxChannelNameLen {
		s.reject(msg.Type, "channel name invalid", nil, "length", len(name))
		return
	}
	if msg.ChannelType != "text" && msg.ChannelType != "voice" {
		s.reject(msg.Type, "channel type invalid", nil, "channel_type", msg.ChannelType)
		return
	}

	taken, err := s.h.channelRepo.NameTaken(s.ctx, msg.ServerID, msg.ChannelType, name)
	if err != nil {
		s.reject(msg.Type, "could not create channel", err, logger.Server(msg.ServerID))
		return
	}
	if taken {
		s.reject(msg.Type, "channel name taken", nil, logger.Server(msg.ServerID), "name", name)
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
			s.reject(msg.Type, "channel name taken", nil, logger.Server(msg.ServerID), "name", name, "lost_race", true)
			return
		}
		s.reject(msg.Type, "could not create channel", err, logger.Server(msg.ServerID))
		return
	}
	slog.InfoContext(s.ctx, "channel created", logger.Server(ch.ServerID), logger.Channel(ch.ID), "type", ch.Type)

	s.h.app.GetOrCreateServer(ch.ServerID).BroadcastMessage("", map[string]any{
		"type":    "channel-created",
		"channel": ch,
	})
}

func (s *connSession) handleDeleteChannel(msg signal.Message) {
	ok, err := s.h.serverRepo.IsOwner(s.ctx, msg.ServerID, s.user.ID)
	if err != nil || !ok {
		s.reject(msg.Type, "unauthorized", err, logger.Server(msg.ServerID))
		return
	}
	ch, err := s.h.channelRepo.GetByID(s.ctx, msg.ChannelID)
	if err != nil {
		s.reject(msg.Type, "could not delete channel", err, logger.Channel(msg.ChannelID))
		return
	}
	if ch == nil || ch.ServerID != msg.ServerID {
		s.reject(msg.Type, "channel not found", nil, logger.Server(msg.ServerID), logger.Channel(msg.ChannelID))
		return
	}

	deleted, err := s.h.channelRepo.SoftDelete(s.ctx, msg.ServerID, msg.ChannelID)
	if err != nil {
		s.reject(msg.Type, "could not delete channel", err, logger.Server(msg.ServerID), logger.Channel(msg.ChannelID))
		return
	}
	if !deleted {
		s.reject(msg.Type, "channel not found", nil, logger.Server(msg.ServerID), logger.Channel(msg.ChannelID))
		return
	}

	srv := s.h.app.GetOrCreateServer(msg.ServerID)
	closed := 0
	if ch.Type == "voice" {
		if vc := srv.RemoveVoiceChannel(msg.ChannelID); vc != nil {
			closed = vc.CloseAll()
		}
	} else {
		srv.RemoveTextChannel(msg.ChannelID)
	}

	slog.InfoContext(s.ctx, "channel deleted", logger.Server(msg.ServerID), logger.Channel(msg.ChannelID), "type", ch.Type, "voice_users_dropped", closed)
	srv.BroadcastMessage("", map[string]any{
		"type":      "channel-deleted",
		"serverId":  msg.ServerID,
		"channelId": msg.ChannelID,
	})
}
