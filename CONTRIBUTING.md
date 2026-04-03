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

- Go 1.24 or later
- Node.js 20 or later (for dashboard)
- Make
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
npm install

# Run dev server
npm run dev

# Build for production
npm run build
```

## Project Structure

```
AnubisWatch/
├── cmd/anubis/          # CLI entry point
├── internal/
│   ├── api/             # REST, WebSocket, gRPC, MCP
│   ├── checkers/        # Protocol checkers
│   ├── core/            # Domain types
│   ├── feather/         # Storage engine
│   ├── jackal/          # Probe engine
│   ├── maat/            # Alert engine
│   ├── dispatch/        # Alert dispatchers
│   ├── raft/            # Raft consensus
│   ├── necropolis/      # Cluster coordination
│   ├── journey/         # Time-series storage
│   ├── acme/            # ACME certificates
│   ├── statuspage/      # Public status pages
│   └── storage/         # Repository implementations
└── web/                 # React dashboard
```

## Coding Standards

### Go Code

- Follow standard Go conventions (gofmt, govet)
- Use `golangci-lint` for linting: `make lint`
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
- Aim for >70% coverage
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
make test-integration
```

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
1. Version bump in version.go
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
