package logger

import (
	"context"
	"log/slog"
	"runtime/debug"
)

type attrsKey struct{}

// With returns a ctx whose every *Context log call carries args, on top of
// whatever ctx already carried.
func With(ctx context.Context, args ...any) context.Context {
	if len(args) == 0 {
		return ctx
	}
	prev, _ := ctx.Value(attrsKey{}).([]slog.Attr)
	r := slog.Record{}
	r.Add(args...)
	next := make([]slog.Attr, len(prev), len(prev)+r.NumAttrs())
	copy(next, prev)
	r.Attrs(func(a slog.Attr) bool {
		next = append(next, a)
		return true
	})
	return context.WithValue(ctx, attrsKey{}, next)
}

type contextHandler struct{ slog.Handler }

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if ctx != nil {
		if attrs, ok := ctx.Value(attrsKey{}).([]slog.Attr); ok {
			r.AddAttrs(attrs...)
		}
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}

// Recover must be deferred directly (`defer logger.Recover(ctx, "where")`):
// it logs a panic with its stack instead of letting it kill the process.
func Recover(ctx context.Context, where string) {
	if v := recover(); v != nil {
		LogPanic(ctx, where, v)
	}
}

func LogPanic(ctx context.Context, where string, v any) {
	slog.ErrorContext(ctx, "panic recovered", "where", where, "panic", v, "stack", string(debug.Stack()))
}

// Go runs fn on its own goroutine under Recover.
func Go(ctx context.Context, where string, fn func()) {
	go func() {
		defer Recover(ctx, where)
		fn()
	}()
}
