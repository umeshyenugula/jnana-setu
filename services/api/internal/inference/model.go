package inference

// Model for the Module 5 inference service. Module 5 owns local text
// generation only. It never retrieves knowledge, orchestrates chat, manages
// tenants, or calls hosted providers. Generated text is untrusted data:
// never execute it as code.

// Roles accepted from Module 6's prepared messages.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Finish reasons returned in the predictable response field.
const (
	FinishStop   = "stop"
	FinishLength = "length"
	FinishError  = "error"
)

// Stable error codes for Module 6 consumption.
const (
	CodeInvalidRequest   = "INVALID_REQUEST"
	CodeUnauthorized     = "UNAUTHORIZED"
	CodeModelNotReady    = "MODEL_NOT_READY"
	CodeModelOverloaded  = "MODEL_OVERLOADED"
	CodeGenerationFailed = "GENERATION_FAILED"
	CodeGatewayTimeout   = "GENERATION_TIMEOUT"
	CodeInternalError    = "INTERNAL_ERROR"
)

// Message is one prepared chat message from Module 6.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GenerationSettings carries the approved per-request knobs. Callers cannot
// set model paths or arbitrary runtime arguments — those are server config.
type GenerationSettings struct {
	Temperature *float64 `json:"temperature"`
	TopP        *float64 `json:"topP"`
	MaxTokens   *int     `json:"maxTokens"`
	Stop        []string `json:"stop"`
}

// GenerationRequest is the body of POST /api/v1/internal/inference/generate.
type GenerationRequest struct {
	Messages []Message           `json:"messages"`
	Settings GenerationSettings  `json:"settings"`
}

// GenerationResult is the predictable success payload for Module 6.
type GenerationResult struct {
	Text              string `json:"text"`
	FinishReason      string `json:"finishReason"`
	PromptTokens      int    `json:"promptTokens"`
	CompletionTokens  int    `json:"completionTokens"`
	Model             string `json:"model"`
	ThinkingStripped  bool   `json:"thinkingStripped"`
}

// ServiceError carries a stable API error code + HTTP status.
type ServiceError struct {
	Code    string
	Message string
	Status  int
}

func (e *ServiceError) Error() string { return e.Message }
