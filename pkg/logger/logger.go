// Package logger configures the process-wide structured logger. Self-hosted
// instances are usually operated by whoever's running them on their own
// machine/VPS, often without a log aggregator — JSON lines to stdout with
// consistent field names (user_id, server_id, channel_id) let that person
// grep/jq their way to "what did user X do" without adding any
// infrastructure, and slot straight into one if they do have a collector.
//
// Rationale for the context attributes, panic recovery and crash file lives
// in docs/ai/logging.md.
package logger

import (
	"io"
	"log/slog"
	"os"
	"runtime/debug"
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
//
// With a log file, fatal runtime errors and unrecovered panics are also
// appended to logFile+".crash" (they bypass slog entirely, see
// docs/ai/logging.md).
func Init(levelStr, logFile string) {
	writer := io.Writer(os.Stdout)
	var fileErr error

	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fileErr = err
		} else {
			writer = io.MultiWriter(os.Stdout, f)
		}
	}

	slog.SetDefault(slog.New(NewHandler(writer, parseLevel(levelStr))))

	if fileErr != nil {
		slog.Warn("could not open log file, logging to stdout only", "path", logFile, "error", fileErr)
		return
	}
	if logFile != "" {
		setCrashOutput(logFile + ".crash")
	}
}

// NewHandler is the JSON handler Init installs, with attributes stored by
// With added to every record.
func NewHandler(w io.Writer, level slog.Level) slog.Handler {
	return contextHandler{slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})}
}

func setCrashOutput(path string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		slog.Warn("could not open crash file, fatal errors go to stderr only", "path", path, "error", err)
		return
	}
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		slog.Warn("could not set crash output", "path", path, "error", err)
	}
	f.Close()
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
