# Module 7 — React Dashboard & Website Chat Widget

User-facing frontend for JñānaSetu: an authenticated admin dashboard and an
embeddable anonymous chat widget. Both are independently buildable Vite +
React + TypeScript apps calling only documented backend APIs. No Go,
migrations, extraction, retrieval, or inference code lives here.

## Applications

- `apps/admin-dashboard` (`:5173`) — sign-in via Supabase Auth (GoTrue REST
  with the anon key; access token as `Bearer` on protected Go endpoints),
  organizations, knowledge upload/list/detail/versions/lifecycle, widget
  settings with origin allow-list and copyable embed snippet, team view,
  Module 6 test chat. Hash routing, responsive layout, loading/empty/error
  states, aria-live regions and labelled controls throughout.
- `apps/chat-widget` (`:5174`) — floating launcher, open/close, history,
  input with keyboard submit, loading/retry/outage states,
  insufficient-evidence notice, verified source links, session reuse via
  sessionStorage, mobile layout. Answers render as plain text; only `http(s)`
  links from verified metadata become anchors (`rel="noopener noreferrer"`).
  Calls only `POST /api/v1/chat`, `GET /api/v1/chat/sessions/*`, and the
  public `GET /api/v1/widget-config/*`.
- `apps/chat-widget/public/loader.js` — dependency-free embed snippet handler:
  validates `data-widget-id`, requires `data-api-base` (the API origin the
  iframe calls; never defaults to the static host), builds a sandboxed
  iframe, relays state with exact-origin `postMessage` both ways, targets
  the exact widget origin (never `"*"`), accepts no host commands beyond
  open/close/toggle.

## API contracts consumed

Module 1 (`/api/v1/me`, organizations, members, widgets, `widget-config`),
Module 2 (knowledge text/files/list/get/versions/patch/reprocess/delete/status),
Module 3 indirectly via document `processingStatus`, Module 6 chat + history.
Typed in `admin-dashboard/src/api/types.ts`; production clients never return
fake successes — `src/api/mocks.ts` does not exist by design; isolated tests
mock `fetch` instead.

## Verification

```bash
cd apps/admin-dashboard && npm install && npm run typecheck && npm test && npm run build && npm run security
cd apps/chat-widget    && npm install && npm run typecheck && npm test && npm run build && npm run security
```

Actual results (2026-10-15): dashboard 24/24 tests pass; widget 28/28
(libs, API guards, 10 component behaviors); both `tsc` clean; both `vite
build` succeed; `security` scans pass on `src/` and `dist/` (no service-role
keys, tokens, internal runtime URLs, or HTML sinks). Integration against a
live backend was not run (no Supabase project configured here); component
and contract tests use mocked fetch/Supabase.

## Deployment

`VITE_API_BASE_URL`, `VITE_SUPABASE_URL`, `VITE_SUPABASE_ANON_KEY` (dashboard),
`VITE_WIDGET_HOST` (snippet host) at build time; Dockerfiles build nginx
images for both apps. The widget host must allow framing by customer sites
(no `X-Frame-Options: DENY`; CSP `frame-ancestors` per deployment), while
the dashboard denies framing. Serve everything over HTTPS in production.

## Limitations and Module 8 notes

- No SSO/refresh-token rotation: expired sessions sign out; rotation is a
  future auth task.
- Dashboard team view is read-only; member add/remove stays API-only for now.
- Widget has no streaming (Module 6 contract is request/response).
- Browser matrix: evergreen Chrome/Firefox/Safari/Edge; `scrollTo` and
  `clipboard` uses are feature-guarded with graceful degradation.
- Module 8 can scrape `X-Correlation-ID` error payloads and the existing
  backend health endpoints; frontend emits no telemetry of its own.
