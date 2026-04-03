# AnubisWatch Makefile
# ═══════════════════════════════════════════════════════════

BINARY    := anubis
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT    := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE      := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS   := -s -w \
  -X github.com/AnubisWatch/anubiswatch/internal/core.Version=$(VERSION) \
  -X github.com/AnubisWatch/anubiswatch/internal/core.Commit=$(COMMIT) \
  -X github.com/AnubisWatch/anubiswatch/internal/core.BuildDate=$(DATE)

.PHONY: all build clean test lint dashboard run docker help

all: dashboard build ## Build dashboard and binary

build: ## Build the anubis binary
	@echo "⚖️  Building AnubisWatch..."
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/anubis
	@echo "✓ Build complete: bin/$(BINARY)"

run: build ## Build and run the server
	./bin/$(BINARY) serve

dev: ## Run in development mode (single node, no TLS)
	go run ./cmd/anubis serve --single --config ./anubis.yaml

clean: ## Clean build artifacts
	rm -rf bin/ web/dist/
	@echo "✓ Cleaned"

test: ## Run all tests
	go test -race -coverprofile=coverage.out ./...

test-short: ## Run short tests only
	go test -short ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format Go code
	go fmt ./...

vet: ## Run go vet
	go vet ./...

# Dashboard
dashboard: ## Build React dashboard
	@echo "⚖️  Building dashboard..."
	cd web && npm ci && npm run build
	@echo "✓ Dashboard built"

dashboard-dev: ## Run dashboard dev server
	cd web && npm run dev

# Cross-compilation
build-all: ## Build for all platforms
	$(MAKE) build-linux-amd64
	$(MAKE) build-linux-arm64
	$(MAKE) build-linux-armv7
	$(MAKE) build-darwin-amd64
	$(MAKE) build-darwin-arm64
	$(MAKE) build-windows-amd64
	$(MAKE) build-freebsd-amd64

build-linux-amd64:
	@echo "Building for linux/amd64..."
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-amd64 ./cmd/anubis

build-linux-arm64:
	@echo "Building for linux/arm64..."
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-arm64 ./cmd/anubis

build-linux-armv7:
	@echo "Building for linux/armv7..."
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-armv7 ./cmd/anubis

build-darwin-amd64:
	@echo "Building for darwin/amd64..."
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-darwin-amd64 ./cmd/anubis

build-darwin-arm64:
	@echo "Building for darwin/arm64..."
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-darwin-arm64 ./cmd/anubis

build-windows-amd64:
	@echo "Building for windows/amd64..."
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-windows-amd64.exe ./cmd/anubis

build-freebsd-amd64:
	@echo "Building for freebsd/amd64..."
	GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-freebsd-amd64 ./cmd/anubis

# Docker
docker: ## Build Docker image
	docker build -t anubiswatch/$(BINARY):$(VERSION) .
	docker tag anubiswatch/$(BINARY):$(VERSION) anubiswatch/$(BINARY):latest

docker-push: docker ## Push Docker image
	docker push anubiswatch/$(BINARY):$(VERSION)
	docker push anubiswatch/$(BINARY):latest

# Release
release: clean dashboard build-all ## Prepare release artifacts
	@echo "⚖️  Creating release artifacts..."
	mkdir -p dist
	for f in bin/$(BINARY)-*; do \
		name=$$(basename $$f); \
		cp $$f dist/$$name; \
		tar -czf dist/$$name.tar.gz -C bin $$(basename $$f); \
	done
	@echo "✓ Release artifacts in dist/"

# Development helpers
init: ## Initialize default config
	./bin/$(BINARY) init

watch: ## Quick-add a monitor (requires TARGET)
	./bin/$(BINARY) watch $(TARGET)

judge: ## Show current judgments
	./bin/$(BINARY) judge

# Dependencies
deps: ## Download and verify dependencies
	go mod download
	go mod verify

deps-update: ## Update dependencies
	go get -u ./...
	go mod tidy

tidy: ## Tidy go modules
	go mod tidy

# Documentation
docs: ## Generate documentation
	@echo "Documentation available in:"
	@echo "  - .project/SPECIFICATION.md"
	@echo "  - .project/IMPLEMENTATION.md"
	@echo "  - .project/TASKS.md"

# Help
help: ## Show this help
	@echo "AnubisWatch — The Judgment Never Sleeps"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
