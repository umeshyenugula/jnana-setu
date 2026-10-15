package observability

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// Prometheus exposition (text format 0.0.4) without external dependencies.
// Label values are bounded: each label declares an allowlist and anything
// else renders as "other", which keeps cardinality finite. Raw message
// contents, session IDs, user IDs, and tenant IDs must never be labels.

// LabelDef bounds one label dimension.
type LabelDef struct {
	Name    string
	Allowed map[string]bool // nil means free-form but still sanitized
}

// normalizeLabelValue maps a value into its bounded set.
func normalizeLabelValue(def LabelDef, v string) string {
	v = sanitizeLabelValue(v)
	if def.Allowed == nil {
		if v == "" {
			return "unknown"
		}
		return v
	}
	if def.Allowed[v] {
		return v
	}
	return "other"
}

func sanitizeLabelValue(v string) string {
	if len(v) > 64 {
		v = v[:64]
	}
	var b strings.Builder
	for _, r := range v {
		// Braces are legal in Prometheus label *values* and keep route
		// templates readable (…/sessions/{id}).
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-' || r == '/' || r == '{' || r == '}' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

// StatusClass buckets HTTP status codes for bounded status labels.
func StatusClass(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "2xx"
	case status >= 400 && status < 500:
		return "4xx"
	case status >= 500:
		return "5xx"
	default:
		return "other"
	}
}

// RouteTemplate normalizes a request path into a bounded route label by
// replacing UUID, numeric, ses_*, and wdg_* segments with {id}. Everything
// else passes through, so unknown paths stay visible without exploding
// cardinality.
func RouteTemplate(path string) string {
	if !strings.HasPrefix(path, "/") {
		return "other"
	}
	segs := strings.Split(strings.Trim(path, "/"), "/")
	for i, s := range segs {
		if isIDSegment(s) {
			segs[i] = "{id}"
		}
	}
	out := "/" + strings.Join(segs, "/")
	if len(out) > 128 {
		return "other"
	}
	return out
}

func isIDSegment(s string) bool {
	if s == "" {
		return false
	}
	if isUUID(s) {
		return true
	}
	if strings.HasPrefix(s, "ses_") || strings.HasPrefix(s, "wdg_") {
		return true
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < 36; i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

// --- instruments ---

type labelValues []string

func valuesKey(v labelValues) string { return strings.Join(v, "\x00") }

// Counter counts monotonically increasing events.
type Counter struct {
	mu     sync.Mutex
	name   string
	help   string
	labels []LabelDef
	vals   map[string]float64
	keys   map[string]labelValues
}

func (c *Counter) Inc(labelVals ...string) { c.Add(1, labelVals...) }

// Add increments by delta (negative deltas are ignored).
func (c *Counter) Add(delta float64, labelVals ...string) {
	if delta < 0 || math.IsNaN(delta) || math.IsInf(delta, 0) {
		return
	}
	nv := c.normalize(labelVals)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals[valuesKey(nv)] += delta
	if _, ok := c.keys[valuesKey(nv)]; !ok {
		c.keys[valuesKey(nv)] = append(labelValues(nil), nv...)
	}
}

func (c *Counter) normalize(labelVals []string) labelValues {
	nv := make(labelValues, len(c.labels))
	for i, def := range c.labels {
		v := ""
		if i < len(labelVals) {
			v = labelVals[i]
		}
		nv[i] = normalizeLabelValue(def, v)
	}
	return nv
}

// Histogram observes distributions with cumulative buckets.
type Histogram struct {
	mu      sync.Mutex
	name    string
	help    string
	labels  []LabelDef
	buckets []float64
	counts  map[string][]uint64
	sums    map[string]float64
	keys    map[string]labelValues
}

// DefaultLatencyBuckets covers 5ms..30s for request/runtime latencies.
var DefaultLatencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

// Observe records one sample in seconds (negative/NaN samples are ignored).
func (h *Histogram) Observe(seconds float64, labelVals ...string) {
	if seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return
	}
	nv := make(labelValues, len(h.labels))
	for i, def := range h.labels {
		v := ""
		if i < len(labelVals) {
			v = labelVals[i]
		}
		nv[i] = normalizeLabelValue(def, v)
	}
	k := valuesKey(nv)
	h.mu.Lock()
	defer h.mu.Unlock()
	bc, ok := h.counts[k]
	if !ok {
		bc = make([]uint64, len(h.buckets))
		h.counts[k] = bc
		h.keys[k] = append(labelValues(nil), nv...)
	}
	for i, b := range h.buckets {
		if seconds <= b {
			bc[i]++
		}
	}
	h.sums[k] += seconds
}

// Gauge holds a point-in-time value (e.g. readiness, queue depth).
type Gauge struct {
	mu     sync.Mutex
	name   string
	help   string
	labels []LabelDef
	vals   map[string]float64
	keys   map[string]labelValues
}

// Set records the current value (NaN/Inf ignored).
func (g *Gauge) Set(v float64, labelVals ...string) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	nv := make(labelValues, len(g.labels))
	for i, def := range g.labels {
		val := ""
		if i < len(labelVals) {
			val = labelVals[i]
		}
		nv[i] = normalizeLabelValue(def, val)
	}
	k := valuesKey(nv)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.vals[k] = v
	if _, ok := g.keys[k]; !ok {
		g.keys[k] = append(labelValues(nil), nv...)
	}
}

// Registry owns all instruments and renders the exposition format.
type Registry struct {
	mu         sync.Mutex
	counters   []*Counter
	histograms []*Histogram
	gauges     []*Gauge
	callbacks  []func()
}

// RegisterCallback adds a scrape-time hook run at the start of Render.
// Callbacks must be fast, bounded, and never fail the scrape.
func (r *Registry) RegisterCallback(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callbacks = append(r.callbacks, fn)
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

func allowSet(values ...string) map[string]bool {
	m := make(map[string]bool, len(values))
	for _, v := range values {
		m[v] = true
	}
	return m
}

// Shared bounded label dimensions.
var (
	LabelMethod     = LabelDef{Name: "method", Allowed: allowSet("GET", "POST", "PATCH", "PUT", "DELETE")}
	LabelStatus     = LabelDef{Name: "status", Allowed: allowSet("2xx", "4xx", "5xx", "other")}
	LabelRoute      = LabelDef{Name: "route"} // bounded by RouteTemplate normalization
	LabelService    = LabelDef{Name: "service"}
	LabelOperation  = LabelDef{Name: "operation"}
	LabelDependency = LabelDef{Name: "dependency"}
	LabelOutcome    = LabelDef{Name: "outcome", Allowed: allowSet("ok", "error")}
)

// RegisterCounter creates and tracks a counter.
func (r *Registry) RegisterCounter(name, help string, labels ...LabelDef) *Counter {
	c := &Counter{name: name, help: help, labels: labels, vals: map[string]float64{}, keys: map[string]labelValues{}}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters = append(r.counters, c)
	return c
}

// RegisterHistogram creates and tracks a histogram.
func (r *Registry) RegisterHistogram(name, help string, buckets []float64, labels ...LabelDef) *Histogram {
	if len(buckets) == 0 {
		buckets = DefaultLatencyBuckets
	}
	h := &Histogram{name: name, help: help, labels: labels, buckets: append([]float64(nil), buckets...),
		counts: map[string][]uint64{}, sums: map[string]float64{}, keys: map[string]labelValues{}}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.histograms = append(r.histograms, h)
	return h
}

// RegisterGauge creates and tracks a gauge.
func (r *Registry) RegisterGauge(name, help string, labels ...LabelDef) *Gauge {
	g := &Gauge{name: name, help: help, labels: labels, vals: map[string]float64{}, keys: map[string]labelValues{}}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges = append(r.gauges, g)
	return g
}

func formatLabels(names []string, vals labelValues) string {
	if len(names) == 0 {
		return ""
	}
	parts := make([]string, len(names))
	for i, n := range names {
		v := ""
		if i < len(vals) {
			v = vals[i]
		}
		parts[i] = fmt.Sprintf(`%s=%q`, n, v)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func labelNames(defs []LabelDef) []string {
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
}

// Render produces the Prometheus text exposition for all instruments.
// Output ordering is deterministic (sorted series) for stable tests.
func (r *Registry) Render() string {
	r.mu.Lock()
	callbacks := append([]func(){}, r.callbacks...)
	counters := append([]*Counter(nil), r.counters...)
	histograms := append([]*Histogram(nil), r.histograms...)
	gauges := append([]*Gauge(nil), r.gauges...)
	r.mu.Unlock()
	for _, fn := range callbacks {
		func() {
			defer func() { _ = recover() }()
			fn()
		}()
	}

	var b strings.Builder
	for _, c := range counters {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s counter\n", c.name, c.help, c.name)
		c.mu.Lock()
		keys := sortedKeys(c.keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s%s %v\n", c.name, formatLabels(labelNames(c.labels), c.keys[k]), c.vals[k])
		}
		c.mu.Unlock()
	}
	for _, h := range histograms {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s histogram\n", h.name, h.help, h.name)
		h.mu.Lock()
		keys := sortedKeys(h.keys)
		for _, k := range keys {
			lv := h.keys[k]
			names := append(append([]string(nil), labelNames(h.labels)...), "le")
			for i, bound := range h.buckets {
				fmt.Fprintf(&b, "%s%s %d\n", h.name+"_bucket", formatLabels(names, append(append(labelValues(nil), lv...), formatBound(bound))), h.counts[k][i])
			}
			total := h.counts[k][len(h.counts[k])-1] // +Inf bucket: all observations
			fmt.Fprintf(&b, "%s%s %d\n", h.name+"_bucket", formatLabels(names, append(append(labelValues(nil), lv...), "+Inf")), total)
			fmt.Fprintf(&b, "%s_sum%s %v\n", h.name, formatLabels(labelNames(h.labels), lv), h.sums[k])
			fmt.Fprintf(&b, "%s_count%s %d\n", h.name, formatLabels(labelNames(h.labels), lv), total)
		}
		h.mu.Unlock()
	}
	for _, g := range gauges {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n", g.name, g.help, g.name)
		g.mu.Lock()
		keys := sortedKeys(g.keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s%s %v\n", g.name, formatLabels(labelNames(g.labels), g.keys[k]), g.vals[k])
		}
		g.mu.Unlock()
	}
	return b.String()
}

func sortedKeys(m map[string]labelValues) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func formatBound(b float64) string {
	s := fmt.Sprintf("%v", b)
	return s
}
