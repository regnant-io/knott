FROM python:3.12-slim
WORKDIR /app
# No third-party dependencies — the engine uses only the Python standard library.
COPY . .
EXPOSE 8003
CMD ["python3", "main.py"]
