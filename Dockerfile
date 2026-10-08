# service-monitor-master-go — multi-stage Docker build
# Build:  docker build -t svcmon-master:latest .
# Run:    docker compose up -d   (lihat docker-compose.yml)

# ---------- Build stage ----------
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Cache dependencies dulu biar rebuild cepat
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Static binary, tanpa CGO (jalan di alpine/scratch)
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /out/master \
    ./cmd/server

# ---------- Runtime stage ----------
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata wget

WORKDIR /app
COPY --from=builder /out/master /app/master

# Direktori yang ditulis aplikasi (dibuat juga oleh aplikasi, tapi eksplisit lebih aman)
RUN mkdir -p /app/logs /app/data

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:${PORT:-8080}/health || exit 1

ENTRYPOINT ["/app/master"]
