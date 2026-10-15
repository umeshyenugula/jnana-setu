"""Local embedding worker: sentence-transformers/all-MiniLM-L6-v2 over loopback.

Internal API (consumed only by the Go API over 127.0.0.1):
  POST /embed   {"texts": [...]} -> {"embeddings": [[...]], "model": ..., "dimensions": 384}
  GET  /health  -> {"status": "ok", ...}
  GET  /ready   -> {"ready": bool, "modelLoaded": bool, ...}

Local execution only: no hosted inference provider is ever called. The model
is loaded from local files (EMBEDDING_MODEL_DIR); after download the worker
runs fully offline (HF_HUB_OFFLINE=1).
"""

import math
import os
import threading
import time

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, field_validator

MODEL_ID = "sentence-transformers/all-MiniLM-L6-v2"
MODEL_DIR = os.environ.get("EMBEDDING_MODEL_DIR", "./models/all-MiniLM-L6-v2")
DIMENSIONS = 384
MAX_BATCH = 64
MAX_CHARS = 8000

app = FastAPI(title="Local Embedding Worker")
_started = time.time()
_model = None
_model_error: str | None = None
_lock = threading.Lock()


class EmbedRequest(BaseModel):
    texts: list[str]

    @field_validator("texts")
    @classmethod
    def check_batch(cls, v: list[str]) -> list[str]:
        if not v:
            raise ValueError("texts must not be empty")
        if len(v) > MAX_BATCH:
            raise ValueError(f"batch of {len(v)} exceeds limit {MAX_BATCH}")
        for i, t in enumerate(v):
            if not t or not t.strip():
                raise ValueError(f"text {i} is empty")
            if len(t) > MAX_CHARS:
                raise ValueError(f"text {i} exceeds {MAX_CHARS} chars")
        return v


def _load() -> None:
    global _model, _model_error
    with _lock:
        if _model is not None or _model_error is not None:
            return
        try:
            from sentence_transformers import SentenceTransformer

            src = MODEL_DIR if os.path.isdir(MODEL_DIR) else MODEL_ID
            _model = SentenceTransformer(src)
        except Exception as exc:  # controlled error, surfaced via /ready
            _model_error = f"{type(exc).__name__}: {exc}"


@app.on_event("startup")
def startup() -> None:
    _load()


@app.get("/health")
def health() -> dict:
    return {"status": "ok", "service": "embeddings", "model": MODEL_ID,
            "dimensions": DIMENSIONS, "uptimeSeconds": time.time() - _started}


@app.get("/ready")
def ready() -> dict:
    loaded = _model is not None
    return {"ready": loaded, "modelLoaded": loaded, "model": MODEL_ID,
            "dimensions": DIMENSIONS, "error": _model_error}


@app.post("/embed")
def embed(req: EmbedRequest) -> dict:
    if _model is None:
        raise HTTPException(status_code=503, detail="model not loaded")
    try:
        vectors = _model.encode(req.texts, batch_size=32, normalize_embeddings=True,
                                show_progress_bar=False)
    except Exception as exc:
        raise HTTPException(status_code=500, detail=f"embedding failed: {exc}")
    out = []
    for vec in vectors:
        vals = [float(x) for x in vec]
        if len(vals) != DIMENSIONS or any(not math.isfinite(x) for x in vals):
            raise HTTPException(status_code=500, detail="invalid embedding output")
        out.append(vals)
    return {"embeddings": out, "model": MODEL_ID, "dimensions": DIMENSIONS}
