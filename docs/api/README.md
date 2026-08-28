# API Specification

The OpenAPI document lives at **[`internal/api/openapi.yaml`](../../internal/api/openapi.yaml)**.

It is not duplicated here on purpose. The spec is embedded into the binary with
`go:embed`, so the file in `internal/api/` is both the thing you read and the
thing the server serves — a copy under `docs/` would be a second document free
to drift, which is exactly what happened before: a hand-maintained
`docs/api/openapi.yaml` (18 paths, never served), a hardcoded JSON blob in
`rest.go` (42 paths, serving an `info.version` of `4.0.0`), and a Go map that
merged a few more paths in at request time. None covered the full route table.

## Reading it from a running server

| Endpoint | Content |
|---|---|
| `/api/docs` | Swagger UI |
| `/api/openapi.json` | The spec as JSON (converted once at startup) |
| `/api/openapi.yaml` | The spec as authored |

## Changing the API

Add the operation to `internal/api/openapi.yaml` in the same change that adds
the route. `TestOpenAPISpecCoversEveryRoute` fails the build if a registered
route has no operation, and `TestOpenAPISpecHasNoPhantomOperations` fails if the
spec advertises an endpoint that is not served. A route that genuinely does not
belong in the published API goes in `routesNotInSpec` with a reason.
