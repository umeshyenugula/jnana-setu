package observability

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Distributed tracing with W3C trace-context propagation and an optional
// OTLP/HTTP+JSON exporter. No external dependencies: the exporter speaks the
// OTLP JSON schema directly, so any self-hosted collector works and nothing
// is required in local development (tracing defaults to local-only spans).

// Attribute keys that must never be recorded on spans.
var sensitiveSpanKeys = []string{
	"prompt", "message", "content", "document", "answer", "question", "query",
	"embedding", "token", "secret", "password", "authorization", "key", "session",
}

// SanitizeSpanAttrs drops sensitive attributes and truncates long values.
func SanitizeSpanAttrs(attrs map[string]string, maxLen int) map[string]string {
	if maxLen <= 0 {
		maxLen = 256
	}
	out := make(map[string]string, len(attrs))
	for k, v := range attrs {
		lower := strings.ToLower(k)
		drop := false
		for _, s := range sensitiveSpanKeys {
			if strings.Contains(lower, s) {
				drop = true
				break
			}
		}
		if drop {
			continue
		}
		if len(v) > maxLen {
			v = v[:maxLen] + "…[truncated]"
		}
		out[k] = v
	}
	return out
}

// SpanContext carries W3C trace identifiers.
type SpanContext struct {
	TraceID string
	SpanID  string
	Sampled bool
}

func newID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// FormatTraceParent renders the W3C traceparent header value.
func FormatTraceParent(sc SpanContext) string {
	flags := "00"
	if sc.Sampled {
		flags = "01"
	}
	return fmt.Sprintf("00-%s-%s-%s", sc.TraceID, sc.SpanID, flags)
}

// ParseTraceParent parses a W3C traceparent header. Malformed input yields ok=false.
func ParseTraceParent(header string) (sc SpanContext, ok bool) {
	parts := strings.Split(strings.TrimSpace(header), "-")
	if len(parts) != 4 || parts[0] != "00" || len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return SpanContext{}, false
	}
	if !isHex(parts[1]) || !isHex(parts[2]) || !isHex(parts[3]) {
		return SpanContext{}, false
	}
	if parts[1] == strings.Repeat("0", 32) || parts[2] == strings.Repeat("0", 16) {
		return SpanContext{}, false
	}
	return SpanContext{TraceID: parts[1], SpanID: parts[2], Sampled: parts[3] == "01"}, true
}

func isHex(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// InjectTraceParent sets the traceparent header on an outgoing request.
func InjectTraceParent(req *http.Request, sc SpanContext) {
	req.Header.Set("traceparent", FormatTraceParent(sc))
}

// ExtractTraceParent reads trace context from an incoming request.
func ExtractTraceParent(r *http.Request) (SpanContext, bool) {
	return ParseTraceParent(r.Header.Get("traceparent"))
}

// Span is one timed operation with bounded, sanitized attributes.
type Span struct {
	Name      string
	TraceID   string
	SpanID    string
	ParentID  string
	Start     time.Time
	End       time.Time
	Attrs     map[string]string
	Status    string // "ok" or "error"
	ErrCode   string
	Sampled   bool
}

// Exporter receives finished sampled spans.
type Exporter interface {
	Export(ctx context.Context, spans []Span) error
}

// RecorderExporter keeps spans in memory for tests and local inspection.
type RecorderExporter struct {
	mu    sync.Mutex
	Spans []Span
}

func (e *RecorderExporter) Export(_ context.Context, spans []Span) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Spans = append(e.Spans, spans...)
	return nil
}

// Reset clears recorded spans.
func (e *RecorderExporter) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Spans = nil
}

// OTLPExporter posts spans to an OTLP/HTTP+JSON endpoint (e.g. a self-hosted
// collector at http://collector:4318/v1/traces). Failures are returned to
// the caller; the tracer treats export errors as non-fatal.
type OTLPExporter struct {
	Endpoint   string
	Service    string
	HTTPClient *http.Client
}

func (e *OTLPExporter) client() *http.Client {
	if e.HTTPClient != nil {
		return e.HTTPClient
	}
	return &http.Client{Timeout: 5 * time.Second}
}

// Export converts spans to OTLP JSON and posts them.
func (e *OTLPExporter) Export(ctx context.Context, spans []Span) error {
	if len(spans) == 0 {
		return nil
	}
	body, err := json.Marshal(otlpTraces(e.Service, spans))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(e.Endpoint, "/")+"/v1/traces", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("otlp export failed: %s", resp.Status)
	}
	return nil
}

func otlpTraces(service string, spans []Span) map[string]any {
	otlpSpans := make([]any, 0, len(spans))
	for _, s := range spans {
		attrs := make([]any, 0, len(s.Attrs))
		for k, v := range s.Attrs {
			attrs = append(attrs, map[string]any{"key": k, "value": map[string]any{"stringValue": v}})
		}
		status := map[string]any{"code": "STATUS_CODE_OK"}
		if s.Status == "error" {
			status = map[string]any{"code": "STATUS_CODE_ERROR"}
		}
		otlpSpans = append(otlpSpans, map[string]any{
			"traceId": s.TraceID, "spanId": s.SpanID,
			"parentSpanId": s.ParentID, "name": s.Name,
			"startTimeUnixNano": fmt.Sprintf("%d", s.Start.UnixNano()),
			"endTimeUnixNano":   fmt.Sprintf("%d", s.End.UnixNano()),
			"attributes": attrs, "status": status,
		})
	}
	return map[string]any{"resourceSpans": []any{map[string]any{
		"resource": map[string]any{"attributes": []any{
			map[string]any{"key": "service.name", "value": map[string]any{"stringValue": service}},
		}},
		"scopeSpans": []any{map[string]any{"spans": otlpSpans}},
	}}}
}

// Tracer starts spans with ratio-based sampling. A nil exporter means
// local-only operation (spans are still attached to contexts for log
// correlation, but nothing leaves the process).
type Tracer struct {
	mu       sync.Mutex
	ratio    float64
	exporter Exporter
}

// NewTracer builds a tracer. ratio in [0,1] controls sampling; values <=0
// disable span export (trace IDs still propagate for log correlation).
func NewTracer(ratio float64, exporter Exporter) *Tracer {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return &Tracer{ratio: ratio, exporter: exporter}
}

func (t *Tracer) shouldSample() bool {
	if t.ratio <= 0 {
		return false
	}
	if t.ratio >= 1 {
		return true
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	v := float64(uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])) / float64(^uint64(0))
	return v < t.ratio
}

// Start begins a span. The returned context carries trace IDs for log
// correlation; finish with span.End(status, errCode).
func (t *Tracer) Start(ctx context.Context, name string, attrs map[string]string) (context.Context, *SpanHandle) {
	var traceID, parentID string
	if parent, ok := SpanFromContext(ctx); ok {
		traceID = parent.TraceID
		parentID = parent.SpanID
	} else if tid, _ := TraceFromContext(ctx); tid != "" {
		traceID = tid
	}
	if traceID == "" {
		traceID = newID(16)
	}
	sampled := t.shouldSample()
	span := &Span{
		Name: name, TraceID: traceID, SpanID: newID(8), ParentID: parentID,
		Start: time.Now(), Attrs: SanitizeSpanAttrs(attrs, 256), Sampled: sampled,
	}
	ctx = WithTrace(ctx, traceID, span.SpanID)
	ctx = context.WithValue(ctx, spanKey{}, span)
	return ctx, &SpanHandle{tracer: t, span: span}
}

type spanKey struct{}

// SpanFromContext returns the active span, if any.
func SpanFromContext(ctx context.Context) (*Span, bool) {
	s, ok := ctx.Value(spanKey{}).(*Span)
	return s, ok && s != nil
}

// SpanHandle finishes a span and exports it when sampled.
type SpanHandle struct {
	tracer *Tracer
	span   *Span
	done   bool
}

// End records the outcome and exports sampled spans. Export failures are
// swallowed: monitoring must never break request handling.
func (h *SpanHandle) End(status, errCode string) {
	if h.done {
		return
	}
	h.done = true
	h.span.End = time.Now()
	h.span.Status = status
	h.span.ErrCode = errCode
	if !h.span.Sampled || h.tracer.exporter == nil {
		return
	}
	_ = h.tracer.exporter.Export(context.Background(), []Span{*h.span})
}

// TraceID returns the span's trace ID for log correlation.
func (h *SpanHandle) TraceID() string { return h.span.TraceID }
