# Admin Dashboard

Authenticated React administration UI for JñānaSetu client organizations
(Module 7). Uses the documented Go APIs; all authorization is enforced by
the backend.

## Develop

```bash
npm install
cp .env.example .env   # set VITE_API_BASE_URL, VITE_SUPABASE_URL, VITE_SUPABASE_ANON_KEY
npm run dev            # http://localhost:5173
```

## Build, test, verify

```bash
npm run typecheck
npm test               # vitest, mocked fetch/Supabase
npm run build          # tsc + vite build → dist/
npm run security       # scans src/ and dist/ for secrets and unsafe sinks
```

Only `VITE_*` variables are bundled. Never add service-role keys or server
secrets — `npm run security` fails the build if they appear.

## Pages

- `#/signin` — Supabase Auth sign-in (GoTrue REST with the anon key).
- `#/` — organization overview + creation.
- `#/orgs/:orgId` — knowledge list, text form, file upload (.txt/.md/.pdf/.docx ≤ 10 MB).
- `#/orgs/:orgId/docs/:docId` — versions, processing status, publish/deactivate/reprocess/delete.
- `#/orgs/:orgId/widgets`, `.../widgets/:widgetId` — allowed domains, snippet, install instructions.
- `#/orgs/:orgId/team` — membership list (read-only; changes are backend-authorized).
- `#/orgs/:orgId/test-chat` — preview using the public Module 6 chat endpoint.

## Deploy

```bash
docker build -t jnana-admin .
docker run -p 80:80 jnana-admin
```

Serve `dist/` over HTTPS behind the platform reverse proxy. Configure the
three `VITE_*` variables at build time per environment.
