FROM python:3.13-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 PYTHONPATH=/app
WORKDIR /app
COPY backend/python/requirements.txt /app/requirements.txt
RUN pip install --no-cache-dir -r requirements.txt && useradd --uid 10002 --create-home ai
COPY backend/python/cyberguard_ai /app/cyberguard_ai
COPY docs/playbooks.md /app/docs/playbooks.md
USER 10002
EXPOSE 8090
CMD ["uvicorn", "cyberguard_ai.api:app", "--host", "0.0.0.0", "--port", "8090", "--workers", "1", "--no-access-log"]
