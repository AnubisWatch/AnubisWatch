# Test Coverage Exceptions

This document records the current uncovered areas that remain after the latest automated test pass and explains why they are not yet at 100% practical coverage.

## Coverage artifacts

- Go coverage profile: `coverage.out`
- Web coverage report directory: `web/coverage/`
- Web HTML report entrypoint: `web/coverage/index.html`
- Web JSON coverage map: `web/coverage/coverage-final.json`

## Full-suite commands

### Go
```bash
go test ./... -coverprofile=coverage.out -covermode=atomic
```

### Web unit/integration coverage
```bash
cd web && npm run test:coverage
```
This command runs Vitest coverage with `--sequence.concurrent false` to keep module-mocked page tests isolated and reproducible in the full suite.

### Web E2E smoke
```bash
cd web && npm run e2e
```

## Remaining practical exceptions

### Generated / third-party / low-value files
- `internal/grpcapi/v1/*.pb.go`
  - Generated protobuf/grpc bindings. These are machine-generated artifacts and are not practical line/branch coverage targets.
- `web/node_modules/**`
  - Third-party vendored dependency code; not project-owned test surface.

### Remaining large UI/state matrices
These files still contain broad interaction surfaces and state permutations that were not fully closed in this pass.

- `web/src/pages/Journeys.tsx`
  - Complex create/edit modal and run-history branch matrix remains the largest uncovered page flow.
- `web/src/pages/SoulEdit.tsx`
  - Still has untested negative and variant editing branches.
- `web/src/pages/SoulDetail.tsx`
  - Core interactions are covered, but deep tab/content permutations still remain.

### Shared hook/client internals
- `web/src/api/hooks.ts`
  - Hook internals have many fetch/error/refetch paths and mounted-state branches; current tests exercise major consumers but not every internal branch directly.
- `web/src/api/client.ts`
  - Significantly improved, but still has uncovered serialization/normalization branches not all exposed through current UI flows.

### Stable full-suite limitation
- The repository now has a stable documented web coverage command (`npm run test:coverage` → `vitest run --coverage --sequence.concurrent false`), and that command passes in documented verification runs.
- Some page suites use partial module mocks of shared hook modules. The documented serialized invocation is stable, but future coverage work should continue moving logic into lower-level pure helper tests where possible to avoid order-sensitive interactions.

### CLI / backend breadth
The Go suite fully passes, but overall backend coverage remains below 100% because the CLI and several operational command paths depend on OS/process/network conditions and numerous argument permutations.

Representative low-coverage areas include:
- `cmd/anubis/config.go`
- `cmd/anubis/init.go`
- `cmd/anubis/cluster.go`
- `cmd/anubis/backup.go`

These are practical to continue improving, but full branch closure requires many environment/IO permutations and is not yet complete.

## Important note

At this stage, the test suite has a 100% pass rate under the documented command surface used for verification, but the repository does **not** yet have 100% line/branch coverage. This file exists to make the current exceptions explicit rather than implicit.
