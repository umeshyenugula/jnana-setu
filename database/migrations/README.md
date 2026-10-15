# API Conventions

## Base Path
All endpoints are versioned:
```
/api/v1
```

Example:
```
GET /api/v1/health
```

## Content Type
Requests and responses use `application/json; charset=utf-8` unless a later module documents a binary stream.

## Success Envelope
```json
{
  "success": true,
  "data": {
    "status": "ok"
  },
  "meta": {
    "correlationId": "0123456789abcdef0123456789abcdef",
    "timestamp": "2026-10-15T00:00:00Z",
    "version": "v1",
    "path": "/api/v1/health"
  }
}
```

## Error Envelope
```json
{
  "success": false,
  "error": {
    "code": "NOT_FOUND",
    "message": "The requested resource was not found."
  },
  "meta": {
    "correlationId": "0123456789abcdef0123456789abcdef",
    "timestamp": "2026-10-15T00:00:00Z",
    "version": "v1",
    "path": "/missing"
  }
}
```

## Correlation ID
- Clients may send `X-Correlation-ID`.
- The API generates one when it is absent.
- The API returns it in `X-Correlation-ID` and `meta.correlationId`.

## Error Codes
- `INVALID_REQUEST`: malformed or invalid client input.
- `UNAUTHORIZED`: authentication is required or invalid.
- `FORBIDDEN`: authenticated caller lacks permission.
- `NOT_FOUND`: resource does not exist.
- `METHOD_NOT_ALLOWED`: HTTP method is unsupported.
- `CONFLICT`: state conflict.
- `INTERNAL_ERROR`: unexpected server failure.

## HTTP Status Mapping
- 200: successful read/action.
- 201: resource created.
- 400: invalid request.
- 401: unauthorized.
- 403: forbidden.
- 404: not found.
- 405: method not allowed.
- 409: conflict.
- 500: internal error.

## Versioning
- Breaking changes require a new version and path prefix.
- Additive fields are preferred within `v1`.
- Clients should ignore unknown fields.
```

Docs final.<|end|><|start|>start<|channel|>write_file <|constrain|>xml<|message|><rfl:parameter name="content">
# Architecture And Ownership

## Directory Ownership
- `apps/`: frontend applications and widget packages.
- `services/`: backend service implementations.
- `contracts/`: public API contracts shared across applications and services.
- `database/`: migrations and database-owned artifacts.
- `infra/`: Docker and local development infrastructure.
- `tests/`: repository-level tests that cross service boundaries.
- `docs/`: conventions and project context.

## Service Boundaries
- Services do not import another service's private implementation packages.
- Database access is owned by the API or an explicitly documented service.
- Shared contracts live under `contracts/`, not inside a service.

## Frontend Boundaries
- React owns presentation and browser-side interaction.
- Frontend code never stores privileged Supabase credentials.
- Frontend code never calls hosted AI inference or embedding APIs.

## Backend Boundaries
- Go owns application backend APIs.
- The API service validates input and coordinates infrastructure concerns.
- Local LLM and embedding runtimes are separate infrastructure concerns configured at runtime.

## Modularity
- Every service must expose a documented public interface.
- Avoid shared implementation packages unless their purpose is documented.
```

Need architecture doc maybe later. README.<|end|><|start|>start<|channel|>write_file <|constrain|>xml<|message|><rfl:parameter name="content">
# AI Knowledge Assistant

Modular, self-hosted AI Knowledge Assistant for client websites.

## Prerequisites
- Go 1.26 or newer
- Node.js 20 or newer
- npm
- Docker and Docker Compose (optional)

## Repository Structure
See `PROJECT_CONTEXT.md` and `docs/`.

## Setup
1. Copy `.env.example` to `.env` and fill only values needed for local development.
2. Install frontend dependencies:
   - `cd apps/admin-dashboard && npm install`
   - `cd apps/chat-widget && npm install`

## Development
Run the Go API:
```
cd services/api
go run ./cmd/api
```
Default URL: `http://localhost:8080`

Run frontends:
```
cd apps/admin-dashboard
npm run dev
```
```
cd apps/chat-widget
npm run dev
```

## Tests
Go API:
```
cd services/api
go test ./...
```

Frontend type-check/build:
```
cd apps/admin-dashboard
npm run build
```
```
cd apps/chat-widget
npm run build
```

## Docker
```
docker compose -f infra/docker/docker-compose.yml config
docker compose -f infra/docker/docker-compose.yml up --build
```

## Conventions
- API base path: `/api/v1`
- Standard response envelopes: `docs/API_CONVENTIONS.md`
- No secrets are committed.
- Hosted AI APIs are prohibited.

## Directory Ownership
- `apps/`: frontend React applications.
- `services/`: backend Go services.
- `contracts/`: shared API contracts.
- `database/`: migrations.
- `infra/`: Docker development skeleton.
- `tests/`: repository-level tests.
- `docs/`: conventions and architecture.
```

OpenAPI.<|end|><|start|>start<|channel|>write_file <|constrain|>xml<|message|><rfl:parameter name="content">
openapi: 3.1.0
info:
  title: AI Knowledge Assistant API
  version: 0.1.0
servers:
  - url: http://localhost:8080
paths:
  /api/v1/health:
    get:
      summary: API liveness check
      responses:
        "200":
          description: API is running
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/SuccessResponse"
components:
  schemas:
    Meta:
      type: object
      properties:
        correlationId:
          type: string
        timestamp:
          type: string
          format: date-time
        version:
          type: string
        path:
          type: string
    HealthData:
      type: object
      properties:
        status:
          type: string
        service:
          type: string
        version:
          type: string
    SuccessResponse:
      type: object
      properties:
        success:
          type: boolean
        data:
          $ref: "#/components/schemas/HealthData"
        meta:
          $ref: "#/components/schemas/Meta"
    Error:
      type: object
      properties:
        code:
          type: string
        message:
          type: string
    ErrorResponse:
      type: object
      properties:
        success:
          type: boolean
        error:
          $ref: "#/components/schemas/Error"
        meta:
          $ref: "#/components/schemas/Meta"
```

Migrations README.<|end|><|start|>start<|channel|>write_file <|constrain|>xml<|message|><rfl:parameter name="content">
# Database Migrations

This directory owns Supabase PostgreSQL migrations. Business tables are not created in the foundation task;_MODULE 1 (or later data modules) will add versioned SQL migrations here.

Conventions:
- Use sequential numeric prefixes: `0001_initial.sql`.
- Include pgvector extension setup when the first vector table is introduced.
- Do not store credentials in migration files.
