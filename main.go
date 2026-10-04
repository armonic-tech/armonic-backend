package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/armonic-tech/armonic-backend/config"
	repo "github.com/armonic-tech/armonic-backend/internal/repositories"
	"github.com/armonic-tech/armonic-backend/internal/server"
	"github.com/armonic-tech/armonic-backend/pkg/logger"

	_ "github.com/joho/godotenv/autoload"
)

// @title           Armonic API
// @version         1.0
// @description     Armonic API backend server.

// @securityDefinitions.apikey  BearerAuth
// @in                          header
// @name                        Authorization
// @description                 Type "Bearer" followed by a space and the JWT returned by /auth/login. Example: "Bearer eyJhbGci..."

// @tag.name        Users
// @tag.description User endpoints

// @tag.name        Server
// @tag.description Server endpoint

// @tag.name        Channel
// @tag.description Channel endpoint
func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}
	logger.Init(cfg.LogLevel, cfg.LogFile)
	started := time.Now()
	logStartup(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := repo.NewDB(cfg.DBPath)
	if err != nil {
		slog.Error("failed to open db", "error", err)
		os.Exit(1)
	}

	repos := repo.InitRepositories(db)

	srv, err := server.New(ctx, cfg, db, repos)
	if err != nil {
		slog.Error("failed to build server", "error", err)
		os.Exit(1)
	}

	go func() {
		<-ctx.Done()
		slog.Info("shutdown signal received", "uptime", time.Since(started).Round(time.Second).String())
	}()

	slog.Info("listening", "addr", cfg.BaseURL())
	if err := srv.Run(ctx); err != nil {
		slog.Error("server stopped", "error", err, "uptime", time.Since(started).Round(time.Second).String())
		os.Exit(1)
	}
	db.Close()
	slog.Info("server stopped cleanly", "uptime", time.Since(started).Round(time.Second).String())
}

// logStartup records what this process is and how it was configured, so a
// log read after a crash says which build and settings produced it. Secrets
// (JWT, claim password, DB DSN) are never logged.
func logStartup(cfg config.Config) {
	revision, modified := "unknown", false
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
	}
	slog.Info("starting armonic",
		"revision", revision,
		"dirty", modified,
		"go", runtime.Version(),
		"os", runtime.GOOS,
		"arch", runtime.GOARCH,
		"pid", os.Getpid(),
		"log_level", cfg.LogLevel,
		"log_file", cfg.LogFile,
		"port", cfg.Port,
		"host", cfg.Host,
		"allowed_origins", cfg.AllowedOrigins,
		"trusted_proxies", cfg.TrustedProxies,
		"upload_dir", cfg.Upload.Dir,
		"pow_enabled", cfg.Pow.Enabled,
		"rtc_udp_port", cfg.RTC.UDPPort,
		"rtc_host_ips", cfg.RTC.HostIPs,
		"stun_servers", cfg.RTC.StunServers,
		"default_jwt_secret", cfg.JWTSecret == "change-me",
	)
	if cfg.JWTSecret == "change-me" {
		slog.Warn("JWT_SECRET is the default value; set it in any real deployment")
	}
}
