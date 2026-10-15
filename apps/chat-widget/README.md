# Chat Widget

Embeddable customer chat for JñānaSetu client websites (Module 7). Calls
only the public Module 6 chat API. Anonymous visitors welcome; no admin
functionality is bundled.

## Develop

```bash
npm install
cp .env.example .env   # VITE_API_BASE_URL (default http://localhost:8080)
npm run dev            # http://localhost:5174
```

## Build, test, verify

```bash
npm run typecheck
npm test               # vitest + jsdom, mocked chat API
npm run build          # tsc + vite build → dist/ (app + loader.js)
npm run security       # scans src/ and dist/ for secrets and unsafe sinks
```

## Embed on a website

1. Deploy this app to a widget host, e.g. `https://widget.example.com`.
2. In the admin dashboard, register the customer domain under Widget settings.
3. Paste the snippet before `</body>`:

```html
<script src="https://widget.example.com/loader.js" data-widget-id="PUBLIC_WIDGET_ID" defer></script>
```

Optional: `data-api-base="https://api.example.com"` overrides the chat API
origin. Try it locally with `embed-example.html` (served over http) against
`npm run dev` plus the Go API.

## How it works

The loader (`public/loader.js`, no build step) validates the snippet, creates
a sandboxed iframe (`allow-scripts allow-same-origin allow-forms
allow-popups`), and relays open/close state over `postMessage` with exact
origin checks on both sides. The React app reads `?widgetId=`/`?apiBase=`,
fetches the public display name, chats via `POST /api/v1/chat`, reuses the
opaque session from `sessionStorage`, renders answers as plain text, and
links only `http(s)` URLs from verified source metadata.

## Security notes

- The public widget ID is not a secret; origin enforcement happens server-side.
- No keys, tokens, or admin endpoints exist in this bundle (`npm run security`).
- Session IDs live in `sessionStorage` per widget; conversation content is not cached elsewhere.
- Answers render as text — never raw HTML — so model output cannot execute.
