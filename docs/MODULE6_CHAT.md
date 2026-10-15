# Module 6 — Chat Orchestration

One stable public API coordinating tenant resolution (Module 1), retrieval
(Module 4), local generation (Module 5), conversation persistence, verified
source attribution, and controlled fallbacks. No hosted AI calls anywhere.

## Routes

- `POST /api/v1/chat` — `{widgetId, sessionId?, message, idempotencyKey?}` →
  `{sessionId, status, answer, sources}` with Module 0 envelopes.
- `GET /api/v1/chat/sessions/{sessionId}?widgetId=...` — oldest-first turns.

Statuses: `answered` (generated from evidence), `insufficient_evidence`
(configured fallback, LLM never called, 200), `temporarily_unavailable`
(downstream failure, 200 with generic text; nothing fabricated is stored).
Errors use Module 0 envelopes: `WIDGET_NOT_FOUND` 404, `ORIGIN_NOT_ALLOWED`
403, `SESSION_NOT_FOUND` 404, `RATE_LIMITED` 429.

## How a turn flows

1. Validate widget/message sizes; enforce per-widget+IP rate limit (default
   30/min, in-memory fixed window).
2. Resolve the tenant server-side via Module 1 (`ResolveTenant` +
   `WidgetConfig` through `IdentityAdapter`; no Module 1 internals touched).
   Inactive/unknown widgets fail closed. Browser tenant IDs are never
   accepted — the request has no tenant field at all.
3. Enforce the widget origin allow-list when the browser sends `Origin`
   (CORS alone is not authorization; absence of `Origin` is allowed for
   non-browser callers).
4. Bind or create the session: `ses_` + 128-bit server random. Sessions are
   scoped to (organization, widget); cross-tenant presentation returns 404.
5. Idempotency: `(sessionId, idempotencyKey)` replays the stored reply
   without re-running retrieval/generation.
6. Persist the user turn, load bounded history (default last 10), retrieve
   with the trusted org ID, fall back on insufficient evidence, otherwise
   build the framed prompt and generate locally.
7. Persist the assistant turn (`ok` with verified sources, `fallback` with
   none). Generation failures persist nothing.

## Prompt policy

System instructions demand evidence-only, concise answers and forbid
invention. Evidence, history, and the question travel in explicit
`Untrusted …` blocks — retrieved content cannot override policy. History
interprets follow-ups; it is never treated as company knowledge. Only
narrow `<think>`-marker handling exists in Module 5; Module 6 deletes no
arbitrary text.

## Sources

Citations come strictly from Module 4 evidence (`documentId`, immutable
`versionId`, `chunkId`, rank, ≤300-char excerpt, page/section meta). The
model never supplies identifiers. Text and metadata stay separate fields;
render answer text as plain text, never generated HTML.

## Persistence & privacy

Persisted per turn: role, content, status (`ok`/`fallback`), sources,
idempotency key, timestamps — bound to conversation → (organization,
widget). Never stored: browser IPs, origins, prompts, embeddings, model
diagnostics. Logs carry sizes, codes, and correlation IDs only.

Retention (`CHAT_RETENTION_DAYS`, default 90): schedule the documented purge
in `0005_module6_chat.sql`. Organization deletion cascades conversations and
messages. Targeted erasure: `DELETE FROM chat_conversations WHERE id = $1
AND organization_id = $2` (messages cascade).

## Configuration

```
CHAT_MAX_MESSAGE_CHARS=2000 CHAT_HISTORY_LIMIT=10 CHAT_TOP_K=5
CHAT_MAX_PROMPT_CHARS=8000 CHAT_FALLBACK_TEXT= (default built-in)
CHAT_RETENTION_DAYS=90 CHAT_RATE_LIMIT_PER_MIN=30
```

## Tests

`go test ./internal/chat/ -v` — **21 tests pass** (mock Module 1/4/5
adapters, memory store): full workflow, session reuse + history in prompt,
invalid/inactive widgets, origin allow/deny/absent, cross-tenant session and
history denial, fallback without LLM call, generation/retrieval/DB failures
(no fabricated storage), message limits, idempotent retry (single
generation), rate limiting, retention purge, prompt framing/truncation,
source assembly, HTTP envelopes + correlation IDs, history endpoint.

`TestPostgresStoreLive` (skipped without `TEST_SUPABASE_DB_URL` +
`TEST_CHAT_ORG_ID`/`TEST_CHAT_WIDGET_ID` and migrations 0001+0005) covers
round-trip, sources JSON, idempotency replay, and cross-tenant denial
against real Postgres. No live Supabase project is configured here, so
integration tests are mock-only; the Postgres path is exercised solely
through unit-tested query construction until then.

## Module 7 integration

```js
const res = await fetch("/api/v1/chat", {method: "POST",
  headers: {"Content-Type": "application/json"},
  body: JSON.stringify({widgetId, sessionId, message})});
const {sessionId: sid, status, answer, sources} = (await res.json()).data;
// store sid (e.g. localStorage) and send it on follow-ups
// render answer as text; render sources from identifiers, never model text
// status === "insufficient_evidence" → show fallback styling
// 429 → back off; 403 → check embed origin; 404 → reset session
```

Safe retries: resend the same `sessionId` + `idempotencyKey`. Never send
`organizationId`. History: `GET /api/v1/chat/sessions/{sid}?widgetId=...`.

## Limitations

- Rate limiting is per-instance memory; put shared limiting in front for
  multi-replica deploys.
- No streaming; one response per turn.
- Fallback text is global (per-widget overrides are a future task).
- Retention purge needs an external schedule (cron); no background worker yet.
