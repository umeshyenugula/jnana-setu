package observability

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Middleware instruments every request without touching business logic:
// structured completion log, Prometheus request/latency metrics with bounded
// labels, and a server span (sampled). It runs after CorrelationMiddleware so
// correlation IDs are always present. Bodies are never read or logged.

// Instruments wires the shared registry instruments for HTTP traffic.
type Instruments struct {
	Requests *Counter
	Latency  *Histogram
}

// NewInstruments registers HTTP instruments on reg.
func NewInstruments(reg *Registry) *Instruments {
	return &Instruments{
		Requests: reg.RegisterCounter(
			"http_requests_total",
			"HTTP requests by method, route template, and status class.",
			LabelMethod, LabelRoute, LabelStatus,
		),
		Latency: reg.RegisterHistogram(
			"http_request_duration_seconds",
			"HTTP request latency in seconds by method, route template, and status class.",
			DefaultLatencyBuckets,
			LabelMethod, LabelRoute, LabelStatus,
		),
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(status int) {
	if !w.wrote {
		w.wrote = true
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Stack bundles the request-scoped observability pieces for HTTP wiring:
// completion logs, Prometheus instruments, and server spans.
type Stack struct {
	Log    *Logger
	Inst   *Instruments
	Tracer *Tracer
}

// Wrap instruments next. Safe to use when fields are nil (each stage skips).
func (s *Stack) Wrap(next http.Handler) http.Handler {
	if s == nil {
		return next
	}
	return Middleware(s.Log, s.Inst, s.Tracer)(next)
}

// Middleware returns instrumentation middleware. A nil tracer disables
// spans; a nil logger disables request logs; instruments are always recorded.
func Middleware(log *Logger, inst *Instruments, tracer *Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ctx := r.Context()

			var span *SpanHandle
			if tracer != nil {
				if parent, ok := ExtractTraceParent(r); ok {
					ctx = WithTrace(ctx, parent.TraceID, parent.SpanID)
				}
				var child context.Context
				child, span = tracer.Start(ctx, "http.server "+r.Method+" "+RouteTemplate(r.URL.Path), map[string]string{
					"http.method": r.Method,
					"http.route":  RouteTemplate(r.URL.Path),
				})
				ctx = child
				r = r.WithContext(ctx)
			}

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)

			elapsed := time.Since(start)
			route := RouteTemplate(r.URL.Path)
			class := StatusClass(sw.status)
			if inst != nil {
				inst.Requests.Inc(r.Method, route, class)
				inst.Latency.Observe(elapsed.Seconds(), r.Method, route, class)
			}
			if span != nil {
				status := "ok"
				if sw.status >= 500 {
					status = "error"
				}
				span.End(status, "")
			}
			if log != nil {
				log.Completed(ctx, "http.request", statusWord(sw.status), elapsed.Milliseconds(),
					slog.String("http.method", r.Method),
					slog.String("http.route", route),
					slog.Int("http.status", sw.status),
				)
			}
		})
	}
}

func statusWord(status int) string {
	if status >= 500 {
		return "error"
	}
	return "ok"
}
