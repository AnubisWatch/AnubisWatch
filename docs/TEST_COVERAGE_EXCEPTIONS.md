# Test Coverage Policy and Exceptions

AnubisWatch enforces a minimum **80% statement coverage** gate on project-owned application code. Coverage is a merge gate, not an informational metric: local and CI checks exit non-zero below the configured threshold. Teams should continue increasing coverage above the minimum, but documentation must not claim exact 100% unless the measured profile proves it.

## Commands

### Go

```bash
make test-coverage
# equivalent: ./scripts/check-go-coverage.sh
```

The Go gate runs race-enabled tests with atomic coverage over:

- `./cmd/...`
- `./internal/...`

It writes `coverage.out`, filters generated protobuf bindings into `coverage-filtered.out`, and compares covered statement blocks with total statement blocks. Direct block comparison avoids decisions based on the rounded percentage printed by `go tool cover`.

The default minimum is 80%. A stricter local or CI experiment can override it explicitly:

```bash
GO_MIN_COVERAGE=90 ./scripts/check-go-coverage.sh
```

### Web

```bash
make dashboard-coverage
# equivalent: cd web && pnpm run test:coverage
```

Vitest covers application TypeScript under `web/src/**/*.{ts,tsx}` according to `web/vitest.config.ts` and enforces 80% global statements, branches, functions, and lines. Per-file percentages remain visible in the report, but a single small component does not invalidate an otherwise healthy application-wide profile.

## Allowed source exclusions

Exclusions are limited to generated or non-executable sources.

### Go

- `internal/grpcapi/v1/*.pb.go` — generated protobuf and gRPC bindings. Regenerate these from their schema instead of editing generated implementation details.

### Web

- `web/src/**/*.{test,spec}.{ts,tsx}` — test implementations.
- `web/src/test/**` — shared test setup and fixtures.
- `web/src/**/*.d.ts` — ambient/type declarations with no executable behavior.
- `web/src/**/__generated__/**` and `web/src/**/*.generated.{ts,tsx}` — generated source.
- `web/dist/**` — compiled build output.
- `web/node_modules/**` — third-party dependencies.

Any new application-source exclusion requires an explicit policy and configuration change; silently narrowing the coverage input is not acceptable.

## CI and Codecov

CI runs the same Go script and Vitest command used locally. Those two steps are the gate: they run before any upload and fail the job on their own.

The Codecov upload is reporting layered on top. It uploads `coverage-filtered.out` with the `backend` flag and `web/coverage/coverage-final.json` with the `frontend` flag, and `codecov.yml` asks for 80% project and patch coverage — but that second opinion only exists when a `CODECOV_TOKEN` secret is configured for the repository. The upload steps are conditional on that secret and are skipped without it.

> This used to read "Codecov upload errors fail CI so a missing report cannot appear successful." The upload did carry `fail_ci_if_error: true`, but no `CODECOV_TOKEN` was ever configured, so every push failed with `Token required - not valid tokenless upload` — after the tests and both coverage gates had passed. CI was red on `main` continuously, which meant a genuine regression was indistinguishable from the standing failure. Reporting is not allowed to fail the build over a missing org secret; the gates above are what enforce coverage.
