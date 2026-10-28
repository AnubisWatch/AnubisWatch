# Binary and Docker installation

The release target is one standalone AnubisWatch process, installed as a binary
or container with one persistent data directory. Use `serve --single`. The
existing three-voter cluster is a separate opt-in deployment; scaling a
standalone container against the same volume does not provide HA.

## Binary installation

Download the archive for your platform from the release and verify its SHA256
against `checksums.txt`. The release publishes `.tar.gz` archives and a Windows
`.zip`; extract the archive before running the binary. For a source build, use
`make all` so the current dashboard is embedded; `make build` alone reuses the
assets already present in `internal/dashboard/dist`.

From the repository root:

```bash
mkdir -p data
chmod 700 data
cp configs/standalone.anubis.yaml anubis.yaml
chmod 600 anubis.yaml
read -r -s -p "Initial admin password: " ANUBIS_ADMIN_PASSWORD
printf "\n"
export ANUBIS_ADMIN_PASSWORD
./bin/anubis serve --single --config ./anubis.yaml
```

For a downloaded executable, replace `./bin/anubis` with its extracted path.
The example listens on `127.0.0.1:8080`, enables authentication and protects
metrics. Open `http://localhost:8080`; the initial administrator is
`admin@anubis.watch`. The explicit authenticated example requires a nonempty
`ANUBIS_ADMIN_PASSWORD`; validation rejects a missing value. The alternative
`anubis init`/`run.sh` path generates and prints an initial password when none
is supplied and writes it into its owner-only config. Keep credentials out of
logs and tickets. Changing the environment
password after initialization is not a password reset; use Settings or
`anubis admin reset-password` with the instance stopped.

`run.sh` is a foreground convenience launcher. It preserves an existing
`$ANUBIS_DATA_DIR/anubis.json`, initializes only a missing config, and stops no
other process. Its port default is 8443; use `./run.sh --port 8080` for 8080.
Use `ANUBIS_BINARY` to select an extracted binary. For a managed service,
supervise `anubis serve --single --config /absolute/path/anubis.yaml` directly.

The standalone example retains raw judgments for seven days. Other shipped
configs may use different retention, including the 90-day code default. Choose
retention and check cadence together: 1,000 monitors at 60 seconds generate
approximately 1.44 million results a day. The retained dataset must fit the
instance's tested memory and recovery budget.

## Docker installation

Set `ANUBIS_IMAGE` to the release's immutable image reference, preferably
`ghcr.io/anubiswatch/anubiswatch@sha256:...`, after replacing the example with
the real published digest. A locally built candidate can use its local tag.

```bash
export ANUBIS_IMAGE=ghcr.io/anubiswatch/anubiswatch:YOUR_RELEASE
# Set ANUBIS_ADMIN_PASSWORD through your secret manager or private environment.
docker compose up -d anubis
docker compose logs --tail 50 anubis
```

Compose requires `ANUBIS_ADMIN_PASSWORD`. Its default standalone service binds
`127.0.0.1:8080`, mounts the named volume `anubis-data` at `/data`, and uses a
read-only root filesystem with a temporary working directory. No cluster secret
is needed for this service. `ANUBIS_PORT` selects the published host port and
`ANUBIS_BIND_HOST` selects its host interface. Do not use `--scale anubis=...`
with this single-writer volume.

Equivalent direct container startup:

```bash
docker run -d --name anubis \
  --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v anubis-data:/data \
  -e ANUBIS_ADMIN_PASSWORD \
  --read-only --tmpfs /var/lib/anubis \
  "$ANUBIS_IMAGE" serve --single
```

The baked config uses `/etc/anubis/anubis.json`, data `/data` and internal HTTP
port 8080. To customize retention, allowed origins, TLS or target CIDRs, copy
`configs/container.anubis.json` into a private operator-owned config and mount
it read-only at that config path. Keep the config's server host `0.0.0.0` inside
the container so port publishing can reach it. An encryption key supplied as
`ANUBIS_ENCRYPTION_KEY` enables encryption; preserve the key separately from
the volume and restore it with the data. Never change it on an existing
encrypted volume as a substitute for a key-rotation procedure.

## Network access and authentication

The local HTTP examples assume a local operator or a TLS-terminating reverse
proxy. For remote browser access, configure the public origin in
`server.allowed_origins`, trust only the actual proxy CIDRs, and terminate TLS
before exposing credentials. Private-target SSRF protection is enabled by
default; set `security.allowed_networks` to explicit monitored CIDRs for LAN or
Docker targets. Keep certificate verification enabled.

Use cookie login or an approved existing session token for authenticated
metrics. Private/protected status pages disable shared caching. Workspace
resource isolation is enforced, but tenant quotas, white-label dashboard
branding and global escalation policies are not implemented. Escalation is
configured on individual rules. Advanced Journey headers/body/extractions can
be configured through YAML/JSON/API; the visual editor exposes a subset.
Status-page email subscription confirmation and delivery are not supported.

## Smoke and restart acceptance

```bash
curl -fsS http://127.0.0.1:8080/health
curl -fsS http://127.0.0.1:8080/ready
./bin/anubis version --json
```

For Docker, read the identity with `docker compose exec -T anubis anubis version
--json`. The authenticated `/metrics` build identity should match the binary
version, commit and build date. Authenticate, create a Soul, run a check, save
its ID, and restart the process/container. Verify the same Soul/history remains
and checks resume. `scripts/production-smoke.sh` accepts the base URL and
optional `ANUBIS_SMOKE_EMAIL` / `ANUBIS_SMOKE_PASSWORD` for authenticated checks.

For a built binary, `scripts/downloaded-binary-smoke.sh /absolute/path/anubis`
creates an isolated instance and cleans it up. `scripts/production-soak.sh`
provides a bounded isolated workload and encrypted restart check; review its
resource limits before use. Its one-second cadence is not a recommendation for
every production monitor.

## Backup and rollback

Stop the writer before offline backup or restore. Copy the entire stopped
data directory/volume, its selected config and required encryption keys;
preserve permissions. Store the backup and keys separately. A running-volume
`cp`/`rsync` is not a consistent backup. Keep a pre-upgrade backup with the
matching binary/image identity.

Restore into a separate empty data directory or volume. Start the matching
binary/image with the same key and verify login, inventory, sampled Judgment
IDs and resumed monitoring before switching traffic. Retain the original data
until the restore is accepted. The entity-backup CLI is useful for selected
exports; it is not interchangeable with full cold-volume recovery.

New writes use atomic WAL `BATCH` records, chunked snapshots and
`{workspace}/judgments/{soul}/{timestamp}/{judgmentID}` keys. New readers retain
legacy timestamp-only records. Older writers must not reopen a new-format data
directory: timestamp lookup/deduplication and older WAL readers are
incompatible. Roll back by restoring the pre-upgrade backup into empty storage
with its matching older binary. Reverting an image tag does not revert data.

Recovery objectives depend on backup frequency, data size and the host's disk.
Measure the intended workload and restore time before assigning RPO/RTO or
long-term capacity guarantees. Cluster quorum, Kubernetes, cross-region and
exactly-once notification guarantees are outside this standalone acceptance.
