package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/rs/cors"

	"github.com/armonic-tech/armonic-backend/config"
	"github.com/armonic-tech/armonic-backend/internal/auth"
	"github.com/armonic-tech/armonic-backend/internal/claim"
	"github.com/armonic-tech/armonic-backend/internal/handlers"
	"github.com/armonic-tech/armonic-backend/internal/media"
	"github.com/armonic-tech/armonic-backend/internal/models/app"
	repo "github.com/armonic-tech/armonic-backend/internal/repositories"
	"github.com/armonic-tech/armonic-backend/pkg/pow"
	"github.com/armonic-tech/armonic-backend/pkg/ratelimit"
	"github.com/google/uuid"
	httpSwagger "github.com/swaggo/http-swagger"
)

// defaultServerSettingsKey stores the ID of the bootstrap server created on
// first run. It is the *only* server an instance ever has: one instance is one
// server, so nothing outside ensureDefaultServer calls ServerRepo.Create.
const defaultServerSettingsKey = "default_server_id"

type Server struct {
	addr           string
	mux            *http.ServeMux
	allowedOrigins []string
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
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		ctx := handlers.WithUserID(req.Context(), claims.Sub)
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

func New(ctx context.Context, cfg config.Config, repos *repo.Repositories) (*Server, error) {
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

	claimMgr := claim.New(cfg.ClaimPassword)
	powMgr := pow.New(cfg.Pow.Enabled, cfg.JWTSecret, cfg.Pow.MaxNumber, cfg.Pow.TTL)

	claimed := func() bool {
		v, _ := repos.Settings().Get(context.Background(), "owner")
		return v != ""
	}
	memberCounter := memberCounterAdapter{repo: repos.Memberships(), serverID: defaultServerID}

	router := NewRouter(authSvc, repos.Memberships(), repos.Servers())

	// public
	router.Public("/ws", wsHandler.HandleWebSocket) // WS authenticates via its first "auth" message, not a header
	router.Public("/info", handlers.InfoHandler(cfg, memberCounter, defaultServerID, claimed))
	router.Public("GET /swagger/", httpSwagger.WrapHandler)
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
	// JWT + membership
	router.Member("GET /server/{id}", handlers.GetByServer(repos.Channels()))
	router.Member("GET /server/{id}/members", handlers.GetServerMembers(repos.Memberships(), appState))
	router.Member("POST /server/{id}/upload", uploads.Upload())
	router.MemberByChannel("GET /channel/{id}", handlers.GetChannelByID(repos.Channels(), appState))
	router.MemberByChannel("GET /channel/{id}/messages", handlers.GetChannelMessages(repos.Channels(), repos.Messages()))
	router.MemberByAttachment("GET /attachment/{id}", uploads.Serve(media.VariantFull))
	router.MemberByAttachment("GET /attachment/{id}/thumb", uploads.Serve(media.VariantThumb))
	// JWT + ownership
	router.Owner("POST /server/{id}/invite", handlers.CreateInvite(repos.Invites(), cfg.BaseURL()))

	return &Server{addr: ":" + cfg.Port, mux: router.mux, allowedOrigins: cfg.AllowedOrigins}, nil
}

func (s *Server) ListenAndServe() error {
	c := cors.New(cors.Options{
		AllowedOrigins:   s.allowedOrigins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: len(s.allowedOrigins) > 0,
	})
	return http.ListenAndServe(s.addr, c.Handler(s.mux))
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
