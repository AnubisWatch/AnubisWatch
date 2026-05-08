# Build stage
FROM golang:1.26.3-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git nodejs npm

WORKDIR /build

# Copy Go module files
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build the embedded dashboard assets
COPY . .
RUN cd web && npm ci && npm run build:embed

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w" \
    -o /build/anubis \
    ./cmd/anubis

# Final stage - minimal image
FROM alpine:latest

# Install ca-certificates for HTTPS and create a non-root runtime user.
RUN apk --no-cache add ca-certificates \
    && addgroup -g 1000 -S anubis \
    && adduser -u 1000 -S -G anubis -h /var/lib/anubis anubis \
    && mkdir -p /data /etc/anubis /var/lib/anubis \
    && chown -R anubis:anubis /data /etc/anubis /var/lib/anubis

# Copy binary
COPY --from=builder /build/anubis /bin/anubis
COPY --chown=anubis:anubis configs/container.anubis.json /etc/anubis/anubis.json

ENV ANUBIS_CONFIG=/etc/anubis/anubis.json \
    ANUBIS_DATA_DIR=/data

WORKDIR /var/lib/anubis
USER anubis:anubis

# Expose ports
EXPOSE 8080 8443 9090 7946

# Run
ENTRYPOINT ["/bin/anubis"]
CMD ["serve", "--single"]
