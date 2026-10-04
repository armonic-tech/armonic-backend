package handlers

import (
	"context"
	"log/slog"
	"time"

	"github.com/armonic-tech/armonic-backend/internal/models/channel"
	"github.com/armonic-tech/armonic-backend/internal/models/server"
	"github.com/armonic-tech/armonic-backend/internal/models/signal"
	"github.com/armonic-tech/armonic-backend/internal/models/user"
	"github.com/armonic-tech/armonic-backend/internal/transport/rtc"
	"github.com/armonic-tech/armonic-backend/pkg/logger"

	"github.com/pion/webrtc/v3"
)

func (s *connSession) handleJoinVoice(msg signal.Message) {
	vctx := logger.With(s.ctx, logger.Server(msg.ServerID), logger.Channel(msg.ChannelID))

	rtcConn, err := rtc.NewWebRTCConn()
	if err != nil {
		slog.ErrorContext(vctx, "join-voice rtc error", "error", err)
		return
	}
	if err := rtcConn.AddAudioTransceiver(); err != nil {
		slog.ErrorContext(vctx, "join-voice transceiver error", "error", err)
		rtcConn.Close()
		return
	}

	rtcConn.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		defer logger.Recover(vctx, "rtc OnICECandidate")
		if candidate == nil {
			slog.DebugContext(vctx, "voice: local ice gathering complete")
			return
		}
		slog.DebugContext(vctx, "voice: local candidate", "type", candidate.Typ.String(), "protocol", candidate.Protocol.String())
		c := candidate.ToJSON()
		s.send(signal.Message{Type: "candidate", Candidate: &c})
	})

	u := &user.User{
		ID:             s.user.ID,
		DisplayName:    s.user.DisplayName,
		AvatarID:       s.user.AvatarID,
		Signaling:      s.conn,
		Media:          rtcConn,
		VoiceChannelID: msg.ChannelID,
		ServerID:       msg.ServerID,
	}
	u.SetVoiceState(s.user.IsMuted(), s.user.IsDeafened())

	srv := s.h.app.GetOrCreateServer(msg.ServerID)
	ch := srv.GetOrCreateVoiceChannel(msg.ChannelID)
	rtcConn.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		defer logger.Recover(vctx, "rtc OnTrack")
		slog.InfoContext(vctx, "voice: track received", "kind", track.Kind().String(),
			"codec", track.Codec().MimeType, "ssrc", uint32(track.SSRC()))
		ch.ForwardTrack(u, track)
	})

	rtcConn.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		slog.DebugContext(vctx, "voice: ice connection state", "state", state.String())
	})

	joined := time.Now()
	rtcConn.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		defer logger.Recover(vctx, "rtc OnConnectionStateChange")
		level := slog.LevelInfo
		if state == webrtc.PeerConnectionStateFailed {
			level = slog.LevelWarn
		}
		args := []any{"state", state.String(), "since_join", time.Since(joined).Round(time.Millisecond).String()}
		if state == webrtc.PeerConnectionStateConnected || state == webrtc.PeerConnectionStateFailed {
			args = append(args, "selected_pair", rtcConn.SelectedCandidatePair())
		}
		slog.Log(vctx, level, "voice: peer connection state", args...)

		switch state {
		case webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateClosed,
			webrtc.PeerConnectionStateDisconnected:
			srv.ReleaseVoice(u)
			if ch.RemoveUserIf(u.ID, u) {
				ch.Broadcast(u.ID, map[string]any{
					"type":      "voice-leave",
					"userId":    u.ID,
					"channelId": msg.ChannelID,
				})
				slog.InfoContext(vctx, "voice presence dropped on rtc state change", "state", state.String())
			}
			rtcConn.Close()
		}
	})

	offer, err := rtcConn.CreateOffer()
	if err != nil {
		slog.ErrorContext(vctx, "join-voice offer error", "error", err)
		rtcConn.Close()
		return
	}
	if err := rtcConn.SetLocalDescription(offer); err != nil {
		slog.ErrorContext(vctx, "join-voice set local description error", "error", err)
		rtcConn.Close()
		return
	}
	if err := s.conn.SendJSON(signal.Message{Type: "offer", SDP: &offer}); err != nil {
		slog.ErrorContext(vctx, "join-voice send offer error", "error", err)
		rtcConn.Close()
		return
	}

	prev, replaced, evicted := srv.JoinVoice(server.VoiceSession{User: u, ChannelID: msg.ChannelID, Media: rtcConn}, ch)
	s.user = u
	if replaced {
		s.endReplacedVoice(vctx, srv, prev, evicted, msg.ChannelID)
	}
	slog.InfoContext(vctx, "user joined voice channel", "offer", channel.SDPSummary(offer.SDP),
		"muted", u.IsMuted(), "deafened", u.IsDeafened())
}

// endReplacedVoice closes the call a user just left by joining again, from
// this device or another one (docs/ai/ws-protocol.md, "One call per user").
func (s *connSession) endReplacedVoice(ctx context.Context, srv *server.Server, prev server.VoiceSession, evicted bool, newChannelID string) {
	sameConn := prev.User.Signaling == s.conn
	if prev.Media != nil {
		prev.Media.Close()
	}
	if evicted && prev.ChannelID != newChannelID {
		if pc := srv.GetVoiceChannel(prev.ChannelID); pc != nil {
			pc.Broadcast(prev.User.ID, map[string]any{
				"type":      "voice-leave",
				"userId":    prev.User.ID,
				"channelId": prev.ChannelID,
			})
		}
	}
	if evicted && !sameConn {
		if err := prev.User.Signaling.SendJSON(map[string]any{"type": "kicked-voice", "reason": "joined-elsewhere"}); err != nil {
			slog.WarnContext(ctx, "voice: could not notify the replaced device", "error", err)
		}
	}
	slog.InfoContext(ctx, "voice: previous session replaced", "previous_channel_id", prev.ChannelID,
		"same_connection", sameConn, "was_present", evicted)
}

func (s *connSession) handleAnswer(msg signal.Message) {
	if msg.SDP == nil || s.user.Media == nil {
		slog.WarnContext(s.ctx, "voice: answer dropped",
			"sdp_nil", msg.SDP == nil, "media_nil", s.user.Media == nil)
		return
	}
	if err := s.user.Media.SetRemoteDescription(*msg.SDP); err != nil {
		slog.ErrorContext(s.ctx, "handleAnswer error", logger.Channel(s.user.VoiceChannelID),
			"answer", channel.SDPSummary(msg.SDP.SDP), "error", err)
		return
	}
	slog.DebugContext(s.ctx, "voice: answer applied", logger.Channel(s.user.VoiceChannelID),
		"answer", channel.SDPSummary(msg.SDP.SDP))
	if s.user.VoiceChannelID == "" {
		return
	}
	srv := s.h.app.GetOrCreateServer(s.user.ServerID)
	ch := srv.GetOrCreateVoiceChannel(s.user.VoiceChannelID)

	u := s.user
	logger.Go(s.ctx, "voice AttachPending", func() { ch.AttachPending(u) })
}

func (s *connSession) handleCandidate(msg signal.Message) {
	if msg.Candidate == nil || s.user.Media == nil {
		slog.DebugContext(s.ctx, "voice: candidate dropped", "candidate_nil", msg.Candidate == nil, "media_nil", s.user.Media == nil)
		return
	}
	if err := s.user.Media.AddICECandidate(*msg.Candidate); err != nil {
		slog.ErrorContext(s.ctx, "handleCandidate error", logger.Channel(s.user.VoiceChannelID), "candidate", msg.Candidate.Candidate, "error", err)
	}
}

func (s *connSession) handleVoiceState(msg signal.Message) {
	muted, deafened := s.user.SetVoiceState(msg.Muted, msg.Deafened)
	if s.user.VoiceChannelID == "" {
		slog.DebugContext(s.ctx, "voice state stored outside a voice channel", "muted", muted, "deafened", deafened)
		return
	}
	srv := s.h.app.GetOrCreateServer(s.user.ServerID)
	ch := srv.GetOrCreateVoiceChannel(s.user.VoiceChannelID)
	ch.Broadcast(s.user.ID, map[string]any{
		"type":      "voice-state",
		"userId":    s.user.ID,
		"channelId": s.user.VoiceChannelID,
		"muted":     muted,
		"deafened":  deafened,
	})
	slog.InfoContext(s.ctx, "voice state updated", logger.Channel(s.user.VoiceChannelID), "muted", muted, "deafened", deafened)
}

func (s *connSession) handleKickVoice(msg signal.Message) {
	ok, err := s.h.serverRepo.IsOwner(s.ctx, msg.ServerID, s.user.ID)
	if err != nil || !ok {
		s.reject(msg.Type, "unauthorized", err, logger.Server(msg.ServerID), "target_user_id", msg.TargetUserID)
		return
	}
	srv := s.h.app.GetOrCreateServer(msg.ServerID)
	ch := srv.GetOrCreateVoiceChannel(msg.ChannelID)
	kicked := ch.KickUser(msg.TargetUserID)
	slog.InfoContext(s.ctx, "user kicked from voice", "target_user_id", msg.TargetUserID, logger.Server(msg.ServerID), logger.Channel(msg.ChannelID), "was_present", kicked)
}

func (s *connSession) handleKickServer(msg signal.Message) {
	ok, err := s.h.serverRepo.IsOwner(s.ctx, msg.ServerID, s.user.ID)
	if err != nil || !ok {
		s.reject(msg.Type, "unauthorized", err, logger.Server(msg.ServerID), "target_user_id", msg.TargetUserID)
		return
	}
	if err := s.h.membershipRepo.Remove(s.ctx, msg.TargetUserID, msg.ServerID); err != nil {
		slog.ErrorContext(s.ctx, "kick-server remove membership error", "target_user_id", msg.TargetUserID, logger.Server(msg.ServerID), "error", err)
		return
	}
	srv := s.h.app.GetOrCreateServer(msg.ServerID)
	srv.KickUserFromAllVoice(msg.TargetUserID)
	conns := srv.RemoveUser(msg.TargetUserID)
	slog.InfoContext(s.ctx, "user kicked from server", "target_user_id", msg.TargetUserID, logger.Server(msg.ServerID), "connections_dropped", conns)
	s.send(map[string]any{"type": "user-kicked", "targetUserId": msg.TargetUserID})
	srv.BroadcastMessage(nil, map[string]any{"type": "mls-sync", "serverId": msg.ServerID})
}

func (s *connSession) leaveVoice() {
	if s.user.VoiceChannelID == "" {
		return
	}
	channelID := s.user.VoiceChannelID
	srv := s.h.app.GetOrCreateServer(s.user.ServerID)
	srv.ReleaseVoice(s.user)
	ch := srv.GetOrCreateVoiceChannel(channelID)
	removed := ch.RemoveUserIf(s.user.ID, s.user)
	s.user.VoiceChannelID = ""
	if s.user.Media != nil {
		s.user.Media.Close()
		s.user.Media = nil
	}
	if removed {
		ch.Broadcast(s.user.ID, map[string]any{
			"type":      "voice-leave",
			"userId":    s.user.ID,
			"channelId": channelID,
		})
	}
	slog.InfoContext(s.ctx, "user left voice", logger.Channel(channelID), "was_present", removed)
}

func (s *connSession) handleLeaveVoice() {
	if s.user.VoiceChannelID == "" {
		slog.DebugContext(s.ctx, "leave-voice outside a voice channel")
	}
	s.leaveVoice()
}

func (s *connSession) handleDisconnect() {
	s.leaveVoice()
	s.h.app.RemoveConnection(s.user.ID, s.conn)
	slog.InfoContext(s.ctx, "user disconnected")
}
