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
	EmbeddingProvider   string
	EmbeddingWorkerURL  string
	EmbeddingModelDir   string
	ProcessingWorkers   int
	ChunkTargetTokens   int
	ChunkOverlapTokens  int
	// Module 4 retrieval engine (internal service-to-service only).
	InternalServiceToken   string
	RetrievalTopK          int
	RetrievalMaxTopK       int
	RetrievalMinSimilarity float64
	RetrievalMaxTokens     int
	RetrievalEmbedTimeoutS int
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
		EmbeddingProvider:   envString("EMBEDDING_PROVIDER", "http"),
		EmbeddingWorkerURL:  envString("EMBEDDING_WORKER_URL", "http://127.0.0.1:8082"),
		EmbeddingModelDir:   envString("EMBEDDING_MODEL_DIR", "./models/all-MiniLM-L6-v2"),
		ProcessingWorkers:   envInt("PROCESSING_WORKERS", 2),
		ChunkTargetTokens:   envInt("CHUNK_TARGET_TOKENS", 200),
		ChunkOverlapTokens:  envInt("CHUNK_OVERLAP_TOKENS", 35),
		InternalServiceToken:   os.Getenv("INTERNAL_SERVICE_TOKEN"),
		RetrievalTopK:          envInt("RETRIEVAL_TOP_K", 5),
		RetrievalMaxTopK:       envInt("RETRIEVAL_MAX_TOP_K", 20),
		RetrievalMinSimilarity: envFloat("RETRIEVAL_MIN_SIMILARITY", 0.1),
		RetrievalMaxTokens:     envInt("RETRIEVAL_MAX_EVIDENCE_TOKENS", 4000),
		RetrievalEmbedTimeoutS: envInt("RETRIEVAL_EMBED_TIMEOUT_S", 30),
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
