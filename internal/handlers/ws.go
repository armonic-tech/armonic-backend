package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/armonic-tech/armonic-backend/config"
	"github.com/armonic-tech/armonic-backend/internal/auth"
	"github.com/armonic-tech/armonic-backend/internal/models/app"
	"github.com/armonic-tech/armonic-backend/internal/models/channel"
	"github.com/armonic-tech/armonic-backend/internal/models/invite"
	"github.com/armonic-tech/armonic-backend/internal/models/message"
	"github.com/armonic-tech/armonic-backend/internal/models/signal"
	"github.com/armonic-tech/armonic-backend/internal/models/user"
	"github.com/armonic-tech/armonic-backend/internal/transport/ws"
	"github.com/armonic-tech/armonic-backend/pkg/logger"

	"github.com/gorilla/websocket"
)

func newUpgrader(allowedOrigins []string) websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" || len(allowedOrigins) == 0 {
				return true
			}
			if slices.Contains(allowedOrigins, origin) {
				return true
			}
			slog.WarnContext(r.Context(), "ws: origin rejected", "origin", origin)
			return false
		},
	}
}

const slowFrame = time.Second

type MessageRepo interface {
	Save(ctx context.Context, msg message.Message) error
	SoftDelete(ctx context.Context, serverID, channelID, messageID, deletedBy string) (bool, error)
}

type MembershipRepo interface {
	Add(ctx context.Context, userID, serverID string) error
	Remove(ctx context.Context, userID, serverID string) error
	IsMember(ctx context.Context, userID, serverID string) (bool, error)
	GetByUser(ctx context.Context, userID string) ([]string, error)
}

type ServerRepo interface {
	Create(ctx context.Context, id, name, ownerID string) error
	IsOwner(ctx context.Context, serverID, userID string) (bool, error)
}

type ChannelRepo interface {
	Create(ctx context.Context, id, serverID, name, chType string) error
	NameTaken(ctx context.Context, serverID, chType, name string) (bool, error)
	GetByID(ctx context.Context, id string) (*channel.ChannelInfo, error)
	GetChannelByServer(ctx context.Context, serverID string) ([]channel.ChannelInfo, error)
	SoftDelete(ctx context.Context, serverID, channelID string) (bool, error)
}

type InviteRepo interface {
	Get(ctx context.Context, token string) (*invite.Invite, error)
	MarkUsed(ctx context.Context, token string) error
}

type Authenticator interface {
	Validate(ctx context.Context, token string) (*auth.Claims, error)
}

type UserRepo interface {
	Upsert(ctx context.Context, id, displayName string) error
	GetName(ctx context.Context, id string) (string, error)
	GetProfile(ctx context.Context, id string) (displayName, avatarID string, err error)
	IsBot(ctx context.Context, id string) (bool, error)
}

type WSHandler struct {
	app            *app.App
	messageRepo    MessageRepo
	membershipRepo MembershipRepo
	serverRepo     ServerRepo
	channelRepo    ChannelRepo
	inviteRepo     InviteRepo
	userRepo       UserRepo
	attachmentRepo AttachmentRepository
	auth           Authenticator
	cfg            config.Config
	upgrader       websocket.Upgrader
}

func NewWSHandler(a *app.App, msg MessageRepo, membership MembershipRepo, server ServerRepo, ch ChannelRepo, inv InviteRepo, users UserRepo, att AttachmentRepository, v Authenticator, cfg config.Config) *WSHandler {
	return &WSHandler{
		app:            a,
		messageRepo:    msg,
		membershipRepo: membership,
		serverRepo:     server,
		channelRepo:    ch,
		inviteRepo:     inv,
		userRepo:       users,
		attachmentRepo: att,
		auth:           v,
		cfg:            cfg,
		upgrader:       newUpgrader(cfg.AllowedOrigins),
	}
}

type connSession struct {
	h    *WSHandler
	conn *ws.WSConn
	user *user.User
	ctx  context.Context
}

// HandleWebSocket runs the per-connection read loop
// the first message must be "auth".
// reference: docs/ai/ws-protocol.md, docs/ai/asyncapi.yaml.
func (h *WSHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.WarnContext(r.Context(), "ws upgrade error", "error", err)
		return
	}

	wsConn := ws.NewWSConn(conn)
	defer wsConn.Close()

	ctx := logger.With(r.Context(), "conn_id", newConnID())
	s := &connSession{h: h, conn: wsConn, ctx: ctx}
	started := time.Now()
	frames := 0
	reason := "closed"

	slog.InfoContext(ctx, "ws: connected", "remote_addr", r.RemoteAddr, "origin", r.Header.Get("Origin"), "user_agent", r.UserAgent())

	defer func() {
		if v := recover(); v != nil {
			logger.LogPanic(s.ctx, "ws session", v)
			reason = "panic"
			if s.user != nil {
				s.handleDisconnect()
			}
		}
		slog.InfoContext(s.ctx, "ws: closed", "reason", reason, "authenticated", s.user != nil,
			"frames", frames, "duration", time.Since(started).Round(time.Millisecond).String())
	}()

	for {
		var msg signal.Message
		if err := s.conn.ReadJSON(&msg); err != nil {
			reason = err.Error()
			if !websocket.IsCloseError(err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure,
			) {
				slog.WarnContext(s.ctx, "ws read error", "error", err)
			}
			if s.user != nil {
				s.handleDisconnect()
			}
			break
		}
		frames++

		slog.DebugContext(s.ctx, "ws: frame", "type", msg.Type)

		if s.user == nil && msg.Type != "auth" {
			slog.WarnContext(s.ctx, "ws: frame before auth", "type", msg.Type)
			s.send(map[string]any{"type": "error", "message": "unauthorized"})
			reason = "unauthenticated frame"
			break
		}

		start := time.Now()
		switch msg.Type {
		case "auth":
			if err := s.handleAuth(msg); err != nil {
				break
			}

		case "join-voice":
			s.handleJoinVoice(msg)
		case "answer":
			s.handleAnswer(msg)
		case "candidate":
			s.handleCandidate(msg)
		case "voice-state":
			s.handleVoiceState(msg)
		case "leave-voice":
			s.handleLeaveVoice()
		case "join-server":
			s.handleJoinServer(msg)
		case "text-message":
			s.handleTextMessage(msg)
		case "delete-message":
			s.handleDeleteMessage(msg)
		case "create-channel":
			s.handleCreateChannel(msg)
		case "delete-channel":
			s.handleDeleteChannel(msg)
		case "kick-voice":
			s.handleKickVoice(msg)
		case "kick-server":
			s.handleKickServer(msg)
		case "summon-bot":
			s.handleSummonBot(msg)
		case "dismiss-bot":
			s.handleDismissBot(msg)
		default:
			slog.WarnContext(s.ctx, "ws: unknown frame type ignored", "type", msg.Type)
		}
		if d := time.Since(start); d > slowFrame {
			slog.WarnContext(s.ctx, "ws: slow frame", "type", msg.Type, "duration", d.Round(time.Millisecond).String())
		}
	}
}

func (s *connSession) send(v any) {
	if err := s.conn.SendJSON(v); err != nil {
		slog.WarnContext(s.ctx, "ws: send failed", "error", err)
	}
}

// reject answers a frame with an error and logs why: Warn for a refusal,
// Error when err says something actually broke.
func (s *connSession) reject(frame, clientMsg string, err error, args ...any) {
	s.rejectWith(frame, clientMsg, nil, err, args...)
}

// rejectWith is reject plus fields on the error frame, for a client that must
// know which of its requests failed.
func (s *connSession) rejectWith(frame, clientMsg string, fields map[string]any, err error, args ...any) {
	level := slog.LevelWarn
	if err != nil {
		level = slog.LevelError
		args = append(args, "error", err)
	}
	slog.Log(s.ctx, level, "ws: "+frame+" rejected", append([]any{"reason", clientMsg}, args...)...)
	reply := map[string]any{"type": "error", "message": clientMsg}
	for k, v := range fields {
		reply[k] = v
	}
	s.send(reply)
}

func newConnID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
