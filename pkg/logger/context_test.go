package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func capture(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(NewHandler(&buf, slog.LevelDebug)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("not one JSON line: %q", buf.String())
	}
	return m
}

func TestWithAccumulatesAndDoesNotLeakIntoParent(t *testing.T) {
	buf := capture(t)
	parent := With(context.Background(), "req_id", "r1")
	child := With(parent, User("u1"))

	slog.InfoContext(child, "x")
	m := decode(t, buf)
	if m["req_id"] != "r1" || m["user_id"] != "u1" {
		t.Fatalf("child missing attrs: %v", m)
	}

	buf.Reset()
	slog.InfoContext(parent, "y")
	if m := decode(t, buf); m["user_id"] != nil {
		t.Fatalf("child attr leaked into parent: %v", m)
	}
}

func TestRecoverLogsPanicWithStack(t *testing.T) {
	buf := capture(t)
	func() {
		defer Recover(With(context.Background(), "conn_id", "c1"), "unit")
		panic("boom")
	}()
	m := decode(t, buf)
	if m["panic"] != "boom" || m["where"] != "unit" || m["conn_id"] != "c1" || m["stack"] == "" {
		t.Fatalf("unexpected panic record: %v", m)
	}
}
