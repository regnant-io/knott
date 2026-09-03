# KNOTT — one image, one process, one port.
#
# The console is built and compiled into the binary, so the runtime stage carries
# no static-file directory and no Node. The optional Python AI engine ships with
# it; KNOTT falls back to deterministic rule-based decisions when it is absent,
# so the image works either way.
#
#   docker build -f build/docker/Dockerfile -t knott .
#   docker run -p 8002:8002 -v knott-data:/var/lib/knott \
#              -e API_KEYS=change-me:admin knott

# ─── Console ──────────────────────────────────────────────────────────────────
FROM node:22-bookworm-slim AS console
WORKDIR /src
COPY apps/designer/package.json apps/designer/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY apps/designer/ ./
RUN npm run build

# ─── Binary ───────────────────────────────────────────────────────────────────
FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=console /src/dist/ ./internal/ui/dist/
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/knott ./cmd/knott

# ─── Runtime ──────────────────────────────────────────────────────────────────
FROM python:3.12-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 10001 --home-dir /var/lib/knott --shell /usr/sbin/nologin knott \
 && install -d -o knott -g knott -m 0750 /var/lib/knott

COPY --from=build /out/knott /usr/local/bin/knott
COPY services/ai-decision-engine/main.py /usr/local/bin/ai-decision-engine/main.py
COPY LICENSE NOTICE /usr/share/doc/knott/

USER knott
WORKDIR /var/lib/knott
ENV KNOTT_HOME=/var/lib/knott \
    KNOTT_BIND_HOST=0.0.0.0 \
    PORT=8002

EXPOSE 8002
VOLUME ["/var/lib/knott"]

HEALTHCHECK --interval=15s --timeout=4s --start-period=20s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8002/api/v1/health || exit 1

ENTRYPOINT ["knott"]
CMD ["serve", "--quiet"]
