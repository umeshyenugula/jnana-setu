package inference

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Config bounds the inference service. Every value is env-configurable and
// documented; callers cannot override model identity or runtime arguments.
type Config struct {
	Model            string
	RequestTimeout   time.Duration
	MaxConcurrent    int
	Defaults         ResolvedSettings
	Policy           Policy
	EnableThinking   *bool
	RuntimeProbeTime time.Duration
}

func DefaultConfig() Config {
	return Config{
		Model:            "qwen3-4b",
		RequestTimeout:   120 * time.Second,
		MaxConcurrent:    1,
		Defaults:         ResolvedSettings{Temperature: 0.3, TopP: 0.9, MaxTokens: 512},
		Policy:           DefaultPolicy(),
		RuntimeProbeTime: 3 * time.Second,
	}
}

// Service orchestrates validated generation with bounded concurrency,
// timeouts, cancellation, metrics, and graceful drain.
type Service struct {
	cfg     Config
	runtime Runtime
	metrics *Metrics
	slots   chan struct{}
	wg      sync.WaitGroup
}

func NewService(cfg Config, runtime Runtime) *Service {
	if cfg.MaxConcurrent < 1 {
		cfg.MaxConcurrent = 1
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 120 * time.Second
	}
	return &Service{
		cfg:     cfg,
		runtime: runtime,
		metrics: &Metrics{},
		slots:   make(chan struct{}, cfg.MaxConcurrent),
	}
}

func (s *Service) Metrics() *Metrics { return s.metrics }

// Generate validates the request, acquires a bounded generation slot,
// calls the local runtime with timeout/cancellation, strips reasoning
// markers, enforces output limits, and records metrics.
func (s *Service) Generate(ctx context.Context, req GenerationRequest) (GenerationResult, error) {
	settings, err := ValidateRequest(req, s.cfg.Defaults, s.cfg.Policy)
	if err != nil {
		return GenerationResult{}, err
	}
	select {
	case s.slots <- struct{}{}:
	default:
		s.metrics.recordFailure(CodeModelOverloaded)
		return GenerationResult{}, &ServiceError{Code: CodeModelOverloaded, Message: "model is busy; retry shortly", Status: 429}
	}
	defer func() { <-s.slots }()
	s.wg.Add(1)
	defer s.wg.Done()

	s.metrics.recordRequest()
	start := time.Now()
	genCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	raw, err := s.runtime.Generate(genCtx, req, settings)
	if err != nil {
		code := CodeGenerationFailed
		if se, ok := err.(*ServiceError); ok {
			code = se.Code
			s.metrics.recordFailure(code)
			return GenerationResult{}, se
		}
		if genCtx.Err() == context.DeadlineExceeded {
			code = CodeGatewayTimeout
		}
		s.metrics.recordFailure(code)
		return GenerationResult{}, &ServiceError{Code: code, Message: "text generation failed", Status: 502}
	}
	text, stripped := StripThinking(raw.Text)
	if strings.TrimSpace(text) == "" {
		s.metrics.recordFailure(CodeGenerationFailed)
		return GenerationResult{}, &ServiceError{Code: CodeGenerationFailed, Message: "model returned empty output", Status: 502}
	}
	if len([]rune(text)) > s.cfg.Policy.MaxResponseChars {
		text = string([]rune(text)[:s.cfg.Policy.MaxResponseChars])
	}
	reason := raw.FinishReason
	if reason == "" {
		reason = FinishStop
	}
	s.metrics.recordSuccess(raw.PromptTokens, raw.CompletionTokens, time.Since(start))
	return GenerationResult{
		Text:             text,
		FinishReason:     reason,
		PromptTokens:     raw.PromptTokens,
		CompletionTokens: raw.CompletionTokens,
		Model:            s.cfg.Model,
		ThinkingStripped: stripped,
	}, nil
}

// CheckReady verifies the local model is loaded. Model-loading failures are
// counted and reported with a stable code; runtime details stay server-side.
func (s *Service) CheckReady(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.RuntimeProbeTime)
	defer cancel()
	if err := s.runtime.Ready(ctx); err != nil {
		s.metrics.recordModelLoadError()
		return &ServiceError{Code: CodeModelNotReady, Message: "model is not ready", Status: 503}
	}
	return nil
}

// Shutdown waits for in-flight generations to finish, bounded by ctx.
func (s *Service) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
