FROM python:3.12-slim

WORKDIR /srv
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY app.py ./

ENV EMBEDDING_MODEL_DIR=/models/all-MiniLM-L6-v2
EXPOSE 8082
USER nobody
CMD ["uvicorn", "app:app", "--host", "127.0.0.1", "--port", "8082"]
