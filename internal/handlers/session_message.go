package handlers

import (
	"log/slog"
	"time"

	"github.com/armonic-tech/armonic-backend/internal/models/message"
	"github.com/armonic-tech/armonic-backend/internal/models/signal"
	"github.com/armonic-tech/armonic-backend/pkg/logger"

	"github.com/google/uuid"
)

func (s *connSession) handleTextMessage(msg signal.Message) {
	ok, err := s.h.membershipRepo.IsMember(s.ctx, s.user.ID, msg.ServerID)
	if err != nil || !ok {
		s.reject(msg.Type, "unauthorized", err, logger.Server(msg.ServerID))
		return
	}
	if len(msg.Content) > s.contentCap(msg.Encrypted) || (len(msg.Content) == 0 && msg.AttachmentID == "") {
		s.reject(msg.Type, "message content invalid", nil, logger.Server(msg.ServerID), logger.Channel(msg.ChannelID),
			"length", len(msg.Content), "cap", s.contentCap(msg.Encrypted), "encrypted", msg.Encrypted)
		return
	}
	if msg.AttachmentID != "" && !s.ownsAttachment(msg.AttachmentID, msg.ServerID) {
		s.reject(msg.Type, "invalid attachment", nil, logger.Server(msg.ServerID), "attachment_id", msg.AttachmentID)
		return
	}
	m := message.Message{
		ID:           uuid.New().String(),
		ChannelID:    msg.ChannelID,
		ServerID:     msg.ServerID,
		UserID:       s.user.ID,
		Content:      msg.Content,
		AttachmentID: msg.AttachmentID,
		Encrypted:    msg.Encrypted,
		CreatedAt:    time.Now(),
	}
	if err := s.h.messageRepo.Save(s.ctx, m); err != nil {
		slog.ErrorContext(s.ctx, "error saving message", logger.Server(msg.ServerID), logger.Channel(msg.ChannelID), "error", err)
		return
	}
	slog.DebugContext(s.ctx, "message saved", logger.Server(m.ServerID), logger.Channel(m.ChannelID), "message_id", m.ID,
		"encrypted", m.Encrypted, "has_attachment", m.AttachmentID != "")
	if msg.ClientMsgID != "" {
		s.send(map[string]any{
			"type":        "message-ack",
			"clientMsgId": msg.ClientMsgID,
			"id":          m.ID,
			"serverId":    m.ServerID,
			"channelId":   m.ChannelID,
			"createdAt":   m.CreatedAt,
		})
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
	if m.AttachmentID != "" {
		broadcast["attachmentId"] = m.AttachmentID
	}
	if m.Encrypted {
		broadcast["encrypted"] = true
	}
	s.h.app.GetOrCreateServer(msg.ServerID).BroadcastMessage(s.user.ID, broadcast)
}

// An MLS envelope wraps the text plus an attachment pointer, AEAD tag and
// base64 — the plaintext limit still applies inside, this is only the
// ciphertext ceiling (docs/ai/e2ee.md).
func (s *connSession) contentCap(encrypted bool) int {
	if encrypted {
		return 2*s.h.cfg.MaxMsgLen + 1024
	}
	return s.h.cfg.MaxMsgLen
}

func (s *connSession) ownsAttachment(attachmentID, serverID string) bool {
	att, err := s.h.attachmentRepo.GetByID(s.ctx, attachmentID)
	if err != nil {
		slog.ErrorContext(s.ctx, "error loading attachment", "attachment_id", attachmentID, "error", err)
		return false
	}
	return att != nil && att.ServerID == serverID && att.UserID == s.user.ID
}

func (s *connSession) handleDeleteMessage(msg signal.Message) {
	ok, err := s.h.serverRepo.IsOwner(s.ctx, msg.ServerID, s.user.ID)
	if err != nil || !ok {
		s.reject(msg.Type, "unauthorized", err, logger.Server(msg.ServerID))
		return
	}
	if msg.MessageID == "" || msg.ChannelID == "" {
		s.reject(msg.Type, "message id invalid", nil)
		return
	}
	deleted, err := s.h.messageRepo.SoftDelete(s.ctx, msg.ServerID, msg.ChannelID, msg.MessageID, s.user.ID)
	if err != nil {
		s.reject(msg.Type, "could not delete message", err, logger.Server(msg.ServerID), logger.Channel(msg.ChannelID), "message_id", msg.MessageID)
		return
	}
	if !deleted {
		s.reject(msg.Type, "message not found", nil, logger.Server(msg.ServerID), logger.Channel(msg.ChannelID), "message_id", msg.MessageID)
		return
	}
	slog.InfoContext(s.ctx, "message deleted", logger.Server(msg.ServerID), logger.Channel(msg.ChannelID), "message_id", msg.MessageID)

	s.h.app.GetOrCreateServer(msg.ServerID).BroadcastMessage("", map[string]any{
		"type":      "message-deleted",
		"id":        msg.MessageID,
		"serverId":  msg.ServerID,
		"channelId": msg.ChannelID,
		"deletedBy": s.user.ID,
	})
}
