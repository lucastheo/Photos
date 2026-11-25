# syntax=docker/dockerfile:1

FROM node:22-alpine AS frontend-builder
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/embed.go ./web/embed.go
COPY --from=frontend-builder /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/phototracker ./cmd/phototracker

FROM debian:bookworm-slim AS runtime
RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    ffmpeg \
    libheif-examples \
    libimage-exiftool-perl \
  && rm -rf /var/lib/apt/lists/* \
  && groupadd --gid 10001 phototracker \
  && useradd --uid 10001 --gid 10001 --home-dir /data --create-home --shell /usr/sbin/nologin phototracker \
  && mkdir -p /photos /data/cache \
  && chown -R phototracker:phototracker /photos /data

COPY --from=backend-builder /out/phototracker /usr/local/bin/phototracker

ENV LIBRARY_PATH=/photos \
    DB_PATH=/data/photos.db \
    CACHE_PATH=/data/cache \
    HOST=0.0.0.0 \
    PORT=8080

USER phototracker
WORKDIR /data
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -fsS "http://127.0.0.1:${PORT}/api/v1/health" || exit 1

ENTRYPOINT ["/usr/local/bin/phototracker"]
