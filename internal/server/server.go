package server

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"

	"github.com/rs/cors"

	"github.com/armonic-tech/armonic-backend/config"
	"github.com/armonic-tech/armonic-backend/internal/auth"
	"github.com/armonic-tech/armonic-backend/internal/claim"
	"github.com/armonic-tech/armonic-backend/internal/handlers"
	"github.com/armonic-tech/armonic-backend/internal/media"
	"github.com/armonic-tech/armonic-backend/internal/models/app"
	repo "github.com/armonic-tech/armonic-backend/internal/repositories"
	"github.com/armonic-tech/armonic-backend/internal/transport/rtc"
	"github.com/armonic-tech/armonic-backend/pkg/logger"
	"github.com/armonic-tech/armonic-backend/pkg/pow"
	"github.com/armonic-tech/armonic-backend/pkg/ratelimit"
	"github.com/google/uuid"
	httpSwagger "github.com/swaggo/http-swagger"
)

// defaultServerSettingsKey stores the ID of the bootstrap server created on
// first run. It is the *only* server an instance ever has: one instance is one
// server, so nothing outside ensureDefaultServer calls ServerRepo.Create.
const defaultServerSettingsKey = "default_server_id"

const (
	shutdownTimeout = 10 * time.Second
	statsInterval   = 5 * time.Minute
)

type Server struct {
	addr           string
	handler        http.Handler
	allowedOrigins []string
	stats          func() []any
}

type memberChecker interface {
	IsMember(ctx context.Context, userID, serverID string) (bool, error)
	IsMemberByChannel(ctx context.Context, userID, channelID string) (bool, error)
	IsMemberByAttachment(ctx context.Context, userID, attachmentID string) (bool, error)
}

type ownerChecker interface {
	IsOwner(ctx context.Context, serverID, userID string) (bool, error)
}

type Router struct {
	mux    *http.ServeMux
	auth   *auth.Service
	member memberChecker
	owner  ownerChecker
}

func NewRouter(a *auth.Service, member memberChecker, owner ownerChecker) *Router {
	return &Router{mux: http.NewServeMux(), auth: a, member: member, owner: owner}
}

func (r *Router) Protected(pattern string, h http.HandlerFunc) {
	r.mux.Handle(pattern, r.requireJWT(h))
}

func (r *Router) Public(pattern string, h http.HandlerFunc) {
	r.mux.Handle(pattern, h)
}

func (r *Router) Member(pattern string, h http.HandlerFunc) {
	r.mux.Handle(pattern, r.requireJWT(r.requireMember(h)))
}

func (r *Router) MemberByChannel(pattern string, h http.HandlerFunc) {
	r.mux.Handle(pattern, r.requireJWT(r.requireMemberByChannel(h)))
}

func (r *Router) MemberByAttachment(pattern string, h http.HandlerFunc) {
	r.mux.Handle(pattern, r.requireJWT(r.requireMemberByAttachment(h)))
}

// the caller must own the server
func (r *Router) Owner(pattern string, h http.HandlerFunc) {
	r.mux.Handle(pattern, r.requireJWT(r.requireOwner(h)))
}

// requireMember gates on IsMember(userID, {id}); {id} is a server id
func (r *Router) requireMember(next http.Handler) http.Handler {
	return r.authorize(next, func(ctx context.Context, userID, pathID string) (bool, error) {
		return r.member.IsMember(ctx, userID, pathID)
	})
}

func (r *Router) requireMemberByChannel(next http.Handler) http.Handler {
	return r.authorize(next, func(ctx context.Context, userID, pathID string) (bool, error) {
		return r.member.IsMemberByChannel(ctx, userID, pathID)
	})
}

func (r *Router) requireMemberByAttachment(next http.Handler) http.Handler {
	return r.authorize(next, func(ctx context.Context, userID, pathID string) (bool, error) {
		return r.member.IsMemberByAttachment(ctx, userID, pathID)
	})
}

func (r *Router) requireOwner(next http.Handler) http.Handler {
	return r.authorize(next, func(ctx context.Context, userID, pathID string) (bool, error) {
		return r.owner.IsOwner(ctx, pathID, userID)
	})
}

func (r *Router) authorize(next http.Handler, check func(ctx context.Context, userID, pathID string) (bool, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		userID, _ := handlers.UserID(req.Context())
		ok, err := check(req.Context(), userID, req.PathValue("id"))
		if err != nil {
			slog.ErrorContext(req.Context(), "authorization check failed", "path_id", req.PathValue("id"), "error", err)
			http.Error(w, "authorization check failed", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, req)
	})
}

func (r *Router) requireJWT(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		token, ok := bearerToken(req)

		if !ok {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		claims, err := r.auth.Validate(req.Context(), token)
		if err != nil {
			if scope := scopeOf(req.Context()); scope != nil {
				scope.authErr = err
			}
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		if scope := scopeOf(req.Context()); scope != nil {
			scope.userID = claims.Sub
		}
		ctx := handlers.WithUserID(req.Context(), claims.Sub)
		ctx = logger.With(ctx, logger.User(claims.Sub))
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) < len(p) || !strings.EqualFold(h[:len(p)], p) {
		return "", false
	}
	return strings.TrimSpace(h[len(p):]), true
}

func New(ctx context.Context, cfg config.Config, db *sql.DB, repos *repo.Repositories) (*Server, error) {
	defaultServerID, err := ensureDefaultServer(ctx, repos, cfg)
	if err != nil {
		return nil, err
	}

	blobs, err := media.NewStore(cfg.Upload.Dir)
	if err != nil {
		return nil, err
	}
	ipKey, err := newIPKey(cfg.TrustedProxies)
	if err != nil {
		return nil, err
	}

	if err := rtc.Configure(rtc.Config{
		STUNServers: cfg.RTC.StunServers,
		UDPPort:     cfg.RTC.UDPPort,
		HostIPs:     cfg.RTC.HostIPs,
	}); err != nil {
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
		repos.Attachments(),
		authSvc,
		cfg,
	)
	uploads := handlers.NewUploads(blobs, repos.Attachments(), cfg.Upload)
	mlsHandlers := handlers.NewMLS(repos.MLS(), repos.Memberships(), repos.Servers(), appState)

	claimMgr := claim.New(cfg.ClaimPassword)
	powMgr := pow.New(cfg.Pow.Enabled, cfg.JWTSecret, cfg.Pow.MaxNumber, cfg.Pow.TTL)

	claimed := func() bool {
		v, err := repos.Settings().Get(context.Background(), "owner")
		if err != nil {
			slog.Error("could not read claim state, treating as unclaimed", "error", err)
		}
		return v != ""
	}
	memberCounter := memberCounterAdapter{repo: repos.Memberships(), serverID: defaultServerID}

	router := NewRouter(authSvc, repos.Memberships(), repos.Servers())

	// public
	router.Public("/ws", wsHandler.HandleWebSocket) // WS authenticates via its first "auth" message, not a header
	router.Public("/info", handlers.InfoHandler(cfg, memberCounter, defaultServerID, claimed))
	router.Public("GET /swagger/", httpSwagger.WrapHandler)

	if cfg.DebugPprof { // Debug only, off by default
		router.Public("GET /debug/pprof/", pprof.Index)
		slog.WarnContext(ctx, "pprof endpoints enabled do not leave this on")
	}
	router.Public("GET /pow/challenge", chain(handlers.PowChallenge(powMgr),
		perIP(powChallengePerMin, powChallengeBurst, ipKey),
	))
	router.Public("POST /claim/password", chain(handlers.ClaimPasswordHandler(claimMgr, claimed, powMgr),
		perIP(claimPerIPPerMin, claimBurst, ipKey),
		global(claimGlobalPerMin, claimBurst),
	))
	router.Public("POST /claim/register", chain(
		handlers.ClaimRegisterHandler(claimMgr, authSvc, repos.Settings(), repos.Servers(), repos.Memberships(), defaultServerID, claimed),
		perIP(claimPerIPPerMin, claimBurst, ipKey),
		global(claimGlobalPerMin, claimBurst),
	))
	router.Public("POST /auth/login", chain(
		handlers.LoginHandler(authSvc, claimed, ratelimit.New(loginPerAccountPerMin, loginBurst), powMgr),
		perIP(loginPerIPPerMin, loginBurst, ipKey),
	))
	router.Public("POST /invite/signup", chain(
		handlers.InviteSignupHandler(repos.Invites(), authSvc, repos.Memberships(), claimed, powMgr),
		perIP(signupPerIPPerMin, signupBurst, ipKey),
	))
	router.Public("GET /invite/status", chain(handlers.InviteStatusHandler(repos.Invites()),
		perIP(signupPerIPPerMin, signupBurst, ipKey),
	))

	// protected
	// JWT only
	router.Protected("GET /server", handlers.GetMyServers(repos.Memberships(), repos.Servers()))
	router.Protected("GET /me", handlers.GetMe(repos.Users()))
	router.Protected("POST /me/avatar", uploads.Avatar(repos.Users(), defaultServerID))
	router.Protected("GET /me/storage", uploads.Storage())
	router.Protected("POST /me/mls/device", mlsHandlers.RegisterDevice())
	router.Protected("POST /me/mls/keypackages", mlsHandlers.PublishKeyPackages())
	// JWT + membership
	router.Member("GET /server/{id}", handlers.GetByServer(repos.Channels()))
	router.Member("GET /server/{id}/members", handlers.GetServerMembers(repos.Memberships(), appState))
	router.Member("POST /server/{id}/upload", uploads.Upload())
	router.Member("POST /server/{id}/upload/encrypted", uploads.UploadEncrypted())
	router.Member("GET /server/{id}/mls", mlsHandlers.State())
	router.Member("GET /server/{id}/mls/commits", mlsHandlers.Commits())
	router.Member("POST /server/{id}/mls/commit", mlsHandlers.Commit())
	router.Member("POST /server/{id}/mls/welcome/ack", mlsHandlers.AckWelcome())
	router.MemberByChannel("GET /channel/{id}", handlers.GetChannelByID(repos.Channels(), appState))
	router.MemberByChannel("GET /channel/{id}/messages", handlers.GetChannelMessages(repos.Channels(), repos.Messages()))
	router.MemberByAttachment("GET /attachment/{id}", uploads.Serve(media.VariantFull))
	router.MemberByAttachment("GET /attachment/{id}/thumb", uploads.Serve(media.VariantThumb))
	// JWT + ownership
	router.Owner("POST /server/{id}/invite", handlers.CreateInvite(repos.Invites(), cfg.BaseURL()))

	stats := func() []any {
		servers, connected, voice := appState.Stats()
		pool := db.Stats()
		return []any{
			"servers", servers,
			"ws_users", connected,
			"voice_users", voice,
			"db_open", pool.OpenConnections,
			"db_in_use", pool.InUse,
			"db_wait_count", pool.WaitCount,
			"db_wait_ms", pool.WaitDuration.Milliseconds(),
		}
	}

	return &Server{
		addr:           ":" + cfg.Port,
		handler:        requestLog(router.mux, ipKey),
		allowedOrigins: cfg.AllowedOrigins,
		stats:          stats,
	}, nil
}

// Run serves until ctx is cancelled, then drains in-flight HTTP requests for
// up to shutdownTimeout. Hijacked WebSockets are not tracked by Shutdown and
// die with the process.
func (s *Server) Run(ctx context.Context) error {
	c := cors.New(cors.Options{
		AllowedOrigins:   s.allowedOrigins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: len(s.allowedOrigins) > 0,
	})
	srv := &http.Server{
		Addr:     s.addr,
		Handler:  c.Handler(s.handler),
		ErrorLog: slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}

	statsCtx, stopStats := context.WithCancel(ctx)
	defer stopStats()
	go logger.RuntimeStats(statsCtx, statsInterval, s.stats)

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down http server", "timeout", shutdownTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
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
	slog.InfoContext(ctx, "first run: creating default server", logger.Server(id))
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
