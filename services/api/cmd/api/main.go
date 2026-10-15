package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"example.com/ai-knowledge-assistant/services/api/internal/auth"
	"example.com/ai-knowledge-assistant/services/api/internal/chat"
	"example.com/ai-knowledge-assistant/services/api/internal/config"
	"example.com/ai-knowledge-assistant/services/api/internal/identity"
	"example.com/ai-knowledge-assistant/services/api/internal/inference"
	"example.com/ai-knowledge-assistant/services/api/internal/knowledge"
	"example.com/ai-knowledge-assistant/services/api/internal/observability"
	"example.com/ai-knowledge-assistant/services/api/internal/processing"
	"example.com/ai-knowledge-assistant/services/api/internal/retrieval"
	"example.com/ai-knowledge-assistant/services/api/internal/router"
	"example.com/ai-knowledge-assistant/services/api/internal/version"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	var store identity.Store
	var knowledgeStore knowledge.Store
	var knowledgeStorage knowledge.Storage
	var pool *pgxpool.Pool
	if strings.TrimSpace(cfg.SupabaseDatabaseURL) == "" {
		logger.Error("SUPABASE_DB_URL is required; configure Supabase and run migrations 0001-0006")
		os.Exit(1)
	}
	if strings.TrimSpace(cfg.SupabaseURL) == "" || strings.TrimSpace(cfg.SupabaseServiceKey) == "" {
		logger.Error("SUPABASE_URL and SUPABASE_SERVICE_ROLE_KEY are required for private storage access")
		os.Exit(1)
	}
	{
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
		knowledgeStorage = knowledge.NewSupabaseStorage(cfg.SupabaseURL, cfg.SupabaseServiceKey)
	}

	service := identity.NewService(store)
	handlers := &identity.Handlers{Service: service, Version: version.Version}
	knowledge.ConfigureLimits(cfg.KnowledgeMaxUploadMB)
	knowledgeService := knowledge.NewService(knowledgeStore, knowledgeStorage, store)
	knowledgeHandlers := &knowledge.Handlers{Service: knowledgeService, Version: version.Version}

	// Module 3: local embedding worker + processing pipeline. The embedder
	// is always the real local worker (EMBEDDING_WORKER_URL); there is no
	// stub or hosted fallback in production. Hosted inference is never used.
	embedder := processing.NewHTTPWorker(cfg.EmbeddingWorkerURL, 0)
	pgStores := processing.NewPostgresStores(pool)
	var procJobs processing.JobStore = pgStores
	var procChunks processing.ChunkStore = pgStores
	procCfg := processing.DefaultConfig()
	procCfg.Chunks = processing.ChunkConfig{TargetTokens: cfg.ChunkTargetTokens, OverlapTokens: cfg.ChunkOverlapTokens}
	module2 := processing.NewModule2Adapter(knowledgeStore, knowledgeStorage, knowledgeService)
	processingService := processing.NewService(procCfg, procJobs, procChunks, embedder, module2, module2, store, module2)
	processingHandlers := &processing.Handlers{Service: processingService, Version: version.Version}
	processingService.Start(context.Background(), cfg.ProcessingWorkers)
	// Outbox relay: turn Module 2 upload/version/delete rows into ingestion
	// jobs. Without this loop nothing is ever indexed.
	processingService.StartRelay(context.Background(),
		time.Duration(cfg.ProcessingRelayIntervalS)*time.Second, 25)

	// Module 4: retrieval engine over the same local embedder and pool.
	var chunkRepo retrieval.ChunkRepository = retrieval.NewPostgresRepository(pool)
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

	// Module 5: local inference over the llama.cpp runtime on the private
	// network. There is no mock or hosted fallback in production.
	thinking := cfg.LLMEnableThinking
	llmRuntime := inference.NewLlamaCppRuntime(inference.LlamaCppConfig{
		BaseURL:        cfg.LlamaCppBaseURL,
		Model:          cfg.LLMModel,
		EnableThinking: &thinking,
	})
	inferenceCfg := inference.DefaultConfig()
	inferenceCfg.Model = cfg.LLMModel
	inferenceCfg.RequestTimeout = time.Duration(cfg.LLMRequestTimeoutS) * time.Second
	inferenceCfg.MaxConcurrent = cfg.LLMMaxConcurrent
	inferenceCfg.Defaults = inference.ResolvedSettings{
		Temperature: cfg.LLMTemperature,
		TopP:        cfg.LLMTopP,
		MaxTokens:   cfg.LLMMaxOutputTokens,
	}
	inferenceCfg.Policy.MaxTokens = cfg.LLMMaxOutputTokens
	inferenceCfg.Policy.MaxPromptChars = cfg.LLMMaxPromptChars
	inferenceService := inference.NewService(inferenceCfg, llmRuntime)
	inferenceHandlers := &inference.Handlers{
		Service: inferenceService,
		Auth:    inference.InternalAuth{Token: cfg.InternalServiceToken},
		Version: version.Version,
	}

	// Module 6: chat orchestration over Module 1 tenants, Module 4
	// retrieval, Module 5 generation, and durable chat persistence.
	chatStore := chat.NewPostgresStore(pool)
	chatCfg := chat.DefaultConfig()
	chatCfg.MaxMessageChars = cfg.ChatMaxMessageChars
	chatCfg.HistoryLimit = cfg.ChatHistoryLimit
	chatCfg.RetrievalTopK = cfg.ChatTopK
	chatCfg.MaxPromptChars = cfg.ChatMaxPromptChars
	if strings.TrimSpace(cfg.ChatFallbackText) != "" {
		chatCfg.FallbackText = cfg.ChatFallbackText
	}
	chatCfg.RetentionDays = cfg.ChatRetentionDays
	chatCfg.RateLimitPerMin = cfg.ChatRateLimitPerMin
	chatService := chat.NewService(chatCfg,
		&chat.IdentityAdapter{Service: service},
		retrievalService, inferenceService, chatStore)
	chatHandlers := &chat.Handlers{Service: chatService, Version: version.Version}

	// Module 8: observability. All instrumentation is read-only and
	// fail-safe: no business logic is modified, and monitoring failures
	// can neither crash the API nor bypass authorization.
	obsLog := observability.NewLogger("api", cfg.AppEnv, cfg.ObsLogLevel)
	obsReg := observability.NewRegistry()
	obsInst := observability.NewInstruments(obsReg)
	var otelExporter *observability.OTLPExporter
	if cfg.OtelEnabled {
		otelExporter = &observability.OTLPExporter{Endpoint: cfg.OtelEndpoint, Service: "api"}
	}
	var otelExp observability.Exporter
	if otelExporter != nil {
		otelExp = otelExporter
	}
	tracer := observability.NewTracer(cfg.OtelSampleRatio, otelExp)
	readyGauge := obsReg.RegisterGauge("platform_ready",
		"Platform readiness: 1 when mandatory dependencies are healthy.", observability.LabelService)
	obsChecks := []observability.Check{
		{
			Name: "db", Mandatory: true,
			Probe: func(ctx context.Context) error {
				pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
				return pool.Ping(pctx)
			},
		},
		{
			Name: "embeddings", Mandatory: false,
			Probe: func(ctx context.Context) error {
				pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
				return embedder.Health(pctx)
			},
		},
		{
			Name: "llm", Mandatory: false,
			Probe: func(ctx context.Context) error {
				pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
				return llmRuntime.Ready(pctx)
			},
		},
	}
	obsChecker := observability.NewChecker("api", obsChecks,
		2*time.Second, time.Duration(cfg.ObsHealthTimeoutS)*time.Second, readyGauge)
	obsHandlers := &observability.Handlers{
		Version: version.Version, Checker: obsChecker, Reg: obsReg,
		Token: cfg.InternalServiceToken,
	}
	observability.RegisterInferenceCollector(obsReg, func() observability.InferenceSnapshot {
		snap := inferenceService.Metrics().Snapshot()
		out := observability.InferenceSnapshot{Failures: map[string]int64{}}
		out.Requests, _ = snap["requests"].(int64)
		out.Success, _ = snap["success"].(int64)
		out.Overloaded, _ = snap["overloaded"].(int64)
		out.Timeouts, _ = snap["timeouts"].(int64)
		out.PromptTokens, _ = snap["promptTokens"].(int64)
		out.CompletionTokens, _ = snap["completionTokens"].(int64)
		out.ModelLoadErrors, _ = snap["modelLoadErrors"].(int64)
		if fm, ok := snap["failures"].(map[string]int64); ok {
			out.Failures = fm
		}
		return out
	})
	observability.RegisterIngestionCollector(obsReg, func(ctx context.Context) (map[string]int64, error) {
		rows, err := pool.Query(ctx, `select status, count(*) from public.ingestion_jobs group by status`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		counts := map[string]int64{}
		for rows.Next() {
			var status string
			var n int64
			if err := rows.Scan(&status, &n); err != nil {
				return nil, err
			}
			counts[status] = n
		}
		return counts, rows.Err()
	})
	// Durable audit sink. A failed startup event is logged, never fatal:
	// audit must not prevent the API from serving.
	auditSink := observability.Emitter(&observability.PostgresEmitter{Pool: pool})
	if err := auditSink.Emit(context.Background(), observability.Event{
		Service: "api", Name: observability.EventConfigChanged,
		Outcome: "ok", Detail: map[string]any{"version": version.Version},
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		obsLog.Warn(context.Background(), "audit.startup_emit_failed")
	}
	obsLog.Event(context.Background(), "api.start",
		slog.String("version", version.Version),
		slog.Bool("otel_enabled", cfg.OtelEnabled))
	verifier := auth.NewVerifier(auth.VerifierConfig{
		URL:       cfg.SupabaseURL,
		JWTSecret: cfg.SupabaseJWTSecret,
		Issuer:    cfg.SupabaseJWTIssuer,
		Audience:  cfg.SupabaseJWTAudience,
	})

	handler := router.New(router.Deps{
		Version:            version.Version,
		Auth:               verifier,
		Handlers:           handlers,
		Knowledge:          knowledgeHandlers,
		Processing:         processingHandlers,
		Retrieval:          retrievalHandlers,
		Inference:          inferenceHandlers,
		Chat:               chatHandlers,
		Ops:                obsHandlers,
		Obs:                &observability.Stack{Log: obsLog, Inst: obsInst, Tracer: tracer},
		CORSAllowedOrigins: strings.Split(cfg.CORSAllowedOrigins, ","),
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
	go func() {
		logger.Info("starting api", "addr", server.Addr, "env", cfg.AppEnv, "version", version.Version)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("api stopped", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down api")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("http shutdown failed", "error", err)
	}
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer drainCancel()
	if err := inferenceService.Shutdown(drainCtx); err != nil {
		logger.Error("inference drain failed", "error", err)
	}
	logger.Info("api stopped")
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
