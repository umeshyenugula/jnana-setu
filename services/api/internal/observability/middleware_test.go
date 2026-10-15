package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateEvent(t *testing.T) {
	org := "org-1"
	ok := Event{Service: "api", Name: EventDocumentPublished, CorrelationID: "c",
		OrganizationID: &org, Outcome: "ok", Detail: map[string]any{"document_id": "d1"}}
	if err := ValidateEvent(ok); err != nil {
		t.Fatalf("valid rejected: %v", err)
	}
	bad := []Event{
		{},
		{Service: "api", Name: EventDocumentPublished, Outcome: "maybe"},
		{Service: "api", Name: EventDocumentPublished, Outcome: "ok",
			Detail: map[string]any{"prompt": "leak"}},
		{Service: "api", Name: EventDocumentPublished, Outcome: "ok",
			Detail: map[string]any{"auth_token": "leak"}},
		{Service: "api", Name: EventDocumentPublished, Outcome: "ok",
			Detail: map[string]any{"blob": strings.Repeat("x", 5000)}},
	}
	for i, e := range bad {
		if err := ValidateEvent(e); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}

func TestLogEmitterValidation(t *testing.T) {
	log := NewLogger("api", "test", "info")
	em := &LogEmitter{Logger: log}
	if err := em.Emit(context.Background(), Event{Service: "api", Name: EventModelUnavailable, Outcome: "error"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if err := em.Emit(context.Background(), Event{}); err == nil {
		t.Fatal("invalid event emitted")
	}
}

func TestInferenceCollectorDeltas(t *testing.T) {
	reg := NewRegistry()
	cur := InferenceSnapshot{Requests: 10, Success: 8, Overloaded: 1, Timeouts: 1,
		Failures: map[string]int64{"GENERATION_FAILED": 2}, ModelLoadErrors: 1}
	RegisterInferenceCollector(reg, func() InferenceSnapshot { return cur })
	out := reg.Render()
	for _, want := range []string{
		`llm_requests_total{service="inference"} 10`,
		`llm_overloaded_total{service="inference"} 1`,
		`llm_timeouts_total{service="inference"} 1`,
		`llm_model_load_errors_total{service="inference"} 1`,
		`llm_failures_total{service="inference",code="GENERATION_FAILED"} 2`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// Second scrape with identical values adds no deltas.
	before := out
	cur.Requests = 12
	out2 := reg.Render()
	if !strings.Contains(out2, `llm_requests_total{service="inference"} 12`) {
		t.Errorf("delta missing:\n%s", out2)
	}
	_ = before
}

func TestIngestionCollectorSkipsFailures(t *testing.T) {
	reg := NewRegistry()
	RegisterIngestionCollector(reg, func(ctx context.Context) (map[string]int64, error) {
		return map[string]int64{"completed": 3}, nil
	})
	out := reg.Render()
	if !strings.Contains(out, `ingestion_jobs{status="completed"} 3`) {
		t.Errorf("missing series:\n%s", out)
	}
	if !strings.Contains(out, `ingestion_jobs{status="queued"} 0`) {
		t.Errorf("missing zero statuses:\n%s", out)
	}

	reg2 := NewRegistry()
	RegisterIngestionCollector(reg2, func(ctx context.Context) (map[string]int64, error) {
		return nil, context.DeadlineExceeded
	})
	// Must not panic or fail the scrape.
	_ = reg2.Render()
}

func TestMiddlewareRecordsBoundedMetrics(t *testing.T) {
	reg := NewRegistry()
	inst := NewInstruments(reg)
	log := NewLogger("api", "test", "error") // silence completion logs
	tr := NewTracer(1, &RecorderExporter{})
	mw := Middleware(log, inst, tr)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CorrelationFromContext(r.Context()) == "" {
			t.Error("correlation missing in handler")
		}
		w.WriteHeader(http.StatusOK)
	})
	h := CorrelationShim(mw(next))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/sessions/ses_abc/ses_def", nil)
	req.Header.Set("X-Correlation-ID", "corr-9")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	out := reg.Render()
	if !strings.Contains(out, `http_requests_total{method="GET",route="/api/v1/chat/sessions/{id}/{id}",status="2xx"} 1`) {
		t.Errorf("bounded series missing:\n%s", out)
	}
	if strings.Contains(out, "ses_abc") {
		t.Errorf("raw session ID leaked into labels:\n%s", out)
	}
	if rec.Header().Get("X-Correlation-ID") != "corr-9" {
		t.Errorf("correlation not echoed: %q", rec.Header().Get("X-Correlation-ID"))
	}
}

// CorrelationShim mimics api.CorrelationMiddleware without importing it.
func CorrelationShim(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Correlation-ID")
		if id == "" {
			id = "generated"
		}
		w.Header().Set("X-Correlation-ID", id)
		next.ServeHTTP(w, r.WithContext(WithCorrelation(r.Context(), id)))
	})
}

func TestMiddlewareErrorStatus(t *testing.T) {
	reg := NewRegistry()
	inst := NewInstruments(reg)
	mw := Middleware(nil, inst, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	rec := httptest.NewRecorder()
	Middleware(nil, inst, nil)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil))
	_ = mw
	out := reg.Render()
	if !strings.Contains(out, `status="5xx"`) {
		t.Errorf("5xx not recorded:\n%s", out)
	}
}
