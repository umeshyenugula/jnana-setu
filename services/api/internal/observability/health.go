package observability

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
)

// Platform health model. Liveness ("am I alive") is the existing
// GET /api/v1/health, which stays untouched. Readiness ("can I do useful
// work") is GET /api/v1/ready, computed here from dependency checks.
//
// Dependency policy (documented, configurable in main.go):
//   - mandatory: Supabase Postgres. If unreachable, status is "not_ready".
//   - optional: embedding worker, llama.cpp runtime. If unreachable, status
//     is "degraded" with per-dependency detail, never "not_ready". Rationale:
//     cold starts and local model reloads are normal; alerting treats
//     "degraded" with softer thresholds (see infra/monitoring/alerts.yml).
//
// Checks run concurrently with per-check timeouts plus an overall bound, so
// one hung dependency can neither hang the endpoint nor cascade into other
// checks. Probes are cheap (ping / /health, never inference).

// DepStatus is one dependency outcome.
type DepStatus struct {
	Name      string `json:"name"`
	Mandatory bool   `json:"mandatory"`
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latencyMs"`
	Error     string `json:"error,omitempty"`
}

// Report is the readiness payload.
type Report struct {
	Status       string      `json:"status"` // ready | degraded | not_ready
	Service      string      `json:"service"`
	Dependencies []DepStatus `json:"dependencies"`
}

// Check probes one dependency. Implementations must honor ctx.
type Check struct {
	Name      string
	Mandatory bool
	Probe     func(ctx context.Context) error
}

// Checker runs checks with bounded timeouts.
type Checker struct {
	service   string
	checks    []Check
	perCheck  time.Duration
	overall   time.Duration
	readiness *Gauge
}

// NewChecker builds a checker. Timeouts <=0 get safe defaults.
func NewChecker(service string, checks []Check, perCheck, overall time.Duration, readiness *Gauge) *Checker {
	if perCheck <= 0 {
		perCheck = 2 * time.Second
	}
	if overall <= 0 {
		overall = 5 * time.Second
	}
	return &Checker{service: service, checks: checks, perCheck: perCheck, overall: overall, readiness: readiness}
}

// Run executes all checks concurrently and computes the platform status.
func (c *Checker) Run(ctx context.Context) Report {
	ctx, cancel := context.WithTimeout(ctx, c.overall)
	defer cancel()

	results := make([]DepStatus, len(c.checks))
	var wg sync.WaitGroup
	for i, chk := range c.checks {
		wg.Add(1)
		go func(i int, chk Check) {
			defer wg.Done()
			pctx, pcancel := context.WithTimeout(ctx, c.perCheck)
			defer pcancel()
			start := time.Now()
			err := chk.Probe(pctx)
			st := DepStatus{Name: chk.Name, Mandatory: chk.Mandatory, LatencyMs: time.Since(start).Milliseconds()}
			if err != nil {
				st.OK = false
				st.Error = "unreachable"
			} else {
				st.OK = true
			}
			results[i] = st
		}(i, chk)
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })

	status := "ready"
	for _, r := range results {
		if r.Mandatory && !r.OK {
			status = "not_ready"
			break
		}
		if !r.Mandatory && !r.OK {
			status = "degraded"
		}
	}
	if c.readiness != nil {
		v := 0.0
		if status == "ready" {
			v = 1.0
		}
		// "degraded" intentionally reports 0: alerts page on not_ready only.
		c.readiness.Set(v, c.service)
	}
	return Report{Status: status, Service: c.service, Dependencies: results}
}

// Handlers exposes readiness and Prometheus exposition over HTTP.
type Handlers struct {
	Version string
	Checker *Checker
	Reg     *Registry
	Token   string // INTERNAL_SERVICE_TOKEN; gates /metrics. Empty fails closed.
}

// Ready handles GET /api/v1/ready. Public but minimal: names, booleans,
// latencies. No credentials, paths, traces, or settings.
func (h *Handlers) Ready(w http.ResponseWriter, r *http.Request) {
	report := h.Checker.Run(r.Context())
	status := http.StatusOK
	if report.Status == "not_ready" {
		status = http.StatusServiceUnavailable
	}
	api.WriteSuccess(w, r, status, report, api.CorrelationID(r.Context()), api.APIVersion)
}

// Metrics handles GET /api/v1/metrics. Gated by the internal service token
// (Prometheus scrapes with a bearer token); never public without controls.
func (h *Handlers) Metrics(w http.ResponseWriter, r *http.Request) {
	if h.Token == "" || r.Header.Get("Authorization") != "Bearer "+h.Token {
		api.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", api.CorrelationID(r.Context()), api.APIVersion)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.Reg.Render()))
}
