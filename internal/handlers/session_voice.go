package handlers

import (
	"github.com/armonic-tech/armonic-backend/internal/models/signal"
	"github.com/armonic-tech/armonic-backend/internal/models/user"
	"github.com/armonic-tech/armonic-backend/internal/transport/rtc"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
	"log/slog"

	"github.com/pion/webrtc/v3"
)

func (s *connSession) handleJoinVoice(msg signal.Message) {
	rtcConn, err := rtc.NewWebRTCConn()
	if err != nil {
		slog.ErrorContext(s.ctx, "join-voice rtc error", logger.User(s.user.ID), "error", err)
		return
	}
	if err := rtcConn.AddAudioTransceiver(); err != nil {
		slog.ErrorContext(s.ctx, "join-voice transceiver error", logger.User(s.user.ID), "error", err)
		return
	}

	rtcConn.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		c := candidate.ToJSON()
		s.conn.SendJSON(signal.Message{Type: "candidate", Candidate: &c})
	})

	u := &user.User{
		ID:             s.user.ID,
		DisplayName:    s.user.DisplayName,
		Signaling:      s.conn,
		Media:          rtcConn,
		VoiceChannelID: msg.ChannelID,
		ServerID:       msg.ServerID,
	}

	srv := s.h.app.GetOrCreateServer(msg.ServerID)
	ch := srv.GetOrCreateVoiceChannel(msg.ChannelID)
	rtcConn.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		slog.DebugContext(s.ctx, "track received", logger.User(u.ID), "kind", track.Kind())
		ch.ForwardTrack(u.ID, track)
	})

	offer, err := rtcConn.CreateOffer()
	if err != nil {
		slog.ErrorContext(s.ctx, "join-voice offer error", logger.User(s.user.ID), "error", err)
		return
	}
	if err := rtcConn.SetLocalDescription(offer); err != nil {
		slog.ErrorContext(s.ctx, "join-voice set local description error", logger.User(s.user.ID), "error", err)
		return
	}
	if err := s.conn.SendJSON(signal.Message{Type: "offer", SDP: &offer}); err != nil {
		slog.ErrorContext(s.ctx, "join-voice send offer error", logger.User(s.user.ID), "error", err)
		return
	}

	ch.AddUser(u)
	s.user = u
	slog.InfoContext(s.ctx, "user joined voice channel", logger.User(u.ID), logger.Server(msg.ServerID), logger.Channel(msg.ChannelID))
}

func (s *connSession) handleAnswer(msg signal.Message) {
	if msg.SDP == nil || s.user.Media == nil {
		return
	}
	if err := s.user.Media.SetRemoteDescription(*msg.SDP); err != nil {
		slog.ErrorContext(s.ctx, "handleAnswer error", logger.User(s.user.ID), "error", err)
	}
}

func (s *connSession) handleCandidate(msg signal.Message) {
	if msg.Candidate == nil || s.user.Media == nil {
		return
	}
	if err := s.user.Media.AddICECandidate(*msg.Candidate); err != nil {
		slog.ErrorContext(s.ctx, "handleCandidate error", logger.User(s.user.ID), "error", err)
	}
}

func (s *connSession) handleKickVoice(msg signal.Message) {
	ok, err := s.h.serverRepo.IsOwner(s.ctx, msg.ServerID, s.user.ID)
	if err != nil || !ok {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "unauthorized"})
		return
	}
	srv := s.h.app.GetOrCreateServer(msg.ServerID)
	ch := srv.GetOrCreateVoiceChannel(msg.ChannelID)
	ch.KickUser(msg.TargetUserID)
	slog.InfoContext(s.ctx, "user kicked from voice", logger.User(msg.TargetUserID), logger.Server(msg.ServerID), logger.Channel(msg.ChannelID), "by", s.user.ID)
}

func (s *connSession) handleKickServer(msg signal.Message) {
	ok, err := s.h.serverRepo.IsOwner(s.ctx, msg.ServerID, s.user.ID)
	if err != nil || !ok {
		s.conn.SendJSON(map[string]any{"type": "error", "message": "unauthorized"})
		return
	}
	if err := s.h.membershipRepo.Remove(s.ctx, msg.TargetUserID, msg.ServerID); err != nil {
		slog.ErrorContext(s.ctx, "kick-server remove membership error", logger.User(msg.TargetUserID), logger.Server(msg.ServerID), "error", err)
		return
	}
	srv := s.h.app.GetOrCreateServer(msg.ServerID)
	srv.KickUserFromAllVoice(msg.TargetUserID)
	slog.InfoContext(s.ctx, "user kicked from server", logger.User(msg.TargetUserID), logger.Server(msg.ServerID), "by", s.user.ID)
	s.conn.SendJSON(map[string]any{"type": "user-kicked", "targetUserId": msg.TargetUserID})
}

func (s *connSession) handleDisconnect() {
	if s.user.VoiceChannelID != "" {
		srv := s.h.app.GetOrCreateServer(s.user.ServerID)
		ch := srv.GetOrCreateVoiceChannel(s.user.VoiceChannelID)
		ch.RemoveUser(s.user.ID)
	}
	s.h.app.RemoveConnectedUser(s.user.ID)
	if s.user.Media != nil {
		s.user.Media.Close()
	}
	slog.InfoContext(s.ctx, "user disconnected", logger.User(s.user.ID))
}
