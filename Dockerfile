# Build stage. Pin both the Go version and the Alpine minor so the
# build is reproducible — `golang:1.26.6-alpine` would float the
# underlying musl libc across rebuilds. The Go team publishes
# `golang:X.Y.Z-alpineN.M` tags for each stable Alpine.
FROM golang:1.26.6-alpine3.24 AS builder

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

# Build binary.
#
# The version stamp is passed in rather than derived: the build context has no
# .git (see .dockerignore), so `git describe` inside the builder is impossible.
# Without these the image reports `anubis version` = dev/unknown/unknown, which
# makes a running container unidentifiable — and SECURITY.md asks vulnerability
# reporters for exactly that output. Defaults match the Go source so a bare
# `docker build` still works.
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w \
      -X main.Version=${VERSION} \
      -X main.Commit=${COMMIT} \
      -X main.BuildDate=${BUILD_DATE}" \
    -o /build/anubis \
    ./cmd/anubis

# Final stage - minimal image. Pinned to alpine 3.24 (the current
# stable; security-only branches are 3.22 and 3.21). Float `latest`
# here was a reproducibility hazard — every rebuild would silently
# pick up whatever musl version Docker Hub is currently serving,
# which is the kind of drift that turns "I just rebuilt" into
# "now my Go binary segfaults on startup because musl changed".
FROM alpine:3.24

# Apply the base image's pending security updates before anything else.
# alpine:3.24 is pinned for reproducibility (see the note above), but a pinned
# tag also freezes whatever package versions that tag shipped with — Trivy
# blocks the build on CVE-2026-14456 (openssl QUIC DoS, HIGH), fixed in
# libcrypto3/libssl3 3.5.8-r0 but not yet rolled into the tag. Taking the
# security updates is the right side of that trade for a security product;
# the Go binary is statically linked and CGO-free, so this only affects the
# runtime image's own packages.
RUN apk --no-cache upgrade

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

# Liveness probe for `docker run` (compose/K8s define their own). Uses the
# plaintext HTTP port the baked container config listens on (8080) and busybox
# wget, which is present in the alpine base.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/health >/dev/null 2>&1 || exit 1

# Run
ENTRYPOINT ["/bin/anubis"]
CMD ["serve", "--single"]
