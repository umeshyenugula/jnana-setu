# Module 5 — Local Model Inference

Independently testable text generation on a locally hosted Qwen3-4B (GGUF
Q4_K_M) via llama.cpp. Accepts prepared messages from Module 6, returns
predictable text. No retrieval, orchestration, tenants, or hosted providers.

## What was implemented

- `services/api/internal/inference/`: `model` (schemas, stable codes),
  `policy` (message/parameter validation, budgets, `<think>` stripping),
  `runtime` (Runtime interface, llama.cpp chat-completions client, health
  probe, deterministic mock), `service` (bounded concurrency, timeouts,
  cancellation, metrics, graceful drain), `metrics` (counters only — prompts
  and customer text are never logged), `http` (service-token auth, Module 0
  envelopes). Wired in `router`, `cmd/api/main.go` (incl. signal-aware
  graceful shutdown), `config`.
- `contracts/api/v1/inference.yaml`. `llm` service in
  `infra/docker/docker-compose.yml` (ghcr.io/ggerganov/llama.cpp server,
  loopback-style private networking, model volume).

## Runtime setup (Task A)

Model acquisition (one-time, needs network):

```powershell
mkdir models\llm
# Qwen3-4B-GGUF Q4_K_M (~2.5 GB) from Hugging Face, e.g.:
huggingface-cli download Qwen/Qwen3-4B-GGUF `
  --include "Qwen3-4B-Q4_K_M.gguf" --local-dir models\llm
# Verify the SHA256 published on the model page before use.
```

llama.cpp build: any release ≥ b6000 with OpenAI-compatible server
(`llama-server`), Qwen3 chat template (built-in `--jinja` default handles
Qwen3, including `enable_thinking`). Start locally:

```powershell
llama-server -m models\llm\Qwen3-4B-Q4_K_M.gguf `
  --host 127.0.0.1 --port 8081 --ctx-size 4096 `
  --n-predict 512 --threads 6 --no-mmap:$false
```

Or `docker compose up llm` (downloads image; mount `./models/llm` read-only).
The runtime binds loopback (dev) or the private compose network only — never
the public internet. Offline afterwards: model loads from local files; set
`HF_HUB_OFFLINE=1` if the environment tries to reach the Hub.

Configuration: context window 4096 tokens, default max output 512,
temperature 0.3 / top_p 0.9 (concise support tone), non-thinking operation
(`LLM_ENABLE_THINKING=false` sends `enable_thinking:false`; output is still
defensively stripped of `<think>` blocks). One active generation fits the
16 GB dev laptop (model ~2.5 GB + ~1–2 GB context); raise
`LLM_MAX_CONCURRENT` only with measured headroom. CPU inference works;
add `--gpu-layers N` when a compatible GPU exists.

Readiness: `GET /api/v1/internal/inference/ready` → 200 only when `GET
<runtime>/health` succeeds; model-loading failures surface as stable
`MODEL_NOT_READY` (details stay server-side). No crash-loop restarts:
compose uses `unless-stopped` without a restart-on-failure policy for `llm`.

## Generation API (Module 6 contract)

`POST /api/v1/internal/inference/generate`, header
`X-Internal-Service-Token: <INTERNAL_SERVICE_TOKEN>`, body
`{messages: [{role, content}…], settings?: {temperature?, topP?, maxTokens?, stop?}}`.
Response: `{text, finishReason: stop|length|error, promptTokens,
completionTokens, model, thinkingStripped}` + Module 0 meta (correlation ID).
Streaming is intentionally absent: output is validated first.

Limits: ≤20 messages, ≥1 user message, ≤12000 prompt chars, temperature
0–2, topP (0,1], maxTokens 1–server max, ≤4 stop sequences, 64 KiB body.
Callers cannot set model paths or runtime args. One concurrent generation by
default; excess work fails fast with 429 `MODEL_OVERLOADED`. Timeouts (504),
cancellation (499), malformed runtime output (502), and empty model output
(502) all map to stable codes. Diagnostics never leak to callers.

## Observability (Task D)

`GET /api/v1/internal/inference/health` returns liveness plus counters:
requests/success/failures-by-code/overloaded/timeouts, token totals,
avg latency, model-load errors. Logs carry sizes, codes, and correlation
IDs only. Shutdown: SIGINT/SIGTERM → stop accepting → 15 s HTTP drain →
30 s in-flight generation drain.

Benchmark on your hardware before promising latency:

```powershell
$body = @{messages=@(@{role='user';content='Say hello briefly.'})} | ConvertTo-Json
Measure-Command { Invoke-RestMethod -Uri http://localhost:8080/api/v1/internal/inference/generate `
  -Method Post -Headers @{'X-Internal-Service-Token'=$env:INTERNAL_SERVICE_TOKEN} `
  -ContentType 'application/json' -Body $body }
```

Expect single-digit to tens of seconds per response for 100–300 output
tokens on the 16 GB CPU laptop; first request pays model-warmup cost.

## Tests

`go test ./internal/inference/ -v` — **24 tests pass** (isolated mock runtime
in tests only; production has no mock path):
valid generation, thinking stripping, invalid/empty messages, parameter
ranges, prompt limits, not-ready 503, timeout 504 (+counter), cancellation,
malformed/empty-choices/503-status/refused runtime paths, success parsing,
slot exhaustion 429 (+counter), graceful drain and drain-timeout, auth
(missing/wrong/unconfigured), bad bodies, health/ready reporting.

Local-model smoke test (requires downloaded weights + running llama-server):

```powershell
go run ./cmd/api
# then POST /api/v1/internal/inference/generate as above; expect 200 with concise text,
# thinkingStripped=false, finishReason=stop.
```

**Status: NOT executed — Qwen3-4B weights and llama.cpp are not installed in
this environment.** Do not treat isolated test-runtime results as model validation.

## Module 6 integration

1. Send Module 6-prepared messages (system prompt + evidence + question) to
   `POST /api/v1/internal/inference/generate` with the service token; keep prompts within
   12000 chars. 2. Use `data.text` as the answer body; 3. On 429 back off and
   retry once; on 503/504 degrade gracefully (never fabricate). 4. Treat
   returned text as untrusted (render as text, never execute).

## Limitations

- Non-streaming only; single default concurrency (throughput ≈ 60–120 s per
  long answer on CPU — size Module 6 timeouts accordingly).
- Token counts come from the runtime `usage` field; approximate when absent.
- Qwen3 thinking disabled by default; enabling it needs calibration of
  stripping vs `reasoning_content` handling per llama.cpp version.
