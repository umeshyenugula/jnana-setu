# Module 4 — Retrieval Engine

Secure, independently testable retrieval over Module 3's indexed chunks. Takes
a customer question + trusted tenant context, embeds locally, cosine-searches
eligible chunks, ranks evidence for Module 6. **Never generates answers.**

## What was implemented

- `services/api/internal/retrieval/`: `model` (Query/Evidence/Result contract),
  `embed` (reuses Module 3's `processing.Embedder` interface — no copy — same
  model/preprocessing, 384-dim finite validation, 30s timeout, no cache, no
  hosted fallback), `repository` (read-only `ChunkRepository` + memory impl),
  `postgres_repo` (calls read-only `match_knowledge_chunks`, parameterized,
  tenant-filtered, no vectors returned), `rank` (ordering, top-K, threshold,
  dedupe, token budget, stable ties, meta validation), `service`
  (orchestration), `http` (private endpoint, service-token auth).
- Migration `0004_module4_retrieval.sql`: read-only SQL function only — no
  table changes. Eligibility enforced inside the function: chunk active AND
  document active AND not deleted AND chunk version == current version. This
  excludes inactive, deleted, stale, staged/unpublished, and incompletely
  indexed content (document flags cover the pre-purge window). Execute granted
  to `service_role` only. No smaller addition was possible: 0002+0003 already
  exposed every needed column.
- Contract `contracts/api/v1/retrieval.yaml`. Wiring in `router`, `main`,
  `config`. Module 3 indexing ownership unchanged (zero modifications).

## Endpoint (private, Module 6 only)

`POST /api/v1/internal/retrieval/query` — header
`X-Internal-Service-Token: <INTERNAL_SERVICE_TOKEN>`, body
`{organizationId, query, topK?, documentIds?}`. Rejects missing/wrong token
(401, fail-closed when unconfigured), rejects browser JWTs (no bearer path).
`organizationId` must be Module 6's server-side tenant resolution (Module 1
widget mapping); it is re-validated as UUID and bound into every DB call.

Response: `{status: results|insufficient_evidence, evidence: [{rank, chunkId,
documentId, versionId, text, similarity, distance, tokenCount, sourceMeta}],
topK, returned, minSimilarity, model, dimensions}` + Module 0 meta
(correlation ID). Scores: `distance` = pgvector cosine distance [0,2]
(lower = better); `similarity` = 1 − distance. Similarity is NOT a
correctness probability.

## Configuration

```
INTERNAL_SERVICE_TOKEN=            # required; empty rejects all retrieval calls
RETRIEVAL_TOP_K=5                  # default evidence count
RETRIEVAL_MAX_TOP_K=20             # hard cap
RETRIEVAL_MIN_SIMILARITY=0.1       # relevance floor; calibrate, do not treat as universal
RETRIEVAL_MAX_EVIDENCE_TOKENS=4000 # total budget protection
RETRIEVAL_EMBED_TIMEOUT_S=30
```

Embedder is always the real Module 3 local HTTP worker (`EMBEDDING_WORKER_URL`); no stub path exists in production.

## Calibrating `RETRIEVAL_MIN_SIMILARITY`

1. Collect 30–50 real client questions with the expected source document each.
2. Run them through `POST .../query` with `RETRIEVAL_MIN_SIMILARITY=-1` and log
   top-1 similarity per question, split by relevant vs irrelevant outcomes.
3. Pick the threshold maximizing F1 on that set; redeploy via env. Re-check
   after each knowledge-base refresh. The shipped default (0.1) suits the
   word-overlap test path; real MiniLM vectors separate better — expect a
   higher calibrated value.

## Evaluation results (actual, 2026-10-15)

`go test ./internal/retrieval/ -v` — **31 tests pass**. Test set
(`eval_test.go`): 5 answerable (direct ×2, paraphrase, terminology,
overlapping) + 2 absent-knowledge cases.

- `eval: n=7 answerable=5 recall@3=1.000 precision@3=1.000 mrr=1.000`
- Absent-knowledge questions return explicit `insufficient_evidence` (HTTP 200).
- Coverage: ordering/stable ties, threshold, dedupe, budget, NaN/empty guards,
  meta validation, tenant isolation, stale/inactive/deleted filtering, doc
  filters, query validation, top-K, empty index, worker failure/timeout/invalid
  vector, DB failure, auth (missing/wrong/unconfigured/Bearer-rejected).

Integration test needing live Supabase (`TestPostgresRepositoryLive`,
`TEST_SUPABASE_DB_URL`, migrations 0001–0004 + indexed chunks) is documented
and skipped in CI. End-to-end relevance with real MiniLM weights requires the
Module 3 worker running and is manual until then.

## Module 6 integration

```python
POST /api/v1/internal/retrieval/query
X-Internal-Service-Token: $INTERNAL_SERVICE_TOKEN
{"organizationId": "<resolved server-side>", "query": "<customer question>", "topK": 5}
```

1. Resolve tenant server-side from the widget/public ID + origin (Module 1);
   never forward a browser-supplied tenant ID. 2. On `status: results`, feed
   `evidence[].text` (+ doc/version ids for citations) to Module 5. 3. On
   `insufficient_evidence`, reply with a graceful fallback — never fabricate.
   Treat `text` as untrusted data. Map error codes: 400 caller bug, 401/403
   credential/tenant misconfiguration, 503 worker/DB outage (retry/backoff).

## Limitations

- Cosine-only ranking; no rerankers/hybrid (needs separate approval).
- Word-overlap test semantics understate real MiniLM paraphrase quality.
- No per-query caching (unjustified: no tenant-specific preprocessing).
- `documentIds` is the only filter; tag/date filters need a Module 2 model change.
