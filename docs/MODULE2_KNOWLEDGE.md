# Module 2 — Knowledge Management

Secure, independently testable knowledge service: clients create knowledge from
text or uploaded files (`.txt`, `.md`, `.pdf`, `.docx`), organize versions,
publish/deactivate/delete, and hand immutable versions to Module 3 for
indexing. Extraction, chunking, embeddings, vector search, LLM inference and
chat are explicitly out of scope.

## What Was Implemented

- Migration `database/migrations/0002_module2_knowledge.sql`: `knowledge_documents`,
  `knowledge_document_versions`, `knowledge_processing_outbox`, indexes, RLS
  (member read-only, writes via Go `service_role`), private bucket
  `knowledge-originals` with service_role-only storage policy.
- Version-storage strategy: original bytes of EVERY version (files and text-as-`.txt`)
  retained verbatim in the private bucket under server-generated paths
  `org/<org>/doc/<doc>/v<N>/<rand><ext>`; Postgres holds metadata + `storage_path`
  only. Client filenames kept as metadata, never as paths.
- Go package `services/api/internal/knowledge`: `model`, `validation`,
  `service` (authz + lifecycle), `store` interface, `memory_store`,
  `postgres_store` (transactional outbox), `storage` (memory + Supabase REST via
  service-role key), `http` handlers. Wired in `internal/router` and `cmd/api/main.go`
  with 30s server timeouts and bounded upload readers.
- Contract `contracts/api/v1/knowledge.yaml`.

## Lifecycle & Ownership

- Module 2 owns: `saved/uploaded`, `pending` (awaiting processing), `is_active`
  flag, soft delete (`deleted_at`). Only active, non-deleted documents are
  eligible for retrieval; `PATCH isActive=false` resets status to `pending` so a
  deactivated document can never be published as ready knowledge.
- Module 3 owns reporting `processing` → `ready` / `failed` via
  `ReportProcessingResult` (stale-version results are acknowledged without
  clobbering a newer pending version). Module 2 never writes chunks/embeddings;
  Module 3 never writes document tables.

## Module 3 Handoff Contract

Each mutation commits a `knowledge_processing_outbox` row atomically with the
document/version change. Relay: `SELECT ... WHERE status='pending' ORDER BY
created_at`, deliver, then `MarkOutbox(status, attempts, last_error)`.

```json
{
  "id": "outbox uuid",
  "organizationId": "uuid",
  "documentId": "uuid",
  "versionId": "immutable version uuid",
  "operation": "index | reindex | delete",
  "idempotencyKey": "client key or server-generated",
  "status": "pending | dispatched | done | failed"
}
```

Module 3 fetches the exact version bytes server-side:
`GET <SUPABASE_URL>/storage/v1/object/authenticated/knowledge-originals/<storage_path>`
with the service-role key, or via a future authorized content-fetch endpoint.
`operation=delete` means purge chunks/embeddings for the document; originals are
already removed by Module 2.

## Authorization

Every operation verifies JWT identity, then Module 1 membership for the target
organization. Unknown/foreign org IDs return `404` (no existence leak).
Any member may read; only `owner`/`admin` may create, version, publish,
deactivate, reprocess, or delete (`member`/`viewer` are read-only). Suspended
orgs are rejected. CORS is not treated as security.

## Endpoints

| Method | Path | Notes |
|---|---|---|
| POST | `/api/v1/organizations/{org}/knowledge/text` | JSON `{title, contentText, idempotencyKey?}` (+ `Idempotency-Key` header) |
| POST | `/api/v1/organizations/{org}/knowledge/files` | multipart `file` + `title?`; ≤10 MiB, magic-byte checked |
| GET | `/api/v1/organizations/{org}/knowledge` | `?includeInactive=&limit=&offset=` |
| GET | `/api/v1/organizations/{org}/knowledge/{doc}` | document + current version metadata |
| GET | `/api/v1/organizations/{org}/knowledge/{doc}/versions` | ascending version history |
| POST | `/api/v1/organizations/{org}/knowledge/{doc}/versions` | multipart file OR JSON `{contentText}`; monotonic `version_number` |
| PATCH | `/api/v1/organizations/{org}/knowledge/{doc}` | `{title?, isActive?}` |
| POST | `/api/v1/organizations/{org}/knowledge/{doc}/reprocess` | retry-safe; same key returns existing, no new version; 202 |
| GET | `/api/v1/organizations/{org}/knowledge/{doc}/status` | `{documentId, isActive, processingStatus, currentVersionId, deleted}` |
| DELETE | `/api/v1/organizations/{org}/knowledge/{doc}` | soft delete + storage removal + `delete` outbox op |

All responses use Module 0 envelopes with `X-Correlation-ID`.

## Setup

```bash
# 1. Configure env (never commit .env):
cp .env.example .env   # set SUPABASE_URL, SUPABASE_SERVICE_ROLE_KEY, SUPABASE_DB_URL, SUPABASE_JWT_SECRET

# 2. Apply migrations in order against your Supabase dev project:
#    0001_module1_identity_tenant.sql, then 0002_module2_knowledge.sql
#    (Supabase SQL editor or psql $SUPABASE_DB_URL -f <file>)

# 3. Run / test:
cd services/api
go run ./cmd/api
go test ./...
go vet ./...
```

`SUPABASE_DB_URL`, `SUPABASE_URL`, and `SUPABASE_SERVICE_ROLE_KEY` are
required: without them the API refuses to boot (fail fast, no silent
memory-store mode). Postgres + Supabase Storage (service-role, server-side
only) are always used.

## Tests

`go test ./internal/knowledge/ -v` — 22 tests, all passing 2026-10-15:

- text submission; supported-file upload (txt/md/pdf/docx); unsupported type;
  oversized + empty; malformed PDF/DOCX + content-type mismatch; invalid text.
- duplicate `Idempotency-Key` → 409; reprocess retry returns same outbox row,
  version count unchanged; monotonic version history + current-version pointer.
- activation/deactivation filtering; delete removes objects + enqueues `delete`
  op; role checks (viewer write → 403, admin write ok); cross-tenant denial;
  unauthenticated → 401; unknown org → 404; injected DB/storage failures → 5xx;
  `ready` transition, stale-version no-clobber, invalid transition → 409.
- HTTP-level: full text lifecycle, multipart validation envelopes, permission
  isolation, server-generated storage paths (no `..`, org-scoped).

Tests needing a live Supabase project (not run here, documented): migration
apply order, RLS denial of direct anon/authenticated writes, bucket privacy
(no anon object access), and end-to-end outbox relay. They require
`SUPABASE_DB_URL` + service-role key and are manual until testcontainers are added.

## Limitations

- PDF/DOCX text extraction is intentionally absent (Module 3's pipeline).
- No full-text search inside Module 2; listing is by `created_at` with
  limit/offset only.
- Outbox relay ships as a background loop: the API runs `StartRelay` on boot
  (immediate catch-up pass, then every `PROCESSING_RELAY_INTERVAL_S`,
  default 10s), turning pending rows into ingestion jobs with no manual
  submission.
- `KNOWLEDGE_MAX_UPLOAD_MB` is wired via `knowledge.ConfigureLimits` at
  startup (1..100 MiB accepted, 10 MiB default).
