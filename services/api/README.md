# API Service

Go API service.

- Entry point: `cmd/api/main.go`
- HTTP conventions: `internal/api`
- Configuration: `internal/config`
- Version: `internal/version`

Supported endpoints:
- `GET /api/v1/health`
- Module 1: `/api/v1/me`, `/api/v1/organizations`, `.../members`, `.../widgets`, `/api/v1/widget-config/{publicId}`
- Module 2 knowledge (see `docs/MODULE2_KNOWLEDGE.md` and `contracts/api/v1/knowledge.yaml`):
  - `POST /api/v1/organizations/{org}/knowledge/text`
  - `POST /api/v1/organizations/{org}/knowledge/files`
  - `GET /api/v1/organizations/{org}/knowledge`
  - `GET /api/v1/organizations/{org}/knowledge/{doc}`
  - `GET /api/v1/organizations/{org}/knowledge/{doc}/versions`
  - `POST /api/v1/organizations/{org}/knowledge/{doc}/versions`
  - `PATCH /api/v1/organizations/{org}/knowledge/{doc}`
  - `POST /api/v1/organizations/{org}/knowledge/{doc}/reprocess`
  - `GET /api/v1/organizations/{org}/knowledge/{doc}/status`
  - `DELETE /api/v1/organizations/{org}/knowledge/{doc}`
- Module 3 processing (see `docs/MODULE3_PROCESSING.md` and `contracts/api/v1/processing.yaml`):
  - `POST /api/v1/processing/jobs`
  - `GET /api/v1/processing/jobs/{id}`
  - `POST /api/v1/processing/jobs/{id}/retry`
  - `POST /api/v1/processing/jobs/{id}/cancel`
  - `GET /api/v1/processing/health`
  - `GET /api/v1/processing/ready`
- Local embeddings worker: `services/embeddings` (`POST /embed`, `GET /health`, `GET /ready` on loopback only).
- Module 4 retrieval, internal only (see `docs/MODULE4_RETRIEVAL.md` and `contracts/api/v1/retrieval.yaml`):
  - `POST /api/v1/internal/retrieval/query` (header `X-Internal-Service-Token`, Module 6 only)
- Module 5 inference, internal only (see `docs/MODULE5_INFERENCE.md` and `contracts/api/v1/inference.yaml`):
  - `POST /api/v1/internal/inference/generate` (header `X-Internal-Service-Token`, Module 6 only)
  - `GET /api/v1/internal/inference/health`, `GET /api/v1/internal/inference/ready` (no auth)
- Module 6 chat, public widget API (see `docs/MODULE6_CHAT.md` and `contracts/api/v1/chat.yaml`):
  - `POST /api/v1/chat`
  - `GET /api/v1/chat/sessions/{sessionId}?widgetId=...`
- Module 8 operations (see `docs/MODULE8_OBSERVABILITY.md` and `contracts/api/v1/ops.yaml`):
  - `GET /api/v1/ready` (public, minimal)
  - `GET /api/v1/metrics` (header `Authorization: Bearer <INTERNAL_SERVICE_TOKEN>`)

Env:
```
# EMBEDDING_PROVIDER=stub for dependency-free dev/tests without model weights.
EMBEDDING_PROVIDER=http EMBEDDING_WORKER_URL=http://127.0.0.1:8082 PROCESSING_WORKERS=2
# Required for retrieval; empty fails closed. Tune threshold per deployment.
INTERNAL_SERVICE_TOKEN=<set-a-secret> RETRIEVAL_TOP_K=5 RETRIEVAL_MIN_SIMILARITY=0.1
```

Run:
```
go run ./cmd/api
```
Test:
```
go test ./...
```
Build:
```
go build ./...
```
