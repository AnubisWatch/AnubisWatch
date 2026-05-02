# Build stage
FROM golang:1.26.2-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git nodejs npm

WORKDIR /build

# Copy Go module files
COPY go.mod go.sum ./
RUN go mod download

# Copy dashboard source and build
COPY web/ ./web/
RUN cd web && npm ci && npm run build

# Copy Go source
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w" \
    -o /build/anubis \
    ./cmd/anubis

# Final stage - minimal image
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy binary
COPY --from=builder /build/anubis .

# Expose ports
EXPOSE 8080 8443 9090 7946

# Run
CMD ["./anubis", "serve", "--single"]
