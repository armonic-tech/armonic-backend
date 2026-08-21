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

func (s *connSession) handleDeleteMessage(msg signal.Message) {
	ok, err := s.h.serverRepo.IsOwner(s.ctx, msg.ServerID, s.user.ID)
	if err != nil || !ok {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "unauthorized"})
		return
	}
	if msg.MessageID == "" || msg.ChannelID == "" {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "message id invalid"})
		return
	}
	deleted, err := s.h.messageRepo.SoftDelete(s.ctx, msg.ServerID, msg.ChannelID, msg.MessageID, s.user.ID)
	if err != nil {
		slog.ErrorContext(s.ctx, "error deleting message", logger.User(s.user.ID), logger.Server(msg.ServerID), logger.Channel(msg.ChannelID), "messageId", msg.MessageID, "error", err)
		s.conn.SendJSON(map[string]any{"type": "error", "message": "could not delete message"})
		return
	}
	if !deleted {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "message not found"})
		return
	}
	slog.InfoContext(s.ctx, "message deleted", logger.User(s.user.ID), logger.Server(msg.ServerID), logger.Channel(msg.ChannelID), "messageId", msg.MessageID)

	s.h.app.GetOrCreateServer(msg.ServerID).BroadcastMessage("", map[string]any{
		"type":      "message-deleted",
		"id":        msg.MessageID,
		"serverId":  msg.ServerID,
		"channelId": msg.ChannelID,
		"deletedBy": s.user.ID,
	})
}
