# AnubisWatch — Multi-stage Dockerfile
# ═══════════════════════════════════════════════════════════

# Stage 1: Build environment
FROM golang:1.26.2-alpine AS builder

# Install build dependencies (pin Node.js 22 for dashboard build)
RUN apk add --no-cache git make nodejs~22 npm

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the React dashboard (if exists)
RUN if [ -f web/package.json ]; then cd web && npm ci && npm run build; else echo "web/ directory not found, skipping dashboard build"; fi

# Build the Go binary (statically linked)
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -extldflags=-static" \
    -tags netgo \
    -o /build/anubis \
    ./cmd/anubis

# Stage 2: Minimal runtime image
FROM scratch

# Labels
LABEL org.opencontainers.image.title="AnubisWatch"
LABEL org.opencontainers.image.description="The Judgment Never Sleeps — Zero-dependency uptime monitoring"
LABEL org.opencontainers.image.vendor="ECOSTACK TECHNOLOGY OÜ"
LABEL org.opencontainers.image.licenses="Apache-2.0"
LABEL org.opencontainers.image.source="https://github.com/AnubisWatch/anubiswatch"
LABEL org.opencontainers.image.ref.name="ghcr.io/anubiswatch/anubiswatch"

# Copy CA certificates for HTTPS checks
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the binary
COPY --from=builder /build/anubis /anubis

# Expose ports:
# 8443 — HTTPS API + Dashboard + WebSocket
# 7946 — Raft consensus (TCP)
# 7947 — Gossip discovery (UDP)
# 9090 — gRPC API
EXPOSE 8443 7946 7947/udp 9090

# Volume for persistent data
VOLUME ["/var/lib/anubis"]

# Run as non-root (uid:gid 65534:65534 is nobody:nogroup)
USER 65534:65534

# Health check endpoint
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/anubis", "health"] || exit 1

# Entry point
ENTRYPOINT ["/anubis"]
CMD ["serve"]
