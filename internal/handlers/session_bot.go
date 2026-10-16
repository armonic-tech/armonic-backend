package handlers

import (
	"log/slog"

	"github.com/armonic-tech/armonic-backend/internal/models/signal"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
)

// handleSummonBot asks a bot to join a voice channel. A bot account cannot
// join voice anywhere else, so it spends nothing until an owner asks
// (docs/ai/bots.md).
func (s *connSession) handleSummonBot(msg signal.Message) {
	args := []any{logger.Server(msg.ServerID), logger.Channel(msg.ChannelID), "target_user_id", msg.TargetUserID}
	ok, err := s.h.serverRepo.IsOwner(s.ctx, msg.ServerID, s.user.ID)
	if err != nil || !ok {
		s.reject(msg.Type, "unauthorized", err, args...)
		return
	}
	if ok, err := s.isBotMember(msg.TargetUserID, msg.ServerID); err != nil || !ok {
		s.reject(msg.Type, "not a bot", err, args...)
		return
	}
	ch, err := s.h.channelRepo.GetByID(s.ctx, msg.ChannelID)
	if err != nil || ch == nil || ch.ServerID != msg.ServerID || ch.Type != "voice" {
		s.reject(msg.Type, "invalid channel", err, args...)
		return
	}

	srv := s.h.app.GetOrCreateServer(msg.ServerID)
	srv.SummonBot(msg.TargetUserID, msg.ChannelID)
	frame := map[string]any{"type": "bot-summon", "serverId": msg.ServerID, "channelId": msg.ChannelID, "requestedBy": s.user.ID}
	if srv.SendToUser(msg.TargetUserID, frame) == 0 {
		srv.DismissBot(msg.TargetUserID)
		s.reject(msg.Type, "bot offline", nil, args...)
		return
	}
	slog.InfoContext(s.ctx, "bot summoned", args...)
}

// handleDismissBot revokes the summon and ends the bot's call server side,
// so a bot that ignores bot-dismiss still loses its audio.
func (s *connSession) handleDismissBot(msg signal.Message) {
	args := []any{logger.Server(msg.ServerID), "target_user_id", msg.TargetUserID}
	ok, err := s.h.serverRepo.IsOwner(s.ctx, msg.ServerID, s.user.ID)
	if err != nil || !ok {
		s.reject(msg.Type, "unauthorized", err, args...)
		return
	}
	if ok, err := s.isBotMember(msg.TargetUserID, msg.ServerID); err != nil || !ok {
		s.reject(msg.Type, "not a bot", err, args...)
		return
	}

	srv := s.h.app.GetOrCreateServer(msg.ServerID)
	srv.DismissBot(msg.TargetUserID)
	srv.SendToUser(msg.TargetUserID, map[string]any{"type": "bot-dismiss", "serverId": msg.ServerID, "requestedBy": s.user.ID})

	kicked := false
	if chID := srv.VoiceChannelOf(msg.TargetUserID); chID != "" {
		if ch := srv.GetVoiceChannel(chID); ch != nil && ch.KickUser(msg.TargetUserID) {
			kicked = true
			ch.Broadcast(msg.TargetUserID, map[string]any{"type": "voice-leave", "userId": msg.TargetUserID, "channelId": chID})
		}
	}
	slog.InfoContext(s.ctx, "bot dismissed", append(args, "was_in_voice", kicked)...)
}

func (s *connSession) isBotMember(userID, serverID string) (bool, error) {
	ok, err := s.h.membershipRepo.IsMember(s.ctx, userID, serverID)
	if err != nil || !ok {
		return false, err
	}
	return s.h.userRepo.IsBot(s.ctx, userID)
}
