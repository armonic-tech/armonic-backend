// Package logger configures the process-wide structured logger. Self-hosted
// instances are usually operated by whoever's running them on their own
// machine/VPS, often without a log aggregator — JSON lines to stdout with
// consistent field names (user_id, server_id, channel_id) let that person
// grep/jq their way to "what did user X do" without adding any
// infrastructure, and slot straight into one if they do have a collector.
package logger

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Init installs a JSON slog handler at the given level as the process-wide
// default logger (retrievable anywhere via slog.Default()). levelStr is
// case-insensitive ("debug", "info", "warn", "error"); anything else, most
// notably the empty string, falls back to "info".
//
// If logFile is non-empty, logs are written to both stdout and that file
// (append mode) — the file is what lets an operator debug a report of "it
// broke earlier" after the fact, without having kept the terminal open or
// wired up a log collector. There's no default path: a relative path's
// meaning depends on the working directory the process happens to be
// launched from (systemd unit, Windows service, container WORKDIR all
// differ), so callers must pass an absolute path if they want this. If the
// file can't be opened (bad path, permissions, read-only filesystem),
// logging falls back to stdout only — a broken log file must never take
// down the server.
func Init(levelStr, logFile string) {
	writer := io.Writer(os.Stdout)

	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
				Level: parseLevel(levelStr),
			})))
			slog.Warn("could not open log file, logging to stdout only", "path", logFile, "error", err)
			return
		}
		writer = io.MultiWriter(os.Stdout, f)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level: parseLevel(levelStr),
	})))
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// User, Server and Channel are the IDs handlers most commonly need to attach
// for traceability — e.g. slog.Error("kick failed", logger.User(id), "error", err).
func User(userID string) slog.Attr       { return slog.String("user_id", userID) }
func Server(serverID string) slog.Attr   { return slog.String("server_id", serverID) }
func Channel(channelID string) slog.Attr { return slog.String("channel_id", channelID) }
