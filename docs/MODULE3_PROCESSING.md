# Module 3 — Document Processing & Embeddings

Local pipeline turning one immutable Module 2 document version into normalized
text, deterministic chunks, locally generated 384-dim embeddings, and durable
`knowledge_chunks` rows for Module 4 retrieval. No ranking, chat, or LLM logic.

## What was implemented

- Migration `database/migrations/0003_module3_processing.sql`: `pgvector`
  extension, `ingestion_jobs` (queued/processing/completed/failed/cancelled,
  attempts, error codes, version ids, idempotency unique), `knowledge_chunks`
  (tenant/doc/version ids, chunk index, text, `vector(384)`, token count,
  `source_meta` page/section/offsets, `is_active`, unique per
  org/doc/version/index), HNSW cosine index for Module 4, RLS member-read +
  service_role-write.
- Go `services/api/internal/processing`: `model`, `extract` (txt/md/pdf/docx,
  OCR-required for scanned PDFs, untrusted-data handling), `chunk`
  (deterministic ~200/35 token-aware approximation, order + metadata,
  O(window) memory), `embed` (local HTTP worker client + stub, 384-dim finite
  validation, batch limits), `store`/`postgres_store` (staging-and-publish,
  idempotent stage, atomic publish, purge), `service` (orchestrator, job ops,
  worker pool, outbox relay), `module2_adapter` (sole Module 2 touchpoint),
  `http` handlers. Wired in `router`, `cmd/api/main.go`, `config`.
- Python `services/embeddings`: FastAPI worker (`/embed`, `/health`, `/ready`),
  `download_model.py`, `requirements.txt`, `Dockerfile`, README. Loopback-only.
- Contract `contracts/api/v1/processing.yaml`.

## Pipeline

`claim (queued→processing)` → fetch exact version bytes (Module 2 adapter) →
check active/deleted (inactive → purge + `DOCUMENT_INACTIVE`) → extract →
chunk → embed in ≤32-text batches → stage inactive → atomic publish
(new set active, older versions superseded) → report `ready` to Module 2.
Failures: staged rows cleaned, job `failed` with controlled code
(`EMPTY_CONTENT`, `MALFORMED_CONTENT`, `OCR_REQUIRED`, `EMBEDDING_FAILED`,
`STORAGE_FAILED`, `DATABASE_FAILED`, `DOCUMENT_INACTIVE`, ...), retry via
`POST .../retry` until `max_attempts` (default 5). `delete` jobs purge active
chunks and complete. Retries never duplicate active chunk sets.

## Module 2 integration (documented interfaces only)

- Content: `Module2Adapter.FetchVersion` (reads version bytes + lifecycle
  flags; never writes Module 2 tables).
- Status: `MarkProcessing` + `ReportResult` → Module 2's
  `ReportProcessingResult` (stale-version safe). No other Module 2 writes.
- Handoff: `RelayOnce` maps Module 2 outbox rows → jobs by the same
  idempotency key (duplicate-safe), marks `dispatched`, and marks
  `done`/`failed` on completion. Module 3 never invents Module 2 internals.

## Endpoints (Module 0 envelopes; jobs need JWT + org writer except reads)

| Method | Path | Notes |
|---|---|---|
| POST | `/api/v1/processing/jobs` | `{organizationId, documentId, versionId, operation, idempotencyKey?}` → 201 |
| GET | `/api/v1/processing/jobs/{id}` | status/attempts/errors → 200 |
| POST | `/api/v1/processing/jobs/{id}/retry` | failed only → 202 |
| POST | `/api/v1/processing/jobs/{id}/cancel` | queued only → 200 |
| GET | `/api/v1/processing/health` | public liveness → 200 |
| GET | `/api/v1/processing/ready` | public; 503 until worker ready |

## Setup

```bash
# 1. Model (one-time, needs network):
cd services/embeddings
pip install -r requirements.txt
python download_model.py            # -> ./models/all-MiniLM-L6-v2 (dim 384)

# 2. Worker (offline OK afterwards):
HF_HUB_OFFLINE=1 EMBEDDING_MODEL_DIR=./models/all-MiniLM-L6-v2 \
  uvicorn app:app --host 127.0.0.1 --port 8082

# 3. Env (never commit .env):
EMBEDDING_PROVIDER=http             # or 'stub' for dev without weights
EMBEDDING_WORKER_URL=http://127.0.0.1:8082
EMBEDDING_MODEL_DIR=./models/all-MiniLM-L6-v2
PROCESSING_WORKERS=2
CHUNK_TARGET_TOKENS=200
CHUNK_OVERLAP_TOKENS=35

# 4. Migrate 0001 -> 0002 -> 0003, then:
cd services/api
go run ./cmd/api
go test ./...
```

`docker compose -f infra/docker/docker-compose.yml up --build` runs api +
embeddings (CPU torch image; first start downloads the model into a volume).

## Tests

`go test ./internal/processing/ -v` — 29 tests, all passing 2026-10-15:

- Extraction: txt/md (sections, code fences kept)/pdf (multi-page + page
  meta)/docx; empty + malformed + unsupported; scanned PDF → `OCR_REQUIRED`;
  script-like content stays inert data.
- Chunking: bounded ≤200, 35-token overlap, order + section/page metadata,
  determinism, 20k-word doc (~121 chunks, bounded memory), no empty chunks.
- Embeddings: dim/NaN/Inf rejection; stub 384-dim unit-norm deterministic;
  worker success, bad-dim rejection, connection-refused, timeout, cancellation,
  model-failure errors.
- Pipeline: success; duplicate idempotency → same job; fail → fix → retry →
  completed with attempts=2; cancel; v2 supersedes v1 (old never exposed);
  tenant isolation + cross-tenant denial + viewer-denied; inactive/deleted →
  purge + `DOCUMENT_INACTIVE`; publish-failure → old set intact → retry wins;
  relay idempotent.
- HTTP: submit/get/retry-conflict/cancel/permissions/health/ready.

Require live Supabase + real weights (not run here, manual): migration order,
pgvector HNSW query, RLS behavior, end-to-end worker embeddings (run worker,
`EMBEDDING_PROVIDER=http`, submit job), offline restart with
`HF_HUB_OFFLINE=1`. `EMBEDDING_PROVIDER=stub` covers all CI tests offline.

## Module 4 interface

```sql
select id, organization_id, document_id, document_version_id, chunk_index,
       content_text, embedding, token_count, source_meta
from public.knowledge_chunks
where organization_id = $1 and is_active = true
order by embedding <=> $2::extensions.vector
limit $3;
```

Only `is_active` rows are searchable; rows are tenant- and version-scoped with
page/section provenance in `source_meta`. Never combine tenants/versions.

## Limitations

- Tokenizer is a word-count approximation (MiniLM WordPiece parity needs the
  vocab file); chunk sizes validated by word-count tests.
- No OCR: scanned PDFs fail explicitly with `OCR_REQUIRED`.
- PDF extraction covers standard text-showing operators; exotic encodings may
  under-extract (reported as empty/OCR rather than silently indexed).
- Worker pool is in-process; multi-instance deployments need an external queue
  (outbox relay is the seam).
- Stub embedder is deterministic hashing, not semantic — dev/tests only.
