package config

import (
	"os"
	"strconv"
)

// Config holds API process configuration. All values come from environment variables.
type Config struct {
	AppEnv               string
	Port                 int
	CORSAllowedOrigins   string
	SupabaseURL          string
	SupabaseAnonKey      string
	SupabaseServiceKey   string
	SupabaseDatabaseURL  string
	SupabaseJWTSecret    string
	SupabaseJWTIssuer    string
	SupabaseJWTAudience  string
	KnowledgeMaxUploadMB int
	// Module 3 document processing & local embeddings.
	EmbeddingWorkerURL  string
	EmbeddingModelDir   string
	ProcessingWorkers   int
	ChunkTargetTokens   int
	ChunkOverlapTokens  int
	ProcessingRelayIntervalS int
	// Module 4 retrieval engine (internal service-to-service only).
	InternalServiceToken   string
	RetrievalTopK          int
	RetrievalMaxTopK       int
	RetrievalMinSimilarity float64
	RetrievalMaxTokens     int
	RetrievalEmbedTimeoutS int
	// Module 5 local inference (internal service-to-service only).
	LlamaCppBaseURL    string
	LLMModel           string
	LLMContextWindow   int
	LLMMaxOutputTokens int
	LLMTemperature     float64
	LLMTopP            float64
	LLMRequestTimeoutS int
	LLMMaxConcurrent   int
	LLMEnableThinking  bool
	LLMMaxPromptChars  int
	// Module 6 chat orchestration (public widget endpoint).
	ChatMaxMessageChars int
	ChatHistoryLimit    int
	ChatTopK            int
	ChatMaxPromptChars  int
	ChatFallbackText    string
	ChatRetentionDays   int
	ChatRateLimitPerMin int
	// Module 8 observability and operations.
	ObsLogLevel        string
	ObsHealthTimeoutS  int
	OtelEnabled        bool
	OtelEndpoint       string
	OtelSampleRatio    float64
}

// Load reads configuration from the process environment.
func Load() Config {
	return Config{
		AppEnv:              envString("APP_ENV", "development"),
		Port:                envInt("PORT", 8080),
		CORSAllowedOrigins:  envString("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:5174"),
		SupabaseURL:         os.Getenv("SUPABASE_URL"),
		SupabaseAnonKey:     os.Getenv("SUPABASE_ANON_KEY"),
		SupabaseServiceKey:  os.Getenv("SUPABASE_SERVICE_ROLE_KEY"),
		SupabaseDatabaseURL: os.Getenv("SUPABASE_DB_URL"),
		SupabaseJWTSecret:   os.Getenv("SUPABASE_JWT_SECRET"),
		SupabaseJWTIssuer:   os.Getenv("SUPABASE_JWT_ISSUER"),
		SupabaseJWTAudience: envString("SUPABASE_JWT_AUDIENCE", "authenticated"),
		KnowledgeMaxUploadMB: envInt("KNOWLEDGE_MAX_UPLOAD_MB", 10),
		EmbeddingWorkerURL:  envString("EMBEDDING_WORKER_URL", "http://127.0.0.1:8082"),
		EmbeddingModelDir:   envString("EMBEDDING_MODEL_DIR", "./models/all-MiniLM-L6-v2"),
		ProcessingWorkers:   envInt("PROCESSING_WORKERS", 2),
		ChunkTargetTokens:   envInt("CHUNK_TARGET_TOKENS", 200),
		ChunkOverlapTokens:  envInt("CHUNK_OVERLAP_TOKENS", 35),
		ProcessingRelayIntervalS: envInt("PROCESSING_RELAY_INTERVAL_S", 10),
		InternalServiceToken:   os.Getenv("INTERNAL_SERVICE_TOKEN"),
		RetrievalTopK:          envInt("RETRIEVAL_TOP_K", 5),
		RetrievalMaxTopK:       envInt("RETRIEVAL_MAX_TOP_K", 20),
		RetrievalMinSimilarity: envFloat("RETRIEVAL_MIN_SIMILARITY", 0.1),
		RetrievalMaxTokens:     envInt("RETRIEVAL_MAX_EVIDENCE_TOKENS", 4000),
		RetrievalEmbedTimeoutS: envInt("RETRIEVAL_EMBED_TIMEOUT_S", 30),
		LlamaCppBaseURL:    envString("LLAMA_CPP_BASE_URL", "http://localhost:8081"),
		LLMModel:           envString("LLM_MODEL", "qwen3-4b"),
		LLMContextWindow:   envInt("LLM_CONTEXT_WINDOW", 4096),
		LLMMaxOutputTokens: envInt("LLM_MAX_OUTPUT_TOKENS", 512),
		LLMTemperature:     envFloat("LLM_TEMPERATURE", 0.3),
		LLMTopP:            envFloat("LLM_TOP_P", 0.9),
		LLMRequestTimeoutS: envInt("LLM_REQUEST_TIMEOUT_S", 120),
		LLMMaxConcurrent:   envInt("LLM_MAX_CONCURRENT", 1),
		LLMEnableThinking:  envBool("LLM_ENABLE_THINKING", false),
		LLMMaxPromptChars:  envInt("LLM_MAX_PROMPT_CHARS", 12000),
		ChatMaxMessageChars: envInt("CHAT_MAX_MESSAGE_CHARS", 2000),
		ChatHistoryLimit:    envInt("CHAT_HISTORY_LIMIT", 10),
		ChatTopK:            envInt("CHAT_TOP_K", 5),
		ChatMaxPromptChars:  envInt("CHAT_MAX_PROMPT_CHARS", 8000),
		ChatFallbackText:    envString("CHAT_FALLBACK_TEXT", ""),
		ChatRetentionDays:   envInt("CHAT_RETENTION_DAYS", 90),
		ChatRateLimitPerMin: envInt("CHAT_RATE_LIMIT_PER_MIN", 30),
		ObsLogLevel:        envString("OBS_LOG_LEVEL", "info"),
		ObsHealthTimeoutS:  envInt("OBS_HEALTH_TIMEOUT_S", 5),
		OtelEnabled:        envBool("OTEL_ENABLED", false),
		OtelEndpoint:       envString("OTEL_ENDPOINT", "http://localhost:4318"),
		OtelSampleRatio:    envFloat("OTEL_SAMPLE_RATIO", 0.1),
	}
}

func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envFloat(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
