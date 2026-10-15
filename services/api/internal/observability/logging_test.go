package observability

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestIsSensitiveKey(t *testing.T) {
	for _, k := range []string{"password", "Authorization", "API_KEY", "user_prompt", "query_text", "session", "embedding"} {
		if !IsSensitiveKey(k) {
			t.Errorf("expected sensitive: %s", k)
		}
	}
	for _, k := range []string{"service", "event", "duration_ms", "http.route", "code"} {
		if IsSensitiveKey(k) {
			t.Errorf("expected non-sensitive: %s", k)
		}
	}
}

func TestRedactAttrs(t *testing.T) {
	in := []slog.Attr{
		slog.String("password", "hunter2"),
		slog.String("event", "chat.done"),
		slog.String("note", strings.Repeat("x", 600)),
	}
	out := RedactAttrs(in, 512)
	if out[0].Value.String() != Redacted {
		t.Errorf("password not redacted: %v", out[0].Value)
	}
	if out[1].Value.String() != "chat.done" {
		t.Errorf("safe attr altered: %v", out[1].Value)
	}
	if !strings.HasSuffix(out[2].Value.String(), "…[truncated]") {
		t.Errorf("long value not truncated")
	}
	// Input must not be mutated.
	if in[0].Value.String() != "hunter2" {
		t.Errorf("input mutated")
	}
}

func TestCorrelationContext(t *testing.T) {
	ctx := context.Background()
	if got := CorrelationFromContext(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
	ctx = WithCorrelation(ctx, "corr-1")
	if got := CorrelationFromContext(ctx); got != "corr-1" {
		t.Errorf("got %q", got)
	}
	ctx = WithTrace(ctx, "trace-1", "span-1")
	tid, sid := TraceFromContext(ctx)
	if tid != "trace-1" || sid != "span-1" {
		t.Errorf("got %q %q", tid, sid)
	}
	if got := CorrelationFromContext(ctx); got != "corr-1" {
		t.Errorf("correlation lost after trace: %q", got)
	}
}

func TestNewLoggerLevels(t *testing.T) {
	for _, lvl := range []string{"debug", "info", "warn", "error", "bogus", ""} {
		l := NewLogger("api", "test", lvl)
		if l == nil || l.service != "api" || l.env != "test" {
			t.Errorf("bad logger for level %q", lvl)
		}
	}
}
