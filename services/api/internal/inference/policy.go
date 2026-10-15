package inference

import (
	"strings"
)

// Policy bounds enforced on every generation request. Values come from server
// configuration; callers supply only the approved subset of settings.
type Policy struct {
	MaxMessages      int
	MaxPromptChars   int
	MaxResponseChars int
	MaxTokens        int
	MinTemperature   float64
	MaxTemperature   float64
	MaxStopSequences int
	MaxStopChars     int
}

func DefaultPolicy() Policy {
	return Policy{
		MaxMessages:      20,
		MaxPromptChars:   12000,
		MaxResponseChars: 8000,
		MaxTokens:        512,
		MinTemperature:   0,
		MaxTemperature:   2,
		MaxStopSequences: 4,
		MaxStopChars:     50,
	}
}

func validRole(role string) bool {
	return role == RoleSystem || role == RoleUser || role == RoleAssistant
}

// ValidateRequest enforces message shape, roles, sizes, and the approved
// generation-parameter ranges. It returns normalized settings with server
// defaults applied for every omitted field.
func ValidateRequest(req GenerationRequest, defaults ResolvedSettings, p Policy) (GenerationSettings, error) {
	if len(req.Messages) == 0 {
		return GenerationSettings{}, &ServiceError{Code: CodeInvalidRequest, Message: "at least one message is required", Status: 400}
	}
	if len(req.Messages) > p.MaxMessages {
		return GenerationSettings{}, &ServiceError{Code: CodeInvalidRequest, Message: "too many messages", Status: 400}
	}
	total := 0
	hasUser := false
	for i, m := range req.Messages {
		if !validRole(m.Role) {
			return GenerationSettings{}, &ServiceError{Code: CodeInvalidRequest, Message: "invalid message role", Status: 400}
		}
		text := strings.TrimSpace(m.Content)
		if text == "" {
			return GenerationSettings{}, &ServiceError{Code: CodeInvalidRequest, Message: "message content must not be empty", Status: 400}
		}
		_ = i
		total += len([]rune(m.Content))
		if m.Role == RoleUser {
			hasUser = true
		}
	}
	if !hasUser {
		return GenerationSettings{}, &ServiceError{Code: CodeInvalidRequest, Message: "at least one user message is required", Status: 400}
	}
	if total > p.MaxPromptChars {
		return GenerationSettings{}, &ServiceError{Code: CodeInvalidRequest, Message: "prompt exceeds maximum size", Status: 400}
	}
	out := GenerationSettings{
		Temperature: &defaults.Temperature,
		TopP:        &defaults.TopP,
		MaxTokens:   &defaults.MaxTokens,
	}
	if req.Settings.Temperature != nil {
		t := *req.Settings.Temperature
		if t < p.MinTemperature || t > p.MaxTemperature {
			return GenerationSettings{}, &ServiceError{Code: CodeInvalidRequest, Message: "temperature out of range", Status: 400}
		}
		out.Temperature = &t
	}
	if req.Settings.TopP != nil {
		tp := *req.Settings.TopP
		if tp <= 0 || tp > 1 {
			return GenerationSettings{}, &ServiceError{Code: CodeInvalidRequest, Message: "topP must be in (0, 1]", Status: 400}
		}
		out.TopP = &tp
	}
	if req.Settings.MaxTokens != nil {
		mt := *req.Settings.MaxTokens
		if mt < 1 || mt > p.MaxTokens {
			return GenerationSettings{}, &ServiceError{Code: CodeInvalidRequest, Message: "maxTokens out of range", Status: 400}
		}
		out.MaxTokens = &mt
	}
	if len(req.Settings.Stop) > p.MaxStopSequences {
		return GenerationSettings{}, &ServiceError{Code: CodeInvalidRequest, Message: "too many stop sequences", Status: 400}
	}
	for _, s := range req.Settings.Stop {
		if s == "" || len([]rune(s)) > p.MaxStopChars {
			return GenerationSettings{}, &ServiceError{Code: CodeInvalidRequest, Message: "invalid stop sequence", Status: 400}
		}
	}
	out.Stop = append([]string(nil), req.Settings.Stop...)
	return out, nil
}

// ResolvedSettings holds the server-configured generation defaults.
type ResolvedSettings struct {
	Temperature float64
	TopP        float64
	MaxTokens   int
}

// StripThinking removes Qwen3 <think>...</think> reasoning blocks (and a
// dangling unclosed opener) so internal reasoning markers never surface as
// the customer-facing answer. Returns the cleaned text and whether anything
// was stripped.
func StripThinking(text string) (string, bool) {
	stripped := false
	for {
		start := strings.Index(text, "<think>")
		if start < 0 {
			break
		}
		stripped = true
		rest := text[start+len("<think>"):]
		if end := strings.Index(rest, "</think>"); end >= 0 {
			text = text[:start] + rest[end+len("</think>"):]
		} else {
			text = text[:start] // unclosed opener: drop it and everything after
			break
		}
	}
	return strings.TrimSpace(text), stripped
}
