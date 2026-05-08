## Experimental ACME Package

This package is excluded from default builds with the `experimental_acme` build tag.

The current implementation does not complete real ACME issuance and is not wired
into runtime server paths. Production deployments must use explicit `tls.cert`
and `tls.key` values or ingress/cert-manager managed TLS.

To work on this package explicitly:

```bash
go test -tags experimental_acme ./internal/acme
```
