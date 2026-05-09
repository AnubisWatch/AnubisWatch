# Production Deployment Runbook

This runbook is the operator checklist for promoting a tested AnubisWatch build
to a production or staging Kubernetes environment.

## Preconditions

- Target image digest or tag is known and has passed GitHub CI.
- Kubernetes context points at the target cluster.
- `helm`, `kubectl`, and `curl` are installed locally.
- `secrets.adminPassword`, `secrets.jwtSecret`, `secrets.sessionSecret`, and
  `secrets.clusterSecret` are set from the deployment secret store.
- TLS is handled by ingress/cert-manager or by explicitly mounted `cert` and
  `key` files. Built-in certificate automation is not provided.
- A rollback revision is available from `helm history`.

## Preflight

```bash
export NAMESPACE=anubiswatch
export RELEASE=anubiswatch
export CHART=deploy/helm/anubiswatch
export VALUES=values-production.yaml

kubectl config current-context
kubectl get namespace "$NAMESPACE" || kubectl create namespace "$NAMESPACE"
helm lint "$CHART"
helm template "$RELEASE" "$CHART" \
  --namespace "$NAMESPACE" \
  -f "$VALUES" >/tmp/anubiswatch-rendered.yaml
kubectl apply --dry-run=server -f /tmp/anubiswatch-rendered.yaml
```

Review the rendered manifest before applying if the change touches storage,
ingress, secrets, RBAC, or replica counts.

## Deploy

```bash
helm upgrade --install "$RELEASE" "$CHART" \
  --namespace "$NAMESPACE" \
  --create-namespace \
  -f "$VALUES" \
  --atomic \
  --timeout 10m
```

Wait for the selected workload to become ready:

```bash
kubectl -n "$NAMESPACE" rollout status statefulset/anubiswatch --timeout=10m
kubectl -n "$NAMESPACE" get pods,svc,ingress
```

If the environment uses the deployment-based chart, replace
`statefulset/anubiswatch` with `deployment/anubiswatch`.

## Smoke Test

Run public checks after DNS and ingress have converged:

```bash
scripts/production-smoke.sh https://anubiswatch.example.com
```

Run authenticated checks when admin credentials are available:

```bash
ANUBIS_SMOKE_EMAIL=admin@example.com \
ANUBIS_SMOKE_PASSWORD="$ANUBIS_ADMIN_PASSWORD" \
scripts/production-smoke.sh https://anubiswatch.example.com
```

Include rollout validation with the same command when running from an operator
machine that has cluster access:

```bash
ANUBIS_SMOKE_NAMESPACE=anubiswatch \
ANUBIS_SMOKE_WORKLOAD=statefulset/anubiswatch \
ANUBIS_SMOKE_EMAIL=admin@example.com \
ANUBIS_SMOKE_PASSWORD="$ANUBIS_ADMIN_PASSWORD" \
scripts/production-smoke.sh https://anubiswatch.example.com
```

The smoke script verifies:

- Kubernetes rollout status when requested.
- `/health`, `/ready`, `/metrics`, `/api/openapi.json`, and the dashboard shell.
- Optional authenticated login, current-user lookup, soul listing, and stats
  overview.
- TLS certificate validation for HTTPS URLs unless `ANUBIS_SMOKE_INSECURE=true`
  is explicitly set.

## Rollback

Use rollback when rollout, health, or authenticated checks fail and the issue is
not an external dependency problem.

```bash
helm history "$RELEASE" --namespace "$NAMESPACE"
helm rollback "$RELEASE" <REVISION> --namespace "$NAMESPACE" --wait --timeout 10m
kubectl -n "$NAMESPACE" rollout status statefulset/anubiswatch --timeout=10m
scripts/production-smoke.sh https://anubiswatch.example.com
```

After rollback, capture the failed release revision, image digest, Helm values
diff, failing check output, and pod logs.

## Evidence To Record

- Git commit SHA and container image digest.
- Helm release revision and values file checksum.
- `kubectl get pods,svc,ingress` output.
- Smoke test command and result.
- Any skipped checks and why they were skipped.

## Known Gaps

- This repository does not contain live cluster credentials, production secret
  values, DNS ownership, or certificate issuer configuration.
- Built-in ACME/autocert support has been removed; certificate lifecycle must be
  owned by ingress/cert-manager or another platform-level TLS process.
- The runbook validates deployment health, not production traffic volume or
  long-term SLO conformance. Run load tests separately for capacity changes.
