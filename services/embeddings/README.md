# Local Embedding Worker

Runs `sentence-transformers/all-MiniLM-L6-v2` (384 dims) entirely locally.
No hosted inference or embedding API is ever called.

## Model download (one-time, needs network)

```
cd services/embeddings
pip install -r requirements.txt
python download_model.py        # writes ./models/all-MiniLM-L6-v2
```

## Run (offline OK afterwards)

```
# quick dev (auto-downloads from Hugging Face cache if present):
uvicorn app:app --host 127.0.0.1 --port 8082

# fully offline from local files:
HF_HUB_OFFLINE=1 EMBEDDING_MODEL_DIR=./models/all-MiniLM-L6-v2 \
  uvicorn app:app --host 127.0.0.1 --port 8082
```

The worker binds **loopback only** and is reached solely by the Go API
(`EMBEDDING_WORKER_URL`, default `http://127.0.0.1:8082`).

## Internal API

- `POST /embed` `{"texts":[...]}` → `{"embeddings":[[...]],"model":...,"dimensions":384}`
  Limits: 64 texts, 8,000 chars each. All outputs validated 384-dim + finite.
- `GET /health` → liveness. `GET /ready` → `{"ready","modelLoaded"}` for the
  Go API readiness gate (`GET /api/v1/processing/ready`).
