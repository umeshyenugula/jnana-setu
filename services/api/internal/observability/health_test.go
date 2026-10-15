package observability

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func okProbe(ctx context.Context) error { _ = ctx; return nil }

func TestCheckerReady(t *testing.T) {
	c := NewChecker("api", []Check{
		{Name: "db", Mandatory: true, Probe: okProbe},
		{Name: "embeddings", Mandatory: false, Probe: okProbe},
	}, 0, 0, nil)
	rep := c.Run(context.Background())
	if rep.Status != "ready" {
		t.Fatalf("got %+v", rep)
	}
	if len(rep.Dependencies) != 2 || rep.Dependencies[0].Name != "db" {
		t.Fatalf("deps not sorted/named: %+v", rep.Dependencies)
	}
}

func TestCheckerDegradedAndNotReady(t *testing.T) {
	fail := func(ctx context.Context) error { _ = ctx; return errors.New("down") }
	c := NewChecker("api", []Check{
		{Name: "db", Mandatory: true, Probe: okProbe},
		{Name: "llm", Mandatory: false, Probe: fail},
	}, 0, 0, nil)
	if rep := c.Run(context.Background()); rep.Status != "degraded" {
		t.Fatalf("got %q", rep.Status)
	}
	c2 := NewChecker("api", []Check{
		{Name: "db", Mandatory: true, Probe: fail},
		{Name: "llm", Mandatory: false, Probe: okProbe},
	}, 0, 0, nil)
	rep := c2.Run(context.Background())
	if rep.Status != "not_ready" {
		t.Fatalf("got %q", rep.Status)
	}
	if rep.Dependencies[0].Error == "" {
		t.Fatal("missing error detail")
	}
	for _, d := range rep.Dependencies {
		if d.Error != "" && (strings.Contains(d.Error, "password") || strings.Contains(d.Error, "/")) {
			t.Fatalf("detail leaks internals: %q", d.Error)
		}
	}
}

func TestCheckerTimeoutsBounded(t *testing.T) {
	hang := func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	c := NewChecker("api", []Check{
		{Name: "hung", Mandatory: false, Probe: hang},
	}, 50*time.Millisecond, 0, nil)
	start := time.Now()
	rep := c.Run(context.Background())
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("check hung the endpoint: %v", elapsed)
	}
	if rep.Status != "degraded" || rep.Dependencies[0].OK {
		t.Fatalf("got %+v", rep)
	}
}

func TestReadinessGauge(t *testing.T) {
	reg := NewRegistry()
	g := reg.RegisterGauge("platform_ready", "Readiness.", LabelService)
	c := NewChecker("api", []Check{{Name: "db", Mandatory: true, Probe: okProbe}}, 0, 0, g)
	c.Run(context.Background())
	if out := reg.Render(); !strings.Contains(out, "platform_ready{service=\"api\"} 1") {
		t.Fatalf("gauge missing:\n%s", out)
	}
}

func TestReadyHandlerStatuses(t *testing.T) {
	reg := NewRegistry()
	h := &Handlers{Version: "v1", Reg: reg, Checker: NewChecker("api", []Check{
		{Name: "db", Mandatory: true, Probe: okProbe},
	}, 0, 0, nil)}
	rec := httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status":"ready"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}

	fail := func(ctx context.Context) error { _ = ctx; return errors.New("down") }
	h.Checker = NewChecker("api", []Check{{Name: "db", Mandatory: true, Probe: fail}}, 0, 0, nil)
	rec = httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "down") {
		t.Fatalf("leaked detail: %s", rec.Body.String())
	}
}

func TestMetricsHandlerAuth(t *testing.T) {
	reg := NewRegistry()
	h := &Handlers{Version: "v1", Reg: reg, Token: "secret"}
	rec := httptest.NewRecorder()
	h.Metrics(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated scrape allowed: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h.Metrics(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("code=%d ct=%s", rec.Code, rec.Header().Get("Content-Type"))
	}
	// Fail-closed when unconfigured.
	h.Token = ""
	req = httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h.Metrics(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unconfigured token allowed scrape: %d", rec.Code)
	}
}
