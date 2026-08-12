# Security Policy

## Reporting a Vulnerability

If you believe you have found a security vulnerability in AnubisWatch, **please do not open a public GitHub issue**.

Instead, report it privately using one of these methods:

1. **GitHub Security Advisories** (preferred): Go to the [Security tab](https://github.com/AnubisWatch/AnubisWatch/security/advisories/new) and click **"Report a vulnerability"**.
2. **Email**: Send details to **security@anubis.watch** with the subject line `[SECURITY] AnubisWatch`.

Please include the following in your report:

- A description of the vulnerability and its potential impact.
- Steps to reproduce (proof-of-concept, affected endpoints, config).
- The version you tested against (run `anubis version`).
- Any relevant logs, screenshots, or stack traces.

### Response Timeline

| Step | Target |
|---|---|
| Acknowledgement of your report | Within **48 hours** |
| Initial assessment (valid / invalid / needs more info) | Within **5 business days** |
| Fix or mitigation for confirmed high/critical issues | Within **30 days** of confirmation |
| Public disclosure (after a fix is released) | Coordinated with you unless there is active exploitation |

We will keep you informed throughout the process and credit you in the release advisory unless you prefer to remain anonymous.

## Supported Versions

AnubisWatch is pre-1.0 software (`v0.x.y`). Only the **latest minor release** receives security fixes.

| Version | Supported | Notes |
|---|---|---|
| `v0.1.x` (latest: `v0.1.4`) | ✅ Active | Current release line |
| `< v0.1.0` | ❌ Unsupported | Early development builds |

When a new minor version (`v0.2.0`) is released, the previous minor line will receive security-only patches for **30 days**, then become unsupported.

## Scope

### In Scope

- Authentication or authorization bypass (local, LDAP, OIDC flows).
- Cross-workspace data leakage (multi-tenancy isolation).
- SSRF protections in probe types (HTTP, TCP, DNS, etc.).
- Injection vulnerabilities (SQL, command, template, etc.).
- Sensitive data exposure in logs, API responses, or error messages.
- Privilege escalation (RBAC, workspace roles, admin access).
- Cryptographic weaknesses (WAL encryption, token generation, cookie handling).

### Out of Scope

- Self-hosted misconfiguration (weak passwords, exposed ports, missing TLS) — see [docs/CONFIGURATION.md](docs/CONFIGURATION.md).
- Denial of service via resource exhaustion on intentionally unbounded public endpoints (rate limits are configurable).
- Vulnerabilities in third-party dependencies — report to the upstream project. Run `govulncheck` and `npm audit` to check known CVEs.
- Social engineering or physical attacks.

If you are unsure whether something is in scope, report it anyway — we will triage and let you know.

## Security Measures Already in Place

- **SSRF protection** on all outbound probes (`internal/probe/ssrf.go`).
- **Secret redaction** in API responses and gRPC (`internal/api/secret_redaction.go`).
- **CSRF protection** on REST, WebSocket, and OIDC flows.
- **Encryption at rest** for WAL and stored data.
- **Rate limiting** on authentication and API endpoints.
- **httpOnly cookies** for session tokens (no token in URL query strings).
- **Per-workspace isolation** enforced at the storage and API layers.

## Dependency Security

We track known vulnerabilities in our dependencies:

- **Go**: `govulncheck ./...` runs in CI.
- **Node.js**: `npm audit` runs against the frontend workspace.

If you find a vulnerable dependency that we have not patched, please report it — but also check whether the vulnerable code path is actually reachable in AnubisWatch.
