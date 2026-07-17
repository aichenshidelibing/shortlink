# ─── Stage 1: build ───────────────────────────────────────────────
FROM golang:1.26.5-alpine3.24 AS builder

WORKDIR /src

# Use module cache separately so source changes don't invalidate deps.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO off for a portable static binary; strip symbols to shrink the image.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -buildvcs=false \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/shortlink ./cmd/server

# ─── Stage 2: runtime ─────────────────────────────────────────────
# alpine (not scratch) so we still have wget for the healthcheck and
# ca-certificates for outbound HTTPS to notification webhooks / CDNs.
FROM alpine:3.24

RUN apk --no-cache add ca-certificates tzdata wget && \
    addgroup -S shortlink && adduser -S -G shortlink shortlink

WORKDIR /app
COPY --from=builder /out/shortlink /app/shortlink
COPY configs/docker.yaml /app/configs/config.yaml
COPY data /app/data
COPY web /app/web

RUN mkdir -p /app/data && chown -R shortlink:shortlink /app

USER shortlink

ENV TZ=Asia/Shanghai
EXPOSE 8080

# App-level healthcheck. Wired to docker-compose so `docker ps` shows the
# real health of the http server, not just "container running".
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${SERVER_PORT:-8080}/api/config" > /dev/null 2>&1 || exit 1

ENTRYPOINT ["/app/shortlink"]
