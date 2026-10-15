# AI Knowledge Assistant — Project Context

## Fixed Stack
- Frontend: React + Vite + TypeScript.
- Backend/API: Go.
- Database: Supabase PostgreSQL + pgvector.
- Local LLM: Qwen3-4B, GGUF Q4_K_M.
- LLM runtime: llama.cpp.
- Embedding model: all-MiniLM-L6-v2, executed locally.
- Packaging: Docker + Docker Compose.
- Hosted AI inference and embedding APIs: prohibited.

## Module Responsibilities
- `apps/admin-dashboard`: browser-based administration UI. Owns presentation and browser-side interaction only.
- `apps/chat-widget`: customer-facing chat widget UI. Owns presentation and browser-side interaction only.
- `services/api`: Go API boundary. Owns HTTP contracts, request validation, database access coordination, and integration with self-hosted AI runtime.
- `contracts/api/v1`: shared, versioned API contracts and response conventions.
- `database/migrations`: Supabase PostgreSQL migrations. Database access and schema changes respect module ownership.
- `infra/docker`: local development and packaging skeleton.
- `tests`: repository-level integration and smoke tests.

## Dependency Restrictions
- Do not call hosted AI inference or embedding APIs.
- Supabase is the managed database provider; the API service may use Supabase PostgreSQL and pgvector.
- Privileged Supabase credentials must remain server-side.
- Frontend code may use only public browser-safe configuration.
- Each service's private implementation packages are not imported by other services.

## Security Principles
- Secrets are provided through environment variables and never committed.
- Service-role credentials never appear in frontend bundles.
- Correlation IDs are generated or accepted for every request.
- Standard success/error envelopes are used for predictable client behavior.
- Inputs are validated at API boundaries before business logic is added.
- Logs must not include credentials or raw private data.

## Token-Budget Rules
- Keep each implementation task scoped to one module or contract.
- Avoid scanning unrelated files when conventions are already documented.
- Prefer targeted reads and targeted tests.
- Split work before expanding scope beyond the documented contract.
- Do not implement future-module business logic in foundation tasks.
