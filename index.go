package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/armonic-tech/armonic-backend/config"
	repo "github.com/armonic-tech/armonic-backend/internal/repositories"
	"github.com/armonic-tech/armonic-backend/internal/server"
	"github.com/armonic-tech/armonic-backend/pkg/logger"

	_ "github.com/joho/godotenv/autoload"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}
	logger.Init(cfg.LogLevel, cfg.LogFile)

	db, err := repo.NewDB(cfg.DBPath)
	if err != nil {
		slog.Error("failed to open db", "error", err)
		os.Exit(1)
	}

	repos := repo.InitRepositories(db)

	srv, err := server.New(ctx, cfg, repos)
	if err != nil {
		slog.Error("failed to build server", "error", err)
		os.Exit(1)
	}

	slog.Info("listening", "addr", cfg.BaseURL())
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
