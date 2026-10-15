package inference

import (
	"sync"
	"time"
)

// Metrics records operational counters without storing prompts, documents, or
// customer messages. Safe for concurrent use.
type Metrics struct {
	mu               sync.Mutex
	Requests         int64
	Success          int64
	Failures         map[string]int64
	Overloaded       int64
	Timeouts         int64
	PromptTokens     int64
	CompletionTokens int64
	LatencyTotal     time.Duration
	LatencyCount     int64
	ModelLoadErrors  int64
}

func (m *Metrics) recordRequest() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Requests++
}

func (m *Metrics) recordSuccess(promptTokens, completionTokens int, latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Success++
	m.PromptTokens += int64(promptTokens)
	m.CompletionTokens += int64(completionTokens)
	m.LatencyTotal += latency
	m.LatencyCount++
}

func (m *Metrics) recordFailure(code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Failures == nil {
		m.Failures = map[string]int64{}
	}
	m.Failures[code]++
	if code == CodeModelOverloaded {
		m.Overloaded++
	}
	if code == CodeGatewayTimeout {
		m.Timeouts++
	}
}

func (m *Metrics) recordModelLoadError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ModelLoadErrors++
}

// Snapshot returns a point-in-time copy including derived averages.
func (m *Metrics) Snapshot() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	failures := map[string]int64{}
	for k, v := range m.Failures {
		failures[k] = v
	}
	var avgMs float64
	if m.LatencyCount > 0 {
		avgMs = float64(m.LatencyTotal.Milliseconds()) / float64(m.LatencyCount)
	}
	return map[string]any{
		"requests":            m.Requests,
		"success":             m.Success,
		"failures":            failures,
		"overloaded":          m.Overloaded,
		"timeouts":            m.Timeouts,
		"promptTokens":        m.PromptTokens,
		"completionTokens":    m.CompletionTokens,
		"avgLatencyMs":        avgMs,
		"modelLoadErrors":     m.ModelLoadErrors,
	}
}
