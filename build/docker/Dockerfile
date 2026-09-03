# Multi-stage build for Go services
FROM golang:1.25-bookworm AS builder
ARG SERVICE
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/${SERVICE} .

FROM debian:bookworm-slim
ARG SERVICE
RUN apt-get update && apt-get install -y ca-certificates wget && rm -rf /var/lib/apt/lists/*
COPY --from=builder /bin/${SERVICE} /app/service
EXPOSE 8000
CMD ["/app/service"]
