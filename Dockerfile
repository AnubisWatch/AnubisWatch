# AnubisWatch — Multi-stage Dockerfile
# ═══════════════════════════════════════════════════════════

# Stage 1: Build environment
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make nodejs npm

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the React dashboard
RUN cd web && npm ci && npm run build

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
