# Web Coverage Report

Generated from:
```bash
cd web && npm run test:coverage
```

This script currently resolves to:
```bash
vitest run --coverage --sequence.concurrent false
```

Artifacts:
- HTML: `web/coverage/index.html`
- JSON: `web/coverage/coverage-final.json`

## Latest measured summary

- Statements: **80.68%**
- Branches: **69.30%**
- Functions: **73.95%**
- Lines: **82.62%**

## Highest remaining gaps

Primary remaining low-coverage files after the latest test additions:
- `src/pages/Journeys.tsx`
- `src/api/hooks.ts`
- `src/api/client.ts` (substantially improved, but still not full)
- `src/pages/SoulEdit.tsx`
- `src/pages/SoulDetail.tsx`

Improved significantly in this pass:
- `src/pages/Maintenance.tsx`
- `src/pages/Souls.tsx`
- `src/pages/StatusPages.tsx`
- `src/pages/Alerts.tsx`
- `src/pages/Dashboard.tsx`
- `src/pages/Incidents.tsx`
- `src/pages/Judgments.tsx`
- `src/pages/Settings.tsx`
- `src/pages/DashboardDetail.tsx`
- `src/utils/soulForm.ts`
- `src/components/SoulProtocolFields.tsx`
- `src/api/client.ts`

## Stability note

The documented coverage command is the supported one for this repository because it serializes the Vitest run to avoid order-sensitive interaction between module-mocked page suites while still producing the full coverage artifact set.

See `../TEST_COVERAGE_EXCEPTIONS.md` for explicit rationale about what is still uncovered and why.
