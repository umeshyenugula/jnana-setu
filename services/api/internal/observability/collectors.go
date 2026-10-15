package observability

import (
	"context"
	"sync"
	"time"
)

// Scrape-time collectors bridge existing module state into Prometheus
// without modifying business code. Callbacks run synchronously inside
// Render, so implementations must be fast, bounded, and failure-safe:
// on any error they skip and leave previous values in place.

// InferenceSnapshot mirrors the cumulative values from the inference
// service's existing Metrics().Snapshot(). main.go adapts that map; this
// package never imports Module 5.
type InferenceSnapshot struct {
	Requests         int64
	Success          int64
	Failures         map[string]int64
	Overloaded       int64
	Timeouts         int64
	PromptTokens     int64
	CompletionTokens int64
	ModelLoadErrors  int64
}

// RegisterInferenceCollector exposes inference counters and model-load
// errors. Snapshot values are cumulative, so the collector tracks
// last-seen values and adds only positive deltas.
func RegisterInferenceCollector(reg *Registry, snapshot func() InferenceSnapshot) {
	requests := reg.RegisterCounter("llm_requests_total", "Local inference requests.", LabelService)
	overloaded := reg.RegisterCounter("llm_overloaded_total", "Rejected requests while the model slot was busy.", LabelService)
	timeouts := reg.RegisterCounter("llm_timeouts_total", "Inference requests that timed out.", LabelService)
	loadErrs := reg.RegisterCounter("llm_model_load_errors_total", "Model readiness check failures.", LabelService)
	failures := reg.RegisterCounter("llm_failures_total", "Failed inference requests by error code.", LabelService, LabelDef{Name: "code"})

	var mu sync.Mutex
	last := map[string]int64{}
	add := func(key string, cur int64, emit func(int64)) {
		mu.Lock()
		defer mu.Unlock()
		if cur < 0 {
			return
		}
		if d := cur - last[key]; d > 0 {
			emit(d)
		}
		last[key] = cur
	}

	reg.RegisterCallback(func() {
		snap := snapshot()
		add("req", snap.Requests, func(d int64) { requests.Add(float64(d), "inference") })
		add("ovl", snap.Overloaded, func(d int64) { overloaded.Add(float64(d), "inference") })
		add("tmo", snap.Timeouts, func(d int64) { timeouts.Add(float64(d), "inference") })
		add("lde", snap.ModelLoadErrors, func(d int64) { loadErrs.Add(float64(d), "inference") })
		for code, cur := range snap.Failures {
			c := code
			add("fail:"+c, cur, func(d int64) { failures.Add(float64(d), "inference", c) })
		}
	})
}

// RegisterIngestionCollector exposes ingestion job counts by status from a
// read-only query. The query runs on each scrape with a short timeout; any
// failure is skipped so monitoring never breaks the API.
func RegisterIngestionCollector(reg *Registry, query func(ctx context.Context) (map[string]int64, error)) {
	jobs := reg.RegisterGauge("ingestion_jobs", "Ingestion jobs by status (best-effort snapshot).", LabelDef{Name: "status"})
	reg.RegisterCallback(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		counts, err := query(ctx)
		if err != nil {
			return
		}
		for _, s := range []string{"queued", "processing", "completed", "failed", "cancelled"} {
			jobs.Set(float64(counts[s]), s)
		}
	})
}
