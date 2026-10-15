package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"example.com/ai-knowledge-assistant/services/api/internal/auth"
	"example.com/ai-knowledge-assistant/services/api/internal/config"
	"example.com/ai-knowledge-assistant/services/api/internal/identity"
	"example.com/ai-knowledge-assistant/services/api/internal/knowledge"
	"example.com/ai-knowledge-assistant/services/api/internal/processing"
	"example.com/ai-knowledge-assistant/services/api/internal/retrieval"
	"example.com/ai-knowledge-assistant/services/api/internal/router"
	"example.com/ai-knowledge-assistant/services/api/internal/version"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	var store identity.Store = identity.NewMemoryStore()
	var knowledgeStore knowledge.Store = knowledge.NewMemoryStore()
	var knowledgeStorage knowledge.Storage = knowledge.NewMemoryStorage()
	var pool *pgxpool.Pool
	if cfg.SupabaseDatabaseURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		p, err := pgxpool.New(ctx, cfg.SupabaseDatabaseURL)
		cancel()
		if err != nil {
			logger.Error("database connection failed", "error", err)
			os.Exit(1)
		}
		pool = p
		defer pool.Close()
		store = identity.NewPostgresStore(pool)
		knowledgeStore = knowledge.NewPostgresStore(pool)
		if cfg.SupabaseURL != "" && cfg.SupabaseServiceKey != "" {
			knowledgeStorage = knowledge.NewSupabaseStorage(cfg.SupabaseURL, cfg.SupabaseServiceKey)
		}
	}

	service := identity.NewService(store)
	handlers := &identity.Handlers{Service: service, Version: version.Version}
	knowledgeService := knowledge.NewService(knowledgeStore, knowledgeStorage, store)
	knowledgeHandlers := &knowledge.Handlers{Service: knowledgeService, Version: version.Version}

	// Module 3: local embedding worker + processing pipeline.
	// EMBEDDING_PROVIDER=stub selects the dependency-free deterministic
	// embedder (tests/dev without model weights); anything else uses the
	// local HTTP worker over loopback. Hosted inference is never used.
	var embedder processing.Embedder = &processing.StubEmbedder{}
	if strings.ToLower(strings.TrimSpace(cfg.EmbeddingProvider)) != "stub" {
		embedder = processing.NewHTTPWorker(cfg.EmbeddingWorkerURL, 0)
	}
	memProc := processing.NewMemoryStores()
	var procJobs processing.JobStore = memProc
	var procChunks processing.ChunkStore = memProc
	if pool != nil {
		pgStores := processing.NewPostgresStores(pool)
		procJobs = pgStores
		procChunks = pgStores
	}
	procCfg := processing.DefaultConfig()
	procCfg.Chunks = processing.ChunkConfig{TargetTokens: cfg.ChunkTargetTokens, OverlapTokens: cfg.ChunkOverlapTokens}
	module2 := processing.NewModule2Adapter(knowledgeStore, knowledgeStorage, knowledgeService)
	processingService := processing.NewService(procCfg, procJobs, procChunks, embedder, module2, module2, store, module2)
	processingHandlers := &processing.Handlers{Service: processingService, Version: version.Version}
	processingService.Start(context.Background(), cfg.ProcessingWorkers)

	// Module 4: retrieval engine over the same local embedder and pool.
	// Without a database the memory repository serves empty results
	// (insufficient evidence) rather than failing.
	var chunkRepo retrieval.ChunkRepository = retrieval.NewMemoryRepository()
	if pool != nil {
		chunkRepo = retrieval.NewPostgresRepository(pool)
	}
	retrievalCfg := retrieval.DefaultConfig()
	retrievalCfg.Rank.TopK = cfg.RetrievalTopK
	retrievalCfg.MaxTopK = cfg.RetrievalMaxTopK
	retrievalCfg.Rank.MinSimilarity = cfg.RetrievalMinSimilarity
	retrievalCfg.Rank.MaxEvidenceTokens = cfg.RetrievalMaxTokens
	retrievalCfg.EmbedTimeout = time.Duration(cfg.RetrievalEmbedTimeoutS) * time.Second
	retrievalService := retrieval.NewService(retrievalCfg, chunkRepo, embedder)
	retrievalHandlers := &retrieval.Handlers{
		Service: retrievalService,
		Auth:    retrieval.InternalAuth{Token: cfg.InternalServiceToken},
		Version: version.Version,
	}
	verifier := auth.NewVerifier(auth.VerifierConfig{
		URL:       cfg.SupabaseURL,
		JWTSecret: cfg.SupabaseJWTSecret,
		Issuer:    cfg.SupabaseJWTIssuer,
		Audience:  cfg.SupabaseJWTAudience,
	})

	handler := router.New(router.Deps{
		Version:    version.Version,
		Auth:       verifier,
		Handlers:   handlers,
		Knowledge:  knowledgeHandlers,
		Processing: processingHandlers,
		Retrieval:  retrievalHandlers,
	})

	server := &http.Server{
		Addr:              ":" + itoa(cfg.Port),
		Handler:           handler,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	logger.Info("starting api", "addr", server.Addr, "env", cfg.AppEnv, "version", version.Version)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
