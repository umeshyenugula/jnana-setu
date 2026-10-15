package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RawResult is the runtime's parsed completion before policy post-processing.
type RawResult struct {
	Text            string
	FinishReason    string
	PromptTokens    int
	CompletionTokens int
}

// Runtime generates text against a local model backend. The interface keeps
// llama.cpp specifics behind a seam so unit tests use a mock and Module 6
// never depends on runtime internals.
type Runtime interface {
	// Generate produces one completion. It must honor ctx cancellation.
	Generate(ctx context.Context, req GenerationRequest, settings GenerationSettings) (RawResult, error)
	// Ready reports whether the model is loaded and able to generate.
	Ready(ctx context.Context) error
	Name() string
}

// LlamaCppConfig points at the local llama-server instance. The server must
// bind loopback or a private service network; it is never public.
type LlamaCppConfig struct {
	BaseURL        string
	Model          string
	EnableThinking *bool
	HTTPClient     *http.Client
}

// LlamaCppRuntime speaks to llama.cpp's local OpenAI-compatible HTTP
// interface (POST /v1/chat/completions, GET /health). This is a local
// protocol integration, not a hosted provider call: the base URL is
// restricted to loopback/private targets by configuration.
type LlamaCppRuntime struct {
	cfg    LlamaCppConfig
	client *http.Client
}

func NewLlamaCppRuntime(cfg LlamaCppConfig) *LlamaCppRuntime {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 180 * time.Second}
	}
	return &LlamaCppRuntime{cfg: cfg, client: client}
}

func (r *LlamaCppRuntime) Name() string { return "llamacpp:" + r.cfg.Model }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Temperature    float64       `json:"temperature"`
	TopP           float64       `json:"top_p"`
	MaxTokens      int           `json:"max_tokens"`
	Stop           []string      `json:"stop,omitempty"`
	Stream         bool          `json:"stream"`
	EnableThinking *bool         `json:"enable_thinking,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error any `json:"error"`
}

func (r *LlamaCppRuntime) Generate(ctx context.Context, req GenerationRequest, settings GenerationSettings) (RawResult, error) {
	msgs := make([]chatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, chatMessage{Role: m.Role, Content: m.Content})
	}
	payload, err := json.Marshal(chatRequest{
		Model:          r.cfg.Model,
		Messages:       msgs,
		Temperature:    derefFloat(settings.Temperature),
		TopP:           derefFloat(settings.TopP),
		MaxTokens:      derefInt(settings.MaxTokens),
		Stop:           settings.Stop,
		Stream:         false,
		EnableThinking: r.cfg.EnableThinking,
	})
	if err != nil {
		return RawResult{}, &ServiceError{Code: CodeInternalError, Message: "could not encode runtime request", Status: 500}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.cfg.BaseURL, "/")+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return RawResult{}, &ServiceError{Code: CodeInternalError, Message: "could not build runtime request", Status: 500}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(httpReq)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return RawResult{}, &ServiceError{Code: CodeGatewayTimeout, Message: "model runtime timed out", Status: 504}
		}
		if ctx.Err() == context.Canceled {
			return RawResult{}, &ServiceError{Code: CodeGenerationFailed, Message: "generation cancelled", Status: 499}
		}
		return RawResult{}, &ServiceError{Code: CodeGenerationFailed, Message: "model runtime unavailable", Status: 502}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return RawResult{}, &ServiceError{Code: CodeGenerationFailed, Message: "could not read runtime response", Status: 502}
	}
	if resp.StatusCode == http.StatusServiceUnavailable {
		return RawResult{}, &ServiceError{Code: CodeModelNotReady, Message: "model is not ready", Status: 503}
	}
	if resp.StatusCode != http.StatusOK {
		return RawResult{}, &ServiceError{Code: CodeGenerationFailed, Message: fmt.Sprintf("model runtime error: %s", resp.Status), Status: 502}
	}
	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return RawResult{}, &ServiceError{Code: CodeGenerationFailed, Message: "malformed runtime response", Status: 502}
	}
	if len(parsed.Choices) == 0 {
		return RawResult{}, &ServiceError{Code: CodeGenerationFailed, Message: "empty runtime response", Status: 502}
	}
	reason := parsed.Choices[0].FinishReason
	if reason == "" {
		reason = FinishStop
	}
	return RawResult{
		Text:             parsed.Choices[0].Message.Content,
		FinishReason:     reason,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
	}, nil
}

// Ready probes GET /health on the local runtime with the caller's context.
func (r *LlamaCppRuntime) Ready(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.cfg.BaseURL, "/")+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := r.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("runtime health: %s", resp.Status)
	}
	return nil
}

// MockRuntime is a deterministic stand-in for unit tests and for Module 6
// development without model weights. It never generates real language-model
// output; responses are clearly labeled.
type MockRuntime struct {
	Text     string
	NotReady bool
	Delay    time.Duration
	OnCall   func()
}

func (m *MockRuntime) Name() string { return "mock" }

func (m *MockRuntime) Ready(ctx context.Context) error {
	_ = ctx
	if m.NotReady {
		return fmt.Errorf("mock model not loaded")
	}
	return nil
}

func (m *MockRuntime) Generate(ctx context.Context, req GenerationRequest, _ GenerationSettings) (RawResult, error) {
	if m.OnCall != nil {
		m.OnCall()
	}
	if m.Delay > 0 {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return RawResult{}, &ServiceError{Code: CodeGatewayTimeout, Message: "model runtime timed out", Status: 504}
			}
			return RawResult{}, &ServiceError{Code: CodeGenerationFailed, Message: "generation cancelled", Status: 499}
		case <-time.After(m.Delay):
		}
	}
	last := ""
	for _, msg := range req.Messages {
		if msg.Role == RoleUser {
			last = msg.Content
		}
	}
	text := m.Text
	if text == "" {
		text = "mock response to: " + last
	}
	return RawResult{Text: text, FinishReason: FinishStop, PromptTokens: 10, CompletionTokens: 5}, nil
}

func derefFloat(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
