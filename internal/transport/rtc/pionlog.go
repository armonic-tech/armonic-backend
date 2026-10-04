package rtc

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/pion/logging"
)

// slogFactory routes pion's internal logs (ICE, DTLS, SCTP) into slog. pion's
// Info is chatty per candidate, so it lands at Debug; Trace is dropped.
type slogFactory struct{}

func (slogFactory) NewLogger(scope string) logging.LeveledLogger {
	return pionLogger{l: slog.Default().With("component", "pion", "scope", scope)}
}

type pionLogger struct{ l *slog.Logger }

func (p pionLogger) log(level slog.Level, msg string) {
	p.l.Log(context.Background(), level, msg)
}

func (p pionLogger) Trace(string)                  {}
func (p pionLogger) Tracef(string, ...interface{}) {}
func (p pionLogger) Debug(msg string)              { p.log(slog.LevelDebug, msg) }
func (p pionLogger) Debugf(f string, a ...interface{}) {
	p.log(slog.LevelDebug, fmt.Sprintf(f, a...))
}
func (p pionLogger) Info(msg string) { p.log(slog.LevelDebug, msg) }
func (p pionLogger) Infof(f string, a ...interface{}) {
	p.log(slog.LevelDebug, fmt.Sprintf(f, a...))
}
func (p pionLogger) Warn(msg string) { p.log(slog.LevelWarn, msg) }
func (p pionLogger) Warnf(f string, a ...interface{}) {
	p.log(slog.LevelWarn, fmt.Sprintf(f, a...))
}
func (p pionLogger) Error(msg string) { p.log(slog.LevelError, msg) }
func (p pionLogger) Errorf(f string, a ...interface{}) {
	p.log(slog.LevelError, fmt.Sprintf(f, a...))
}
