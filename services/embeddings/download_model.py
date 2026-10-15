"""Download all-MiniLM-L6-v2 weights + tokenizer into EMBEDDING_MODEL_DIR.

Run once with network access; afterwards the worker loads offline:
  python download_model.py
  HF_HUB_OFFLINE=1 uvicorn app:app --host 127.0.0.1 --port 8082
"""
import os

MODEL_ID = "sentence-transformers/all-MiniLM-L6-v2"
TARGET = os.environ.get("EMBEDDING_MODEL_DIR", "./models/all-MiniLM-L6-v2")


def main() -> None:
    from sentence_transformers import SentenceTransformer

    os.makedirs(TARGET, exist_ok=True)
    model = SentenceTransformer(MODEL_ID)
    model.save(TARGET)
    vec = model.encode(["download check"], normalize_embeddings=True)
    assert len(vec[0]) == 384, f"unexpected dimensions: {len(vec[0])}"
    print(f"saved {MODEL_ID} to {TARGET} (dim=384)")


if __name__ == "__main__":
    main()
