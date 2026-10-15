package observability

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTraceParentRoundTrip(t *testing.T) {
	sc := SpanContext{TraceID: strings.Repeat("a", 32), SpanID: strings.Repeat("b", 16), Sampled: true}
	h := FormatTraceParent(sc)
	if h != "00-"+strings.Repeat("a", 32)+"-"+strings.Repeat("b", 16)+"-01" {
		t.Fatalf("bad header %q", h)
	}
	got, ok := ParseTraceParent(h)
	if !ok || got != sc {
		t.Fatalf("round trip failed: %+v %v", got, ok)
	}
}

func TestParseTraceParentRejects(t *testing.T) {
	for _, h := range []string{
		"", "bogus", "00-short-01", "01-" + strings.Repeat("a", 32) + "-" + strings.Repeat("b", 16) + "-01",
		"00-" + strings.Repeat("0", 32) + "-" + strings.Repeat("b", 16) + "-01",
		"00-" + strings.Repeat("a", 32) + "-" + strings.Repeat("0", 16) + "-01",
		"00-" + strings.Repeat("z", 32) + "-" + strings.Repeat("b", 16) + "-01",
	} {
		if _, ok := ParseTraceParent(h); ok {
			t.Errorf("accepted malformed header %q", h)
		}
	}
}

func TestSpanSanitization(t *testing.T) {
	attrs := SanitizeSpanAttrs(map[string]string{
		"http.method": "POST",
		"user_prompt": "secret question",
		"API_KEY":     "abc",
		"db.rows":     "12",
	}, 0)
	if attrs["http.method"] != "POST" || attrs["db.rows"] != "12" {
		t.Errorf("safe attrs altered: %v", attrs)
	}
	if _, ok := attrs["user_prompt"]; ok {
		t.Errorf("sensitive attr kept: %v", attrs)
	}
	if _, ok := attrs["API_KEY"]; ok {
		t.Errorf("sensitive attr kept: %v", attrs)
	}
}

func TestTracerSamplingAndExport(t *testing.T) {
	rec := &RecorderExporter{}
	tr := NewTracer(1, rec)
	ctx, span := tr.Start(context.Background(), "chat.request", map[string]string{"http.method": "POST"})
	if _, ok := SpanFromContext(ctx); !ok {
		t.Fatal("span missing from context")
	}
	tid, _ := TraceFromContext(ctx)
	if tid == "" || tid != span.TraceID() {
		t.Fatal("trace IDs not propagated to context")
	}
	span.End("ok", "")
	if len(rec.Spans) != 1 || rec.Spans[0].Name != "chat.request" || rec.Spans[0].Status != "ok" {
		t.Fatalf("bad export: %+v", rec.Spans)
	}

	// Disabled sampling exports nothing but keeps IDs for log correlation.
	rec.Reset()
	off := NewTracer(0, rec)
	ctx2, span2 := off.Start(context.Background(), "x", nil)
	if _, ok := SpanFromContext(ctx2); !ok {
		t.Fatal("local span missing")
	}
	span2.End("error", "E")
	if len(rec.Spans) != 0 {
		t.Fatal("disabled tracer exported")
	}

	// Child spans inherit the trace ID.
	ctx3, child := tr.Start(ctx, "retrieval.request", nil)
	if child.span.TraceID != span.TraceID() {
		t.Fatal("child trace ID mismatch")
	}
	_ = ctx3
	child.End("ok", "")
}

func TestInjectExtractHTTP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	sc := SpanContext{TraceID: strings.Repeat("1", 32), SpanID: strings.Repeat("2", 16), Sampled: false}
	InjectTraceParent(req, sc)
	got, ok := ExtractTraceParent(req)
	if !ok || got.TraceID != sc.TraceID {
		t.Fatalf("propagation failed: %+v %v", got, ok)
	}
}

func TestOTLPExportShape(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("bad export request: %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	exp := &OTLPExporter{Endpoint: srv.URL, Service: "api"}
	s := Span{Name: "db.search", TraceID: strings.Repeat("a", 32), SpanID: strings.Repeat("b", 16),
		Start: time.Now(), End: time.Now(), Attrs: map[string]string{"db.rows": "3"}, Status: "ok"}
	if err := exp.Export(context.Background(), []Span{s}); err != nil {
		t.Fatalf("export: %v", err)
	}
	for _, want := range []string{"resourceSpans", "service.name", "db.search", "STATUS_CODE_OK"} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("body missing %q: %s", want, gotBody)
		}
	}
	// Empty batches are no-ops, even against a dead endpoint.
	dead := &OTLPExporter{Endpoint: "http://127.0.0.1:1", Service: "api"}
	if err := dead.Export(context.Background(), nil); err != nil {
		t.Fatalf("empty export should succeed: %v", err)
	}
}
