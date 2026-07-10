package handlers

import (
	"context"
	"github.com/armonic-tech/armonic-backend/config"
	"github.com/armonic-tech/armonic-backend/internal/auth"
	"github.com/armonic-tech/armonic-backend/internal/models/app"
	"github.com/armonic-tech/armonic-backend/internal/models/channel"
	"github.com/armonic-tech/armonic-backend/internal/models/invite"
	"github.com/armonic-tech/armonic-backend/internal/models/message"
	"github.com/armonic-tech/armonic-backend/internal/models/server"
	"github.com/armonic-tech/armonic-backend/internal/models/signal"
	"github.com/armonic-tech/armonic-backend/internal/models/user"
	"github.com/armonic-tech/armonic-backend/internal/transport/ws"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type MessageRepo interface {
	Save(ctx context.Context, msg message.Message) error
	GetByChannel(ctx context.Context, serverID, channelID string, limit int) ([]message.Message, error)
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
	GetByIDs(ctx context.Context, ids []string) ([]server.ServerInfo, error)
}

type ChannelRepo interface {
	Create(ctx context.Context, id, serverID, name, chType string) error
	GetByServer(ctx context.Context, serverID string) ([]channel.ChannelInfo, error)
}

type InviteRepo interface {
	Create(ctx context.Context, serverID, createdBy string, expiresIn time.Duration) (string, error)
	Get(ctx context.Context, token string) (*invite.Invite, error)
	MarkUsed(ctx context.Context, token string) error
}

type Authenticator interface {
	Validate(ctx context.Context, token string) (*auth.Claims, error)
}

type UserRepo interface {
	Upsert(ctx context.Context, id, displayName string) error
	GetName(ctx context.Context, id string) (string, error)
}

type WSHandler struct {
	app            *app.App
	messageRepo    MessageRepo
	membershipRepo MembershipRepo
	serverRepo     ServerRepo
	channelRepo    ChannelRepo
	inviteRepo     InviteRepo
	userRepo       UserRepo
	auth           Authenticator
	baseURL        string
	cfg            config.Config
}

func NewWSHandler(a *app.App, msg MessageRepo, membership MembershipRepo, server ServerRepo, ch ChannelRepo, inv InviteRepo, users UserRepo, v Authenticator, baseURL string, cfg config.Config) *WSHandler {
	return &WSHandler{
		app:            a,
		messageRepo:    msg,
		membershipRepo: membership,
		serverRepo:     server,
		channelRepo:    ch,
		inviteRepo:     inv,
		userRepo:       users,
		auth:           v,
		baseURL:        baseURL,
		cfg:            cfg,
	}
}

type connSession struct {
	h    *WSHandler
	conn *ws.WSConn
	user *user.User
	ctx  context.Context
}

func (h *WSHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.ErrorContext(r.Context(), "ws upgrade error", "error", err)
		return
	}

	wsConn := ws.NewWSConn(conn)
	defer wsConn.Close()

	s := &connSession{h: h, conn: wsConn, ctx: r.Context()}

	for {
		var msg signal.Message
		if err := s.conn.ReadJSON(&msg); err != nil {
			if !websocket.IsCloseError(err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure,
			) {
				slog.ErrorContext(s.ctx, "ws read error", "error", err)
			}
			if s.user != nil {
				s.handleDisconnect()
			}
			break
		}

		if s.user == nil && msg.Type != "auth" {
			s.conn.SendJSON(map[string]any{"type": "error", "message": "unauthorized"})
			break
		}

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
		case "create-server":
			s.handleCreateServer(msg)
		case "create-invite":
			s.handleCreateInvite(msg)
		case "join-server":
			s.handleJoinServer(msg)
		case "text-message":
			s.handleTextMessage(msg)
		case "get-servers":
			s.handleGetServers()
		case "get-channels":
			s.handleGetChannels(msg)
		case "kick-voice":
			s.handleKickVoice(msg)
		case "kick-server":
			s.handleKickServer(msg)
		case "get-messages":
			s.handleGetMessages(msg)
		}
	}
}
