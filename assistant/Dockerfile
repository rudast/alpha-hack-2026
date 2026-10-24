FROM python:3.12-slim

WORKDIR /app

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PORT=8081

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .
RUN ln -s /app /app/assistant

ENV PYTHONPATH=/app

RUN useradd -u 10001 -m assistant && chown -R assistant:assistant /app
USER assistant

EXPOSE 8081

HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
    CMD python3 -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8081/healthz')" || exit 1

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8081"]
