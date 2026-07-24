# Test Coverage Policy and Exceptions

AnubisWatch enforces exact 100% coverage on project-owned application code. Coverage is a merge gate, not an informational metric: both local targets and CI exit non-zero when the covered surface is below the configured threshold.

## Commands

### Go

```bash
make test-coverage
# equivalent: ./scripts/check-go-coverage.sh
```

The Go gate runs race-enabled tests with atomic coverage over **only**:

- `./cmd/...`
- `./internal/...`

It writes `coverage.out`, filters the generated exception into `coverage-filtered.out`, and compares covered statement blocks with total statement blocks. Direct block comparison is intentional because the formatted `go tool cover` percentage is rounded and could otherwise display `100.0%` with an uncovered block.

### Web

```bash
make dashboard-coverage
# equivalent: cd web && pnpm run test:coverage
```

Vitest starts with every application TypeScript file under `web/src/**/*.{ts,tsx}`, including `web/src/main.tsx`, and enforces 100% statements, branches, functions, and lines both globally and per file.

## Allowed exceptions

Exceptions are limited to files that are not hand-written application logic.

### Go

- `internal/grpcapi/v1/*.pb.go` — generated protobuf and gRPC bindings. Regenerate these from their schema rather than editing or unit-testing generated implementation details.

### Web

- `web/src/**/*.{test,spec}.{ts,tsx}` — test implementations.
- `web/src/test/**` — shared test setup and fixtures.
- `web/src/**/*.d.ts` — ambient/type declarations with no executable application behavior.
- `web/src/**/__generated__/**` and `web/src/**/*.generated.{ts,tsx}` — generated source.
- `web/dist/**` — compiled build output.
- `web/node_modules/**` — third-party dependencies.

There are no application-file, feature, CLI, page, hook, store, component, or entry-point exceptions. Any new exception requires an explicit policy change in this document and the corresponding coverage configuration; silently narrowing the coverage input is not acceptable.

## CI and Codecov

CI runs the same Go script and Vitest command used locally, then uploads `coverage-filtered.out` with the `backend` flag and `web/coverage/coverage-final.json` with the `frontend` flag. `codecov.yml` independently requires 100% project and patch coverage with zero threshold tolerance. Codecov upload errors also fail CI so a missing report cannot look like a successful coverage check.
