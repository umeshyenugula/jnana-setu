package observability

import (
	"strings"
	"testing"
)

func TestStatusClass(t *testing.T) {
	cases := map[int]string{200: "2xx", 201: "2xx", 400: "4xx", 404: "4xx", 429: "4xx", 500: "5xx", 503: "5xx", 100: "other", 302: "other"}
	for code, want := range cases {
		if got := StatusClass(code); got != want {
			t.Errorf("StatusClass(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestRouteTemplate(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/api/v1/health", "/api/v1/health"},
		{"/api/v1/organizations/123e4567-e89b-12d3-a456-426614174000/knowledge", "/api/v1/organizations/{id}/knowledge"},
		{"/api/v1/chat/sessions/ses_abcdef0123456789abcdef0123456789", "/api/v1/chat/sessions/{id}"},
		{"/api/v1/widget-config/wdg_abc123", "/api/v1/widget-config/{id}"},
		{"/api/v1/processing/jobs/42/retry", "/api/v1/processing/jobs/{id}/retry"},
		{"", "other"},
		{"nope", "other"},
		{"/api/v1/organizations/not-a-uuid-at-all", "/api/v1/organizations/not-a-uuid-at-all"},
	}
	for _, tc := range tests {
		if got := RouteTemplate(tc.in); got != tc.want {
			t.Errorf("RouteTemplate(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCounterRender(t *testing.T) {
	reg := NewRegistry()
	c := reg.RegisterCounter("test_requests_total", "Test counter.", LabelMethod, LabelStatus)
	c.Inc("GET", "2xx")
	c.Inc("GET", "2xx")
	c.Add(-5, "GET", "2xx") // ignored
	c.Inc("BREW", "2xx")    // unknown method -> other
	out := reg.Render()
	if !strings.Contains(out, `test_requests_total{method="GET",status="2xx"} 2`) {
		t.Errorf("missing series:\n%s", out)
	}
	if !strings.Contains(out, `method="other"`) {
		t.Errorf("unknown label not bounded:\n%s", out)
	}
	if !strings.Contains(out, "# TYPE test_requests_total counter") {
		t.Errorf("missing TYPE line:\n%s", out)
	}
}

func TestHistogramRender(t *testing.T) {
	reg := NewRegistry()
	h := reg.RegisterHistogram("test_latency_seconds", "Test histogram.", []float64{0.1, 1}, LabelOperation)
	h.Observe(0.05, "retrieve")
	h.Observe(0.5, "retrieve")
	h.Observe(-1, "retrieve") // ignored
	out := reg.Render()
	for _, want := range []string{
		`test_latency_seconds_bucket{operation="retrieve",le="0.1"} 1`,
		`test_latency_seconds_bucket{operation="retrieve",le="1"} 2`,
		`test_latency_seconds_bucket{operation="retrieve",le="+Inf"} 2`,
		`test_latency_seconds_sum{operation="retrieve"} 0.55`,
		`test_latency_seconds_count{operation="retrieve"} 2`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestGaugeSet(t *testing.T) {
	reg := NewRegistry()
	g := reg.RegisterGauge("test_ready", "Test gauge.", LabelService)
	g.Set(1, "api")
	g.Set(0, "api")
	out := reg.Render()
	if !strings.Contains(out, "test_ready{service=\"api\"} 0") {
		t.Errorf("gauge not overwritten:\n%s", out)
	}
}

func TestLabelCardinalityBounded(t *testing.T) {
	reg := NewRegistry()
	c := reg.RegisterCounter("bounded_total", "Bounded.", LabelDef{Name: "code", Allowed: allowSet("ok")})
	for i := 0; i < 100; i++ {
		c.Inc("distinct-value-that-would-explode-cardinality")
	}
	out := reg.Render()
	lines := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "bounded_total{") {
			lines++
			if !strings.Contains(line, `code="other"`) {
				t.Errorf("unbounded series leaked: %q", line)
			}
		}
	}
	if lines != 1 {
		t.Errorf("expected 1 series, got %d", lines)
	}
}

func TestRenderDeterministic(t *testing.T) {
	reg := NewRegistry()
	c := reg.RegisterCounter("det_total", "Deterministic.", LabelService)
	c.Inc("b")
	c.Inc("a")
	if a, b := reg.Render(), reg.Render(); a != b {
		t.Errorf("render not deterministic")
	}
}
