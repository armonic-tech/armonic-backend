package handlers

import (
	"github.com/armonic-tech/armonic-backend/internal/models/message"
	"github.com/armonic-tech/armonic-backend/internal/models/signal"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

func (s *connSession) handleTextMessage(msg signal.Message) {
	ok, err := s.h.membershipRepo.IsMember(s.ctx, s.user.ID, msg.ServerID)
	if err != nil || !ok {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "unauthorized"})
		return
	}
	if len(msg.Content) == 0 || len(msg.Content) > s.h.cfg.MaxMsgLen {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "message content invalid"})
		return
	}
	m := message.Message{
		ID:        uuid.New().String(),
		ChannelID: msg.ChannelID,
		ServerID:  msg.ServerID,
		UserID:    s.user.ID,
		Content:   msg.Content,
		CreatedAt: time.Now(),
	}
	if err := s.h.messageRepo.Save(s.ctx, m); err != nil {
		slog.ErrorContext(s.ctx, "error saving message", logger.User(s.user.ID), logger.Server(msg.ServerID), logger.Channel(msg.ChannelID), "error", err)
		return
	}
	broadcast := map[string]any{
		"type":      "text-message",
		"id":        m.ID,
		"serverId":  m.ServerID,
		"channelId": m.ChannelID,
		"userId":    m.UserID,
		"content":   m.Content,
		"createdAt": m.CreatedAt,
	}
	s.h.app.GetOrCreateServer(msg.ServerID).BroadcastMessage(s.user.ID, broadcast)
}
