// Package observability implements Module 8: logging conventions, health and
// readiness checks, Prometheus-compatible metrics, W3C distributed tracing
// with an optional OTLP exporter, audit-event conventions, and HTTP
// instrumentation middleware.
//
// The package never implements business logic and never mutates other
// modules' data. It observes through existing public surfaces (health
// methods, metric snapshots, read-only SQL) or through adoption hooks other
// modules wire explicitly.
package observability

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// Log field names. Every structured log line uses exactly these keys so
// operators can query uniformly across services.
const (
	FieldTimestamp     = "timestamp"
	FieldSeverity      = "severity"
	FieldService       = "service"
	FieldEnv           = "env"
	FieldEvent         = "event"
	FieldCorrelationID = "correlation_id"
	FieldTraceID       = "trace_id"
	FieldSpanID        = "span_id"
	FieldCode          = "code"
	FieldDurationMS    = "duration_ms"
	FieldStatus        = "status"
)

// Keys that must never appear with raw values in logs, spans, or metrics.
// Matching is case-insensitive on attribute/field names.
var sensitiveKeys = []string{
	"password", "passwd", "secret", "token", "authorization", "api_key", "apikey",
	"prompt", "message", "content", "document", "documents", "answer", "question",
	"query", "query_text", "embedding", "chunks", "session",
}

// Redacted is the placeholder written instead of sensitive values.
const Redacted = "[redacted]"

// IsSensitiveKey reports whether a field name must be redacted.
func IsSensitiveKey(name string) bool {
	lower := strings.ToLower(name)
	for _, k := range sensitiveKeys {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

// RedactAttrs drops sensitive attributes and truncates long values. It
// returns a new slice; the input is never mutated.
func RedactAttrs(attrs []slog.Attr, maxValueLen int) []slog.Attr {
	if maxValueLen <= 0 {
		maxValueLen = 512
	}
	out := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		if IsSensitiveKey(a.Key) {
			a.Value = slog.StringValue(Redacted)
			out = append(out, a)
			continue
		}
		if s, ok := a.Value.Any().(string); ok && len(s) > maxValueLen {
			a.Value = slog.StringValue(s[:maxValueLen] + "…[truncated]")
		}
		out = append(out, a)
	}
	return out
}

type ctxKey string

const (
	ctxCorrelationKey ctxKey = "obs_correlation_id"
	ctxTraceKey       ctxKey = "obs_trace_id"
	ctxSpanKey        ctxKey = "obs_span_id"
)

// WithCorrelation stores the request correlation ID in the context. Callers
// must pass contexts explicitly; there is no global mutable request state.
func WithCorrelation(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxCorrelationKey, id)
}

// CorrelationFromContext returns the correlation ID, or "" when absent.
func CorrelationFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxCorrelationKey).(string); ok {
		return v
	}
	return ""
}

// WithTrace stores trace/span IDs alongside the correlation context.
func WithTrace(ctx context.Context, traceID, spanID string) context.Context {
	ctx = context.WithValue(ctx, ctxTraceKey, traceID)
	return context.WithValue(ctx, ctxSpanKey, spanID)
}

// TraceFromContext returns trace and span IDs, or "" when absent.
func TraceFromContext(ctx context.Context) (traceID, spanID string) {
	traceID, _ = ctx.Value(ctxTraceKey).(string)
	spanID, _ = ctx.Value(ctxSpanKey).(string)
	return traceID, spanID
}

// Logger is a service-scoped structured logger enforcing the Module 8
// schema. Prompts, customer messages, retrieved documents, tokens, and
// secrets are redacted by construction: use Event/Error/Completed with
// counts and codes, never raw payloads.
type Logger struct {
	base    *slog.Logger
	service string
	env     string
}

// NewLogger builds a JSON logger at the given level ("debug", "info",
// "warn", "error"; unknown values fall back to "info").
func NewLogger(service, env, level string) *Logger {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return &Logger{base: slog.New(h), service: service, env: env}
}

func (l *Logger) attrs(ctx context.Context, event string, extra []slog.Attr) []any {
	traceID, spanID := TraceFromContext(ctx)
	attrs := []slog.Attr{
		slog.String(FieldService, l.service),
		slog.String(FieldEnv, l.env),
		slog.String(FieldEvent, event),
		slog.String(FieldCorrelationID, CorrelationFromContext(ctx)),
	}
	if traceID != "" {
		attrs = append(attrs, slog.String(FieldTraceID, traceID))
	}
	if spanID != "" {
		attrs = append(attrs, slog.String(FieldSpanID, spanID))
	}
	attrs = append(attrs, extra...)
	attrs = RedactAttrs(attrs, 512)
	out := make([]any, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, a)
	}
	return out
}

// Event logs a normal operational event at info level.
func (l *Logger) Event(ctx context.Context, event string, attrs ...slog.Attr) {
	l.base.InfoContext(ctx, event, l.attrs(ctx, event, attrs)...)
}

// Debug logs at debug level (disabled unless OBS_LOG_LEVEL=debug).
func (l *Logger) Debug(ctx context.Context, event string, attrs ...slog.Attr) {
	l.base.DebugContext(ctx, event, l.attrs(ctx, event, attrs)...)
}

// Warn logs at warn level.
func (l *Logger) Warn(ctx context.Context, event string, attrs ...slog.Attr) {
	l.base.WarnContext(ctx, event, l.attrs(ctx, event, attrs)...)
}

// Error logs a failed operation with a stable code. Internal details stay
// server-side: pass codes and counts, never stack traces or payloads.
func (l *Logger) Error(ctx context.Context, event, code string, attrs ...slog.Attr) {
	all := append([]slog.Attr{slog.String(FieldCode, code)}, attrs...)
	l.base.ErrorContext(ctx, event, l.attrs(ctx, event, all)...)
}

// Completed logs a finished operation with duration and outcome status.
func (l *Logger) Completed(ctx context.Context, event, status string, durationMs int64, attrs ...slog.Attr) {
	all := append([]slog.Attr{
		slog.String(FieldStatus, status),
		slog.Int64(FieldDurationMS, durationMs),
	}, attrs...)
	l.base.InfoContext(ctx, event, l.attrs(ctx, event, all)...)
}
