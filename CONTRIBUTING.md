# Contributing to AnubisWatch

Thank you for your interest in contributing to AnubisWatch! This document provides guidelines and instructions for contributing.

## Code of Conduct

This project and everyone participating in it is governed by our commitment to:
- Be respectful and inclusive
- Welcome newcomers
- Focus on constructive feedback
- Prioritize user safety and privacy

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/YOUR_USERNAME/anubiswatch.git`
3. Create a branch: `git checkout -b feature/your-feature-name`
4. Make your changes
5. Run tests: `make test`
6. Commit with clear messages
7. Push and create a Pull Request

## Development Setup

### Prerequisites

- Go 1.27.3, selected by `go.mod`'s toolchain directive (module language minimum: Go 1.26)
- Node.js 22 and pnpm 10 for the dashboard, matching CI
- Git

### Building

```bash
# Clone the repository
git clone https://github.com/AnubisWatch/anubiswatch.git
cd anubiswatch

# Download dependencies
go mod download

# Build the binary
make build

# Build with dashboard
make all

# Run tests
make test
```

### Dashboard Development

```bash
# Navigate to dashboard
cd web

# Install dependencies
pnpm install --frozen-lockfile

# Run dev server
pnpm run dev

# Build for production
pnpm run build:embed
```

`build:embed` builds the dashboard and copies its assets into the Go embed
directory. Use `pnpm run build` when only `web/dist` is needed.

## Project Structure

```
AnubisWatch/
├── cmd/anubis/          # CLI entry point
├── internal/
│   ├── alert/           # Alert management and dispatchers
│   ├── api/             # REST API, MCP server, routing
│   ├── auth/            # Authentication and sessions
│   ├── cluster/         # Raft consensus and clustering
│   ├── core/            # Core data types and interfaces
│   ├── dashboard/       # Web dashboard handler
│   ├── journey/         # Synthetic monitoring journeys (Duat)
│   ├── probe/           # Health check implementations
│   ├── raft/            # Raft consensus implementation
│   ├── statuspage/      # Public status page generator
│   └── storage/         # CobaltDB storage engine
├── docs/                # Documentation
├── .project/            # Project metadata and roadmap
└── web/                 # React dashboard (optional)
```

## Coding Standards

### Go Code

- Follow standard Go conventions (gofmt, govet)
- Run `make lint`: it uses CI's pinned `golangci-lint` v2.15.0 with the checked-in v2 `.golangci.yml`
- Keep functions focused and small
- Document exported types and functions
- Use meaningful variable names
- Handle errors explicitly

### Style Guidelines

```go
// Good: Clear, documented function
// IsHealthy checks if the soul is currently healthy
func (s *Soul) IsHealthy() bool {
    return s.Status == SoulAlive
}

// Good: Error handling
result, err := doSomething()
if err != nil {
    return fmt.Errorf("failed to do something: %w", err)
}
```

### Testing

- Write unit tests for new functionality
- Enforce the 80% backend statement gate with `make test-coverage`; it runs race-enabled tests for `cmd` and `internal`, excluding generated protobuf bindings
- Run dashboard coverage with `pnpm --dir web run test:coverage`; its statements, branches, functions and lines thresholds are all 80%
- Use table-driven tests where appropriate
- Mock external dependencies

```go
func TestSoulIsHealthy(t *testing.T) {
    tests := []struct {
        name     string
        status   SoulStatus
        expected bool
    }{
        {"alive", SoulAlive, true},
        {"dead", SoulDead, false},
        {"degraded", SoulDegraded, false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            s := &Soul{Status: tt.status}
            if got := s.IsHealthy(); got != tt.expected {
                t.Errorf("IsHealthy() = %v, want %v", got, tt.expected)
            }
        })
    }
}
```

## Commit Messages

Use clear, descriptive commit messages:

```
feat: add HTTP checker with custom headers
fix: resolve race condition in probe engine
docs: update API documentation
test: add unit tests for dispatchers
refactor: simplify Raft log compaction
chore: update dependencies
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `test`: Test changes
- `refactor`: Code refactoring
- `perf`: Performance improvements
- `chore`: Maintenance tasks

## Pull Request Process

1. Update documentation for any changed functionality
2. Add tests for new code
3. Ensure all tests pass: `make test`
4. Update CHANGELOG.md if applicable
5. Request review from maintainers
6. Address review feedback

## Areas for Contribution

### Priority Areas

- Additional protocol checkers (Redis, MongoDB, PostgreSQL)
- More alert channels (Microsoft Teams, Matrix, etc.)
- Dashboard improvements
- Performance optimizations
- Documentation improvements
- Bug fixes

### Good First Issues

Look for issues labeled:
- `good-first-issue`
- `help-wanted`
- `documentation`

## Security

If you discover a security vulnerability:
1. Do NOT open a public issue
2. Email security@anubis.watch
3. Include detailed description and reproduction steps
4. Allow time for fix before disclosure

## Testing

### Unit Tests

```bash
make test
```

### Integration Tests

```bash
# Offline Kubernetes drill regressions; no cluster access
bash scripts/k8s-drill/k8s-drill_test.sh

# Chart rendering tests require the CI-pinned Helm 3.14.4
go test -count=1 ./deploy/helm/anubiswatch/tests
```

For the live eight-scenario drill, use an explicitly owned disposable cluster,
kubeconfig, context and candidate image. Follow
[the drill guide](docs/deployment/k8s-acceptance-drill.md); the runner's
`--help` lists the required arguments. A successful offline test is not a live
deployment result. Chaos and load suites require their explicit opt-ins in CI.

### Manual Testing

```bash
# Start the server
./bin/anubis serve

# Add a test monitor
./bin/anubis watch https://httpbin.org/get

# View judgments
./bin/anubis judge
```

## Documentation

- Update README.md for user-facing changes
- Update SPECIFICATION.md for architectural changes
- Add code comments for complex logic
- Update API documentation for endpoint changes

## Release Process

Releases are managed by maintainers:
1. Choose the release version; build metadata lives in `cmd/anubis/main.go` and is injected through linker flags by the Makefile/release workflow
2. Update CHANGELOG.md
3. Create git tag: `git tag v0.x.x`
4. Push tag: `git push origin v0.x.x`
5. GitHub Actions builds and publishes

## Questions?

- Open a Discussion for questions
- Join our Discord: https://discord.gg/anubiswatch
- Email: hello@anubis.watch

## License

By contributing, you agree that your contributions will be licensed under the MIT License.

---

⚖️ **The Judgment Never Sleeps**
