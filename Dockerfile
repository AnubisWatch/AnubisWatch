# Build stage
FROM golang:1.26.3-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git nodejs npm

WORKDIR /build

# Copy Go module files
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build the embedded dashboard assets.
# The project uses pnpm — install it globally instead of npm ci, since
# package-lock.json is not maintained and would fail the npm-ci check.
# Alpine's nodejs package doesn't bundle corepack, so install pnpm directly.
RUN npm install -g pnpm@10
COPY . .
RUN cd web && pnpm install --frozen-lockfile && pnpm run build:embed

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

# NOTE: For production deployments, set the container's root filesystem to
# read-only (docker run --read-only or Kubernetes securityContext
# readOnlyRootFilesystem: true). The application only requires write access
# to the paths backed by volumes:
#   - /data          (storage data — persist across restarts)
#   - /etc/anubis    (config, can be baked into image for single-node)
#   - /var/lib/anubis (temp/working dir, can be tmpfs)
# See deploy/helm/anubiswatch/values.yaml securityContext for the
# Kubernetes implementation (runAsNonRoot: true, readOnlyRootFilesystem: true).

# Expose ports
EXPOSE 8080 8443 9090 7946

# Run
ENTRYPOINT ["/bin/anubis"]
CMD ["serve", "--single"]
