package logger

import (
	"context"
	"log/slog"
	"runtime"
	"time"
)

// RuntimeStats logs a heartbeat every interval until ctx is done: goroutines,
// heap and GC, plus whatever extra returns. A steady climb across heartbeats
// is the trail an OOM kill or a goroutine leak leaves behind.
func RuntimeStats(ctx context.Context, interval time.Duration, extra func() []any) {
	start := time.Now()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			args := []any{
				"uptime", time.Since(start).Round(time.Second).String(),
				"goroutines", runtime.NumGoroutine(),
				"heap_alloc_mb", m.HeapAlloc >> 20,
				"sys_mb", m.Sys >> 20,
				"num_gc", m.NumGC,
			}
			if extra != nil {
				args = append(args, extra()...)
			}
			slog.InfoContext(ctx, "runtime stats", args...)
		}
	}
}
