
FROM python:3.12-slim

WORKDIR /srv
COPY requirements.txt ./

RUN mkdir -p /models && chown -R nobody:nogroup /models

# Install CPU-only PyTorch first.
RUN pip install --no-cache-dir \
    --index-url https://download.pytorch.org/whl/cpu \
    torch==2.5.1

# Install the remaining application dependencies.
RUN pip install --no-cache-dir -r requirements.txt

COPY app.py ./

ENV EMBEDDING_MODEL_DIR=/models/all-MiniLM-L6-v2
ENV HF_HOME=/tmp/huggingface
ENV TRANSFORMERS_CACHE=/tmp/huggingface
EXPOSE 8082

USER nobody
CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8082"]
