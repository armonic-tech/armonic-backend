package server

import (
	"context"
	"net/http"

	"github.com/armonic-tech/armonic-backend/config"
	"github.com/armonic-tech/armonic-backend/internal/auth"
	"github.com/armonic-tech/armonic-backend/internal/claim"
	"github.com/armonic-tech/armonic-backend/internal/handlers"
	"github.com/armonic-tech/armonic-backend/internal/models/app"
	repo "github.com/armonic-tech/armonic-backend/internal/repositories"

	"github.com/google/uuid"
)

// defaultServerSettingsKey stores the ID of the single bootstrap server
// created on first run. handleCreateServer (session_server.go) lets an
// authenticated owner create further servers beyond this one.
const defaultServerSettingsKey = "default_server_id"

type Server struct {
	addr string
	mux  *http.ServeMux
}

func New(ctx context.Context, cfg config.Config, repos *repo.Repositories) (*Server, error) {
	defaultServerID, err := ensureDefaultServer(ctx, repos, cfg)
	if err != nil {
		return nil, err
	}

	authSvc := auth.NewService(cfg.JWTSecret, repos.Users())
	appState := app.NewApp()

	wsHandler := handlers.NewWSHandler(
		appState,
		repos.Messages(),
		repos.Memberships(),
		repos.Servers(),
		repos.Channels(),
		repos.Invites(),
		repos.Users(),
		authSvc,
		cfg.BaseURL(),
		cfg,
	)

	claimMgr := claim.New(cfg.ClaimPassword)

	claimed := func() bool {
		v, _ := repos.Settings().Get(context.Background(), "owner")
		return v != ""
	}
	memberCounter := memberCounterAdapter{repo: repos.Memberships(), serverID: defaultServerID}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsHandler.HandleWebSocket)
	mux.HandleFunc("/info", handlers.InfoHandler(cfg, memberCounter, defaultServerID, claimed))
	mux.HandleFunc("/claim/password", handlers.ClaimPasswordHandler(claimMgr, claimed))
	mux.HandleFunc("/claim/register", handlers.ClaimRegisterHandler(claimMgr, authSvc, repos.Settings(), repos.Servers(), repos.Memberships(), defaultServerID, claimed))
	mux.HandleFunc("/auth/login", handlers.LoginHandler(authSvc, claimed))
	mux.HandleFunc("/invite/signup", handlers.InviteSignupHandler(repos.Invites(), authSvc, repos.Memberships(), claimed))
	mux.HandleFunc("GET /invite/status", handlers.InviteStatusHandler(repos.Invites()))

	return &Server{addr: ":" + cfg.Port, mux: mux}, nil
}

func (s *Server) ListenAndServe() error {
	return http.ListenAndServe(s.addr, s.mux)
}

// memberCounterAdapter binds handlers.MemberCounter (a no-arg CountAll) to a
// specific server, since /info always reports on the single default server.
type memberCounterAdapter struct {
	repo     *repo.MembershipRepo
	serverID string
}

func (m memberCounterAdapter) CountAll(ctx context.Context) (int, error) {
	return m.repo.CountByServer(ctx, m.serverID)
}

func ensureDefaultServer(ctx context.Context, repos *repo.Repositories, cfg config.Config) (string, error) {
	id, err := repos.Settings().Get(ctx, defaultServerSettingsKey)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}

	id = uuid.New().String()
	if err := repos.Servers().Create(ctx, id, cfg.ServerName, ""); err != nil {
		return "", err
	}
	if err := repos.Channels().Create(ctx, uuid.New().String(), id, "general", "text"); err != nil {
		return "", err
	}
	if err := repos.Channels().Create(ctx, uuid.New().String(), id, "General", "voice"); err != nil {
		return "", err
	}
	if err := repos.Settings().Set(ctx, defaultServerSettingsKey, id); err != nil {
		return "", err
	}
	return id, nil
}
