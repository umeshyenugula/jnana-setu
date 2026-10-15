# Module 8 — Observability & Platform Operations

Makes service health, errors, latency, resource use, ingestion status,
retrieval behavior, and local inference availability measurable and
diagnosable — without touching other modules' business logic or data.

## What was implemented

- `services/api/internal/observability/`: `logging` (slog JSON schema +
  redaction + correlation/trace context helpers), `metrics` (dependency-free
  Prometheus counters/histograms/gauges with bounded labels), `tracing`
  (W3C traceparent, ratio sampler, sanitized spans, OTLP/HTTP-JSON exporter),
  `health` (mandatory/optional dependency checker + `/ready` and token-gated
  `/metrics` handlers), `audit` (event schema, validation, log + Postgres
  sinks), `middleware` (request logs, HTTP metrics, server spans),
  `collectors` (inference-snapshot bridge, ingestion job-count snapshot).
- Migration `0006_module8_audit.sql`: `operational_events` table, indexes,
  RLS enabled, service-role-only access, retention guidance.
- Wiring in `cmd/api/main.go` (logger, registry, tracer, checker, collectors,
  audit sink, middleware) and `internal/router` (`/api/v1/ready`,
  `/api/v1/metrics`; existing `/api/v1/health` untouched).
- Monitoring stack: `infra/monitoring/` (prometheus.yml, alerts.yml, Grafana
  dashboard + provisioning, OTLP collector config) and a `monitoring`
  Compose profile (Prometheus, Grafana, collector; loopback ports only).
- Contract `contracts/api/v1/ops.yaml`.

## Logging conventions

Every line: `timestamp, severity, service, env, event, correlation_id`,
plus `trace_id`/`span_id` when tracing, `code` on errors, `duration_ms` +
`status` on completions. Levels via `OBS_LOG_LEVEL` (debug/info/warn/error;
production default `info`, never `debug` with traffic).

Never logged by default: secrets, tokens, passwords, prompts, customer
messages, retrieved documents, embeddings, session IDs. Field names
containing `password|secret|token|authorization|api_key|prompt|message|
content|document|answer|question|query|embedding|chunks|session` are replaced
with `[redacted]`; other string values truncate at 512 chars. Error
responses carry stable codes, never stack traces.

## Health endpoints

- `GET /api/v1/health` — liveness, unchanged, cheap, public.
- `GET /api/v1/ready` — 200 `ready`, 200 `degraded` (optional dep down),
  503 `not_ready` (mandatory dep down). Payload: names, booleans, latencies
  only. Checks run concurrently with 2s per-check and 5s overall timeouts,
  so one hung dependency cannot hang the endpoint.
- Dependency policy: **mandatory** = Supabase Postgres; **optional** =
  embedding worker, llama.cpp runtime (cold starts and model reloads are
  normal; alerts treat `degraded` softly).
- `GET /api/v1/metrics` — Prometheus text, gated by
  `Authorization: Bearer $INTERNAL_SERVICE_TOKEN` (fail-closed when unset).

## Metrics

Core series (see docs for full catalog): `http_requests_total{method,route,
status}`, `http_request_duration_seconds{…}` (5ms–30s buckets), `platform_
ready{service}`, `llm_requests_total/llm_overloaded_total/llm_timeouts_total/
llm_failures_total{code}/llm_model_load_errors_total`, `ingestion_jobs
{status}` (best-effort DB snapshot per scrape).

Label discipline: method/status from fixed sets, route from UUID/numeric/
`ses_`/`wdg_` normalization, everything else allowlisted with `other`
overflow. Session/user/tenant IDs and message contents are never labels.
Interpretation: 5xx share per route, p95 latency (cold-start aware),
`platform_ready==0` pages, `increase(llm_model_load_errors_total[10m])`
tickets, `increase(ingestion_jobs{status="failed"}[1h])` tickets.

## Tracing

W3C `traceparent` is honored inbound and propagated by helpers for outbound
worker calls. The middleware opens a sampled `http.server` span per request;
trace/span IDs flow into log lines. Sampling via `OTEL_SAMPLE_RATIO`
(default 0.1). Export is off by default (`OTEL_ENABLED=false` keeps spans
local-only); enabling posts OTLP/HTTP+JSON to `OTEL_ENDPOINT` (self-hosted
collector, never a hosted vendor). Span attributes are sanitized (no
prompts, text, embeddings, tokens) and capped at 256 chars. Export failures
are swallowed — monitoring never breaks requests.

Workflow coverage today: every HTTP request (chat, retrieval, inference,
admin). DB-search, embedding-worker, and generation child spans are
documented adoption points: wrap the call with `tracer.Start(ctx, name,
attrs)` and `span.End("ok"/"error", code)` — one line each at the call site,
owned by the respective module.

## Audit and operational events

Schema: service, name (`document.*`, `ingestion.failed_final`,
`widget.changed`, `membership.changed`, `auth.repeated_failures`,
`model.unavailable`, `config.changed`), correlation/organization/actor,
outcome, identifier-only detail (≤4KiB, blocked keys enforced), timestamp.
Sinks: structured-log (default, survives DB outages) and Postgres
`operational_events` (used when a pool exists). Only server-side code
constructs events — no HTTP handler accepts event payloads. Retention: 180
days via scheduled delete (see migration); org deletion nulls the reference.

## Enabling the monitoring profile

```bash
printf '%s' "$INTERNAL_SERVICE_TOKEN" > infra/monitoring/api_token  # never commit
docker compose -f infra/docker/docker-compose.yml --profile monitoring up -d
# Prometheus http://localhost:9090  Grafana http://localhost:3000 (admin/admin, change me)
# OTLP collector: OTEL_ENABLED=1 OTEL_ENDPOINT=http://localhost:4318
```

The API is fully usable without the profile. Prometheus absence changes
nothing; disabled OTLP keeps spans local; scrape failures only skip that
scrape. Grafana admin password via `GRAFANA_ADMIN_PASSWORD` (do not keep
the default in production).

## Tests

`go test ./internal/observability/ -v` — **29 tests pass**: log schema and
redaction, correlation/trace context, route/label bounding and cardinality,
exposition shape, traceparent round-trip/rejection, span sanitization,
sampling/export, OTLP payload shape, checker ready/degraded/not-ready,
timeout bounds, gauge + handler statuses, metrics auth + fail-closed,
event validation, emitter validation, collector deltas and failure skips,
middleware metrics without session-ID leakage. Monitoring-config YAML is
parsed and cross-checked in `TestMonitoringConfigs`.

Integration requiring live systems (documented, not run here): Supabase for
`PostgresEmitter`/ingestion snapshot (gated pattern: `TEST_SUPABASE_DB_URL`,
as in Modules 4/6), a collector for OTLP delivery, the Compose monitoring
profile for end-to-end scrape/alert evaluation (`promtool check rules` if
available).

## Security and privacy

- `/metrics` token-gated and fail-closed; `/ready` minimal by design.
- No tokens, keys, prompts, documents, or conversation history in logs,
  metrics, traces, or audit details — enforced by validators and covered by
  tests.
- Monitoring outages degrade silently (skipped snapshots, swallowed export
  errors) and can never bypass authorization or crash request handling.
- Audit table is service-role only; dashboards sit behind Grafana auth on
  loopback ports.

## Integration instructions for other modules

- Emit owned audit events via `observability.Emitter` (log sink default):
  `emitter.Emit(ctx, observability.Event{Service:"api", Name:
  observability.EventDocumentPublished, ...})`. Adoption sites are marked
  `// TODO(module8)` nowhere — instead see the table in this doc's appendix:
  knowledge publish/deactivate/delete, processing final ingestion failure,
  identity membership changes, inference model-unavailable transitions.
- Add workflow child spans with `tracer.Start`/`span.End` at retrieval,
  embedding-worker, DB-search, and generation call sites (attributes:
  bounded counts and codes only).
- Keep passing `ctx` (correlation IDs flow automatically); never add
  tenant/user/message labels to new metrics — extend the allowlists in
  `metrics.go` instead.

## Known limitations

- Retrieval empty-result rate and chat fallback rate have instruments
  defined but await one-line adoption hooks in Modules 4/6 (documented
  above); HTTP-level 4xx/5xx per route covers failure visibility today.
- OTLP exporter speaks HTTP+JSON only (no gRPC); sufficient for the
  self-hosted collector profile.
- In-memory metric state resets on restart; Prometheus `rate()`/`increase()`
  bridge restarts, but absolute counters are scrape-derived.
- No log aggregation shipper is configured; JSON logs go to stdout for the
  platform's log collector.
