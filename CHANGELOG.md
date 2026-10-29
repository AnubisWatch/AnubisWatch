# Changelog

All notable changes to AnubisWatch will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### HA soak can inject real network partitions

- `scripts/hasoak -partition` (root only) cuts a running node's Raft traffic
  in both directions with iptables while its HTTP port stays reachable; each
  node then runs as its own uid so its outbound loopback traffic can be matched.
  Rules are removed after every fault and on exit. Without root it refuses to
  start. HA stays labelled experimental until this has passed.

### Shared escalation policies work

- `verdicts.escalation` parsed but did nothing (startup only warned). A rule
  now uses a shared policy with `escalation: {name: <policy>}`; unknown
  names, unnamed or duplicate policies and policies without stages fail
  config validation.

### Restores refuse archives they cannot verify

- `anubis restore` refused a tampered signed archive but restored the same
  archive once its `signature` field was removed (checksums are unkeyed), and
  restored signed archives on a node without the backup key, with only a
  warning the CLI never printed. Both are now refused with
  `ErrUnauthenticatedBackup`; `--allow-unsigned` restores them deliberately
  (pre-signature archives, trusted sources). A wrong signature is always
  refused. Plain archives are also checked against their legacy
  `<archive>.key` sidecar, like encrypted ones.
- **Upgrade note:** restoring onto a fresh node needs the original node's
  backup key (`<data>/backup.key` or `ANUBIS_BACKUP_ENCRYPTION_KEY`).

### HA: restarted nodes rejoin in seconds

- Found by the HA soak: once a snapshot existed, every restart rewrote the
  node's whole store from it, deleting and re-writing each key with its own
  fsync — minutes for a real dataset, during which the node did not start
  its Raft transport. A node whose durable store already applied past the
  snapshot now skips the rewrite (its persisted `lastApplied` proves the
  state is there). Installing a snapshot sent by the leader, which must
  replace the store, now does it in batched frames.

### HA: log compaction no longer stalls the cluster for a minute

- Found by the HA soak: after a snapshot, compaction deleted every compacted
  log entry with its own fsync while holding the Raft mutex. With ~8000
  entries that took close to a minute, during which the leader sent no
  heartbeats and followers answered no AppendEntries; writes waited 35–70 s
  and the leader finally stepped down. Compacted entries are now deleted
  after the mutex is released, in batched tombstone frames (replayed exactly
  like DELETE entries, also by older versions).
- The size estimate for batched WAL frames now counts keys, the double
  base64 encoding and encryption overhead; a 5000-entry log append no longer
  exceeds the frame limit.

### HA: followers catch up 3–4× faster

- Found by the HA soak: a node restarted or paused under load drained its
  backlog at 12–22 log entries per second, because every log entry, every
  command of a replicated batch and every replicated judgment was its own
  fsynced WAL write. Linearizable reads on that node waited for it, so its
  API stayed slow for a minute or more.
- Appended log entries, runs of plain writes in a batch, and runs of
  replicated judgments (with their summaries) are now written as single WAL
  frames. Judgments and summaries end up exactly as when saved one by one;
  a replayed batch still counts nothing twice. Measured catch-up: 37–61
  entries per second on the same disk.
- The applied index is now persisted once per apply pass (at most every
  256 entries) instead of after every entry, which was a second fsync per
  entry. Found by a 2-hour soak: at ~2500 monitors a restarted follower
  stayed seconds behind the leader, and its API with it. After a crash the
  entries past the persisted index are applied again, which is safe: plain
  writes converge, command handlers are idempotent, and the shared
  authentication compare-and-set is guarded by a growing revision.
- Authentication on a follower waits only until it applied the last write
  to the shared auth state, not everything the leader committed. A node
  catching up on judgments after a restart answered every authenticated
  request in 5–9 s until it had caught up; it now answers at once. A leader
  running an older version ignores the narrower request and answers as
  before.

### HA: Raft snapshots are files, not in-memory records

- Raft snapshots were stored as CobaltDB records, so the in-memory tree (and
  its WAL) held a second full copy of the state machine. They now live in
  `<data>/raft-snapshots/` as framed files — encrypted per frame with the
  storage key when encryption is on, with frame order and completeness
  authenticated — and `meta.json` is replaced atomically once a snapshot is
  complete. A snapshot written by an older version stays readable and is
  removed by the first new snapshot.

### HA: authentication no longer writes to Raft on every request

- Found by the new HA soak (`scripts/hasoak`). In cluster mode every
  authenticated request ran its quorum barrier as a Raft write (a barrier
  key, fsynced on a quorum) while holding the authenticator's mutex, and
  re-imported the whole shared auth state. On a follower that was catching up
  after a pause, each barrier waited out its 5 s timeout and every other
  request queued behind it: the node's API, `/api/v1/cluster/status`
  included, answered after 20–30 s.
- Followers now get a linearizable read point from the leader with a new
  `read_index` forward (ReadIndex: the leader confirms its leadership and
  answers its commit index; nothing is appended), then wait to apply up to
  it. A leader too old to answer gets the old write barrier, so rolling
  upgrades keep working.
- Concurrent requests share one barrier, it runs outside the mutex, and the
  shared state is imported only when its revision changed.

### Dashboard branding is applied

- `dashboard.branding` parsed but did nothing (a startup warning said so).
  The title and logo now appear in the sidebar, on the login page and in the
  browser title, and `theme: dark|light` is the default for viewers who
  have not chosen one (`auto`, the default, keeps the dashboard's own). It is served publicly by `GET /api/v1/branding`. Config validation
  rejects a logo that is not an `https://` URL or a `/` path, an unknown
  theme, and titles over 64 characters.

### Private status pages accept API keys

- `X-API-Key` on a private status page was a stub that refused every key.
  It now accepts an `anb_` API key of the page's workspace; other values,
  session tokens included, are refused through that header.

### WAL checkpoints no longer stall writes

- A checkpoint held the write lock while it serialized and fsynced the whole
  live dataset, so every write — judgments included — waited for the full
  rewrite. The snapshot is now written without the lock; the lock is retaken
  only to carry over the writes made meanwhile (copied verbatim from the old
  log's tail) and to rename the new log into place. A checkpoint that races a
  rewrite or truncation is abandoned and retried later.

### Flap state after a longer outage

- A new runner looked for the soul's last judgment only two intervals plus a
  minute back, so after a longer outage a flapping soul lost its flag and its
  owed `flapping_stopped` notice. When that short lookup finds nothing and flap
  detection is on, the runner now looks back over the flap window.

### ICMP souls on Kubernetes

- Unprivileged ICMP uses ping sockets, which Linux allows only for groups in
  `net.ipv4.ping_group_range`. Docker opens that range by default, but many
  Kubernetes runtimes do not, so every ICMP soul was judged dead with a bare
  "permission denied". The Helm chart and `deploy/k8s` manifests now set the
  pod-scoped safe sysctl, and the judgment message names the sysctl.

### WebSocket per-user limit and duplicate session checks

- Concurrent handshakes of one user could exceed `maxConnsPerUser`: the slot
  was counted only once the connection was registered, after the client had
  already seen the upgrade succeed. The slot is now reserved before the
  upgrade.
- The dashboard checked the session (`/auth/me`) three times per page load
  (App, Header, Sidebar), spending the per-IP rate budget; concurrent checks
  now share one request.

### Dashboard fields the API never sent, and null dashboard widgets

- `POST`/`PUT /api/v1/dashboards` without a `widgets` array stored and
  returned `"widgets": null`, and the dashboard detail page crashed on it.
  Create and update now store `[]`, list and get serve `[]` for records
  stored with `null` (the stored record is not rewritten), the spec declares
  `widgets` a non-null array, and the dashboard client reads a `null` from an
  older server as `[]`.
- Maintenance windows always send `description` (`""` when unset) instead of
  omitting an empty one; the spec lists it as required.
- Status page list, get, create and update responses carry
  `subscriber_count`: the number of confirmed email subscribers, counted
  server-side in one scan (pending subscriptions are not counted, no address
  is returned, and only pages of the caller's workspace are served). The
  Status Pages view shows it per page and in total, and no longer sends the
  `subscribers` field it used to invent on save.
- The Judgments view showed a "Purity" score the backend has never had (it
  always read 0). The column is now the judgment's `status_code` (— when the
  check type has none), the "Avg Purity" card is a p95 latency from
  `duration`, and the CSV export's `purity` column is `status_code`.
- Soul detail rendered the interval as `{interval || weight}s` from a field
  the API does not send; interval and timeout now render as compact
  durations (`30s`, `1m`, `5m`, `1h 30m`, `500ms`).

### Dashboard API types generated from the OpenAPI spec

- `web/src/api/schema.generated.ts` is generated from
  `internal/api/openapi.yaml` by `openapi-typescript` 7.13.0 (pinned
  devDependency, offline). `pnpm run gen:api` regenerates it;
  `pnpm run check:api` fails when it is stale and runs in the `test-frontend`
  CI job (followed by a new `tsc --noEmit` step) and in `make dashboard`
  (`make dashboard-api-types` / `make dashboard-check-api` run them alone).
- The dashboard's API types (`web/src/api/client.ts`, `reports.ts`,
  `hooks.ts` and the Journeys, Incidents, Maintenance, Settings, Login and
  status-page incident views) now derive from the generated types; the
  presentation mappings (alive/dead to the UI status words, durations to
  seconds, rule and theme reshaping) keep their behaviour but are typed
  against the spec, so a renamed field or a new status fails `tsc`.
- OpenAPI fixes found on the way, each checked against the Go handlers:
  `Judgment` documented `latency_ms`/`error`, which the API never sent (it
  sends `duration` in nanoseconds, `message`, `status_code`, `jackal_id`,
  `region`, `details`, `tls_info`, and `embalmed` status); `Soul` lacked the
  tcp/udp/dns/smtp/imap/icmp/grpc/websocket/tls blocks, `last_check`,
  `latency` and `has_secrets`, and `SoulCreate` typed those blocks as empty
  objects; the journey list omitted `step_count`, `last_status`, `last_run`,
  `avg_duration` and `success_rate`; journey runs, incidents, workspaces,
  dashboards, maintenance windows, the paginated `/channels` and `/rules`
  lists and the stats `alerts` block had no response schema; alert channel
  `config` and status page `theme`/`groups` were untyped; `ClusterStatus`
  lacked `commit_index` and `User` lacked `api_key_id`. Response schemas now
  list `required` fields and `nullable` ones, and
  `TestOpenAPIResponseSchemasMatchGoTypes` checks both against the Go
  structs (a required field must never be omitted, and a required slice,
  map or pointer must be nullable unless the server guarantees a value).

### Confirmation retries and flap detection

- Souls accept `retries` (0–5, default 0) and `retry_interval` (default 5s,
  ≥ 100ms and shorter than the check interval). A failed check is re-run up
  to `retries` times before the soul is recorded dead; exactly one judgment
  is stored per scheduled check, with `details.attempts`. The concurrency
  slot is released while a retry waits, retries stop at the next scheduled
  check or on cancellation, and heartbeat souls and manual checks do not
  retry. `retries: 0` keeps the previous behaviour.
- Flap detection (`probe.flap_detection`, per-soul `flap_detection`; off by
  default, threshold 5 changes in 10m): a flapping soul's judgments carry
  `details.flapping`, soul payloads (REST, WebSocket, MCP) carry
  `"flapping": true` beside the unchanged `status`, and alert rules pause
  for it. Each covering rule sends one `flapping_started` and one
  `flapping_stopped` notice (new `kind` field on alert events, deduplicated
  separately from status alerts); PagerDuty/Opsgenie do not treat a
  "flapping started" notice as a recovery. Flapping also ends after
  `threshold` steady checks, and on stabilizing rules compare against the
  pre-flap status (`details.flap_baseline`), so a soul that went hard down
  while flagged still opens an incident and one that recovered resolves.
  New runners restore flap state from the latest stored judgment, so an
  interval change or owner move neither drops the flag nor the "stopped"
  notice. gRPC `Soul.retries` and
  Create/UpdateSoul `retries` are now wired.
- Dashboard: Retries, retry interval and flap settings under an "Advanced"
  disclosure on the soul create/edit forms; a "Flapping" badge on the soul
  list and detail page. CLI: `anubis watch --retries N --retry-interval D`.

### SLA reports and judgment export

- `GET /api/v1/reports/sla` (+ `.csv`): availability, failed checks,
  estimated downtime, incident count and MTTR, p50/p95/p99 latency and, with
  `target`, the error budget, per soul and per workspace over any period up
  to the retention horizon (at most 1,830 days). Whole hours and days are read
  from the time-series rollups, so 90-day and 1-year reports do not scan raw
  judgments. Maintenance windows (including recurrences) are excluded time.
  Requires `judgments:read`.
- `GET /api/v1/souls/:id/judgments/export?format=csv|ndjson`: streams a
  soul's judgments oldest first, an hour of storage at a time, clipped to
  raw retention and capped by `limit` (default 100,000, max 1,000,000; the
  `X-Export-Truncated` trailer says when more remained). Workspace-scoped,
  `judgments:read`. CSV text cells starting with `= + - @` are prefixed with
  `'` against formula injection; downloads carry `Content-Disposition`.
  Exports are limited to 2 per user / 8 per server (429), and a client that
  stops reading is cut off after 30 s idle.
- Report latency percentiles use a bounded log-linear histogram (±1%), so a
  long report cannot go quadratic; reports have a 60 s budget (503) and stop
  on client disconnect; the day hourly retention cuts through is no longer
  dropped (daily rollup minus surviving hours).
- CLI: `anubis report sla [--from --to --target --soul --format]` and
  `anubis export judgments --soul ID [--from --to --format --limit -o]`,
  using the `anubis login` session or `ANUBIS_API_TOKEN`.
- Dashboard: a Reports page ("Scrolls of Ma'at") with 7d/30d/90d/1y/custom
  periods (custom limited by retention), a per-soul SLA table, an SLO target
  with error budget, and CSV/NDJSON downloads. Soul uptime charts add 90-day
  and 1-year ranges (`/souls/:id/uptime?range=90d|1y`).

### Status page incidents and scheduled maintenance

- Dedicated, workspace-scoped endpoints (`settings:write`) to declare a status
  page incident (title, impact none/minor/major/critical, affected souls,
  status investigating/identified/monitoring/resolved), post timeline
  updates, resolve, edit and delete it, and to announce scheduled
  maintenance (start/end, affected souls, optional link to a one-off
  maintenance window whose schedule it follows). Writes are a locked
  read-modify-write of the page, so concurrent editors never lose an update;
  the whole-page PUT takes the same lock. `incidents` and `announcements`
  are read-only on page create/PUT (ignored; stored values kept), so that
  path can no longer bypass their validation. They replicate through the
  status-page save path.
- A page record is capped at 256 KiB serialized (well under the 1 MiB WAL
  entry limit after the Raft/WAL base64 wrapping; writes that would grow past
  it get 413), and every incident write drops resolved incidents and
  finished maintenance older than the 90-day public history.
- Each created incident, posted update and new announcement emails confirmed
  subscribers exactly once; metadata edits and deletes send nothing.
- The public page shows an active-incident banner coloured by impact,
  component status raised by incident impact (and "under maintenance" during
  announced work), upcoming/in-progress maintenance and a 90-day incident
  history with timelines. The JSON view adds `incident_history`,
  `maintenance` and `incident_impact`; the RSS feed lists page incidents and
  announcements. Text is plain text, escaped in HTML, JSON and RSS, with
  http(s) URLs linked (`rel="nofollow noopener noreferrer"`).
- Dashboard: an Incidents & maintenance panel per status page (list, create,
  post update, resolve, delete with confirmation, announcement editor, live
  preview link). Editing a page no longer re-sends its incidents.

### Heartbeat (push / cron) monitors

- New soul type `heartbeat`: the monitored job pings AnubisWatch instead of
  being probed, so a cron job, batch worker or backup that silently stops is
  detected. `weight` is the expected period (up to 31 days), `heartbeat.grace`
  how late a ping may be (1s-7d, default half the interval). No target.
- Public, token-authenticated `GET|POST /api/v1/heartbeat/{token}` (success),
  `/start` (measures run duration) and `/fail` (dead judgment; body or `?msg=`
  is the message, 1KB max, any content type). Plain-text `OK`; unknown tokens
  get 404; 30 pings per token per minute on top of the per-IP limit. Tokens
  are 256-bit, generated server-side, looked up by SHA-256 and compared in
  constant time, and redacted from request logs, traces and audit records.
- A ping records an `alive` judgment; a missed `weight + grace` deadline
  records `dead`, repeated every interval until a ping arrives, through the
  normal judgment path (storage, live updates, Ma'at alert rules and
  maintenance windows). The probe engine judges heartbeat souls with a
  checker that never dials.
- Cluster mode: any node accepts a ping and writes the soul's ping record
  (`{workspace}/heartbeats/{soul}`) through Raft; the owner, woken by the
  replicated write, is the only node that judges pings and deadlines.
- `GET /api/v1/souls/{id}/heartbeat` returns ping URLs (token only for
  editors and above) and the last ping; `POST .../heartbeat/rotate` replaces
  the token. Soul list/get responses return the token as `[REDACTED]`;
  updates keep it. Force check on a heartbeat soul answers 409.
- Type accepted by the config file (with an explicit `token`), REST, MCP
  `create_soul` (target now optional), gRPC (string type), `anubis watch
  --type heartbeat` and `anubis souls add|import` (token generated).
- Dashboard: Heartbeat type in the create/edit forms (interval + grace, no
  target); the soul page shows the ping URL with copy button, an example
  crontab line, token rotation and the last ping, result and run time.

### API keys for automation

- New `GET/POST /api/v1/api-keys` and `DELETE /api/v1/api-keys/{id}` issue,
  list and revoke workspace API keys (`anb_<id>_<secret>`). The plaintext is
  shown once; only its SHA-256 is stored (under `{workspace}/apikeys/`,
  replicated through Raft in cluster mode) and compared in constant time.
  Keys carry a role that may not rank above their creator's, an optional
  expiry of at most 366 days, a throttled `last_used_at`, and revocation.
- `Authorization: Bearer anb_...` authenticates REST, gRPC metadata,
  WebSocket, SSE and MCP; open streams close once the key is revoked. A key
  acts as `apikey:<id>`, so the audit log names it on every request. Keys
  cannot manage keys, change passwords or switch workspaces (403).
- RBAC: `admin` gains `apikeys:*` (owners have everything); `admin` and
  `editor` gain `judgments:read`, which gRPC judgment reads check and REST
  never required.
- CLI: `anubis apikey list|create|revoke`; `ANUBIS_API_TOKEN` accepts a key.
- Dashboard: Settings → API & Integrations has an API Keys panel (create with
  one-time reveal and copy, list, revoke with confirmation).

### Team members and invitations

- Local auth is multi-user. Accounts have an email (unique, case-insensitive),
  name, bcrypt password (same cost and policy), a role per workspace, a
  disabled flag and last sign-in. The configured admin becomes the owner of
  the default workspace and keeps its config/env password semantics,
  `anubis admin reset-password` and the persisted `admin_password_hash`.
  Brute-force lockout applies per account and client IP for every account.
- Existing `sessions.json` files migrate on load: identities, sessions and the
  admin record keep their shape (older binaries still read the admin), and
  the directory is added under new `accounts` / `invitations` keys.
- New endpoints (interactive sessions with `members:*` only; API keys get
  403): `GET|POST /api/v1/members`, `PATCH|DELETE /api/v1/members/{id}`,
  `DELETE /api/v1/users/{id}`, `GET|POST /api/v1/invitations`,
  `DELETE /api/v1/invitations/{id}`, and the public
  `POST /api/v1/auth/accept-invite`.
- Invitations: a single-use token, stored as SHA-256 and never logged,
  valid 7 days. With mail configured the `/accept-invite` link is emailed;
  otherwise the API returns it once for the admin to share. Admins can also
  create an account with an initial password, which must be changed at first
  sign-in (until then only `/auth/me` and `/auth/change-password` answer).
- Nobody can grant or manage a role above their own, and every workspace
  keeps an enabled owner. Disabling, deleting, removing or downgrading an
  account ends its sessions and live WebSocket/SSE streams at once, and its
  API keys stop authenticating while it lacks the key's role.
- OIDC and LDAP sign-ins are provisioned into the same persisted directory
  (first sign-in: viewer of the default workspace, as before); roles set in
  the members list are kept and applied on the next sign-in, and disabling
  takes effect on open sessions. An IdP account whose email belongs to a
  local account is refused rather than linked.
- HA: the directory is part of the shared authentication state, so every
  change commits through the same compare-and-swap as sessions. That state is
  one Raft entry capped at 192 KiB, so everything in it is capped so that all
  caps together still fit: 150 accounts, 25 pending invitations, 10 sessions
  per account and 200 in total (oldest evicted), 20 reset tokens (one per
  account, replaced), 64 replicated lockout sources.
- Security review fixes: account emails are ASCII-only and compared with
  ASCII case folding only (a Unicode look-alike such as `opſ@` could become
  the configured admin); reset links go to the account's stored address;
  without outgoing mail no reset token is issued at all (each grew the shared
  state for an unauthenticated caller); a refused member update changes
  nothing; invitations are void once the inviter loses the authority to issue
  them; existing accounts join another workspace only by accepting an
  invitation with their own password, and names are changed only by the user
  or someone with authority over all of its workspaces; `api` now ranks below
  `editor`, so an api member cannot mint editor keys; OIDC requires
  `email_verified: true` unless `auth.oidc.allow_unverified_email` is set;
  SSO first sign-ins count against `max_team_members`; gRPC
  `StreamJudgments`/`StreamVerdicts` re-check their credentials before each
  batch and every 30s and end with `Unauthenticated`/`PermissionDenied`;
  status page subscription limits key on the mailbox (case and `+tag`
  folded) and a page sends at most 100 confirmation emails an hour.
- `tenants.default_quotas.max_team_members` is enforced again (members plus
  pending invitations, at invite/create time) and reported by
  `GET /api/v1/quota` as `team_members`.
- Dashboard: Settings → Members (list, invite with copy-once link, create,
  role change, disable/enable, remove, pending invitations with revoke), an
  `/accept-invite` page, a forced password change screen, and the current
  user's name in Settings → Security.

### Outgoing mail: status page subscriptions and password reset

- New `mail:` section (host, port, username, password, from, tls
  `starttls|implicit|none`, timeout) and `server.public_url` for links; mail is
  off until `mail.host` is set, and then `server.public_url` is required.
  `mail.password` supports `${ENV}` expansion and is masked by
  `anubis config show`. Helm: `config.mail.*`, `config.server.publicURL`,
  `secrets.mailPassword` (key `mail-password`).
- New `internal/mail` package: SMTP transport, header-injection-safe
  multipart messages (Date, Message-ID, MIME) and an asynchronous retrying
  queue. The alert email channel now uses the same transport, so both dial
  through `probe.SafeDialContext`.
- Status page subscriptions work: double opt-in with a single-use, 48-hour,
  hashed confirmation token; confirm and unsubscribe links render a button
  (POST) so link scanners cannot act; RFC 8058 `List-Unsubscribe` one-click.
  Subscribe is rate limited per IP and per address and never reveals whether
  an address is subscribed. Confirmed subscribers are emailed when an alert
  incident for one of the page's souls opens, is acknowledged or resolves, and
  when an operator posts, updates or resolves a page incident; in a cluster
  only the leader sends. Without mail, `POST /status/subscribe` answers 503
  and pages hide the form (`subscriptions_available` in the page JSON). Only
  email subscriptions exist (webhook requests get 400). The slugs `subscribe`
  and `subscriptions` are reserved. Subscription writes replicate in cluster
  mode.
- Password reset: with mail configured, `POST /api/v1/auth/reset-password`
  emails a `<public_url>/reset-password?token=...` link (enumeration-safe,
  3 per address per hour), `/api/v1/auth/methods` reports `password_reset`,
  and the dashboard shows "Forgot password?" and a reset page. Without mail
  the response still points to `anubis admin reset-password`. Tokens are never
  logged.

### Workspace quotas are enforced

- With `tenants.enabled: true`, `tenants.default_quotas` now limits every
  workspace: `max_souls`, `max_journeys` and `max_alert_channels` refuse creates
  past the limit from REST, gRPC, MCP and config-file alert channels (one check
  in the shared storage adapters). Refusals name the quota and limit: HTTP 409
  `{"code":"quota_exceeded","quota":…,"limit":…}`, gRPC `RESOURCE_EXHAUSTED`,
  MCP error `-32002`. Updates are never refused for a count quota, so data over
  a lowered limit stays readable and editable.
- `check_interval_min` refuses a soul or journey created with, or changed to, a
  shorter interval; `retention_days` caps every time-series retention tier.
- Quotas are no longer defaulted to 100/20/10/…: 0 or unset means unlimited, and
  nothing is limited while tenants are disabled, so upgrading blocks nobody.
- `max_team_members` is enforced since member management exists (see Team
  members and invitations). The "tenants.* is not enforced yet" startup
  warning is gone.
- New `GET /api/v1/quota` reports the workspace's usage against its limits;
  Settings → Storage shows it, and the soul, journey and channel forms show a
  readable message when a create hits a quota.

### Storage and backup integrity

- Storage encryption keys shorter than 16 bytes are rejected (32+, e.g.
  `openssl rand -hex 32`, recommended); the key string is still used verbatim,
  so existing data stays readable. `ANUBIS_ALLOW_WEAK_ENCRYPTION_KEY=1` opens a
  store with a shorter key once for migration. An `encryption.keycheck` file in
  the data directory now makes startup fail on a wrong key, or on a missing key
  for an encrypted store, instead of mixing data under two keys.
- `/ready` reports not-ready after a WAL fsync failure (writes are refused until
  restart).
- WAL damage followed by valid entries now stops startup instead of being
  skipped (a skipped DELETE could resurrect a key). Set
  `ANUBIS_WAL_SKIP_CORRUPT=1` to skip it; a torn tail is still cut off silently.
- Backups carry an HMAC signature, so an edited archive is rejected. The
  generated backup key moves from `backups/.backup_key` to `<data dir>/backup.key`
  (migrated automatically) and is no longer copied beside each archive;
  `ANUBIS_BACKUP_ENCRYPTION_KEY` accepts 64-character hex. Older archives,
  `<archive>.key` sidecars and raw keys still restore; unsigned plain archives
  restore with an "unauthenticated" warning.

### Standalone binary and Docker completion

- Preserve distinct Judgment IDs at the same timestamp. New records use a
  timestamp/ID key suffix; legacy timestamp-only records remain readable and
  legacy same-ID rewrites stay in place. Replication deduplicates by ID. Older
  writers must not reopen new-format data directories; restore a pre-upgrade
  backup into empty storage to roll back.
- Private/protected status HTML, JSON, badges, widgets, feeds and password forms
  now use `private, no-store`. Private pages accept the normal login cookie,
  with legacy-cookie compatibility and canonical-cookie precedence. Password
  POST redirects remain on the same origin.
- Journey edit controls now follow viewer/editor/owner permissions correctly.
- CLI, Prometheus and tracing use the linked build version. Release image builds
  receive version, commit and a shared timestamp.
- Standalone Compose no longer needs a cluster secret for a disabled profile;
  the standalone service uses a read-only root and loopback port publishing.
  `ANUBIS_IMAGE` selects the release/candidate reference.
- The launcher preserves existing config and uses foreground `exec` instead of
  process-name-wide termination. Init credential files use owner-only access.
- Update the test-only undici dependency to 8.12.3 and retain the audit gate.
- Document supported standalone scope and cold-volume recovery in
  `docs/deployment/binary-and-docker.md`; optional HA/Kubernetes acceptance is
  separate from this release target.

### HA and production readiness

- **Journey persistence and ownership**: manual runs now return exhausted
  save/retry errors with the completed check result instead of reporting a
  successful save. A stopped or reassigned scheduled run whose saver finishes
  late no longer overwrites the replacement run's deduplication state. In
  cluster mode, manual runs wait for consensus and local application before
  acknowledging the record; scheduled runs retain asynchronous batching.
- **Alert ownership return**: a node reacquiring a Soul rebuilds its failure
  streak and status-duration clock from current replicated history, so an old
  ownership period cannot open an incident before the configured threshold.
  Notification cooldowns and channel rate limits are retained.
- **WAL format boundary (downgrade restriction)**: Judgment primary records,
  lookup indexes and live rollups now commit together in one checksummed
  `BATCH` frame and one fsync. Failed writes no longer acknowledge missing
  rollups or leave a primary record without its index. This binary reads
  legacy `PUT`/`DELETE` WAL records; older binaries do not understand `BATCH`
  and may silently discard those writes when recovering/checkpointing.
  Never open a newly written data directory with an older binary. Roll back
  using a pre-upgrade backup restored into a separate data directory.
- **HA shared authentication (opt-in, three-voter clusters)**: sessions, password
  changes, password-reset state, logout and lockouts now use a quorum-backed
  shared authority, so a user session is valid on every voter and revocation is
  cluster-wide. Failed or uncertain quorum commits never return successful
  credentials. Enabled by the chart when `statefulSet.replicas: 3` with
  verified peer mTLS and shared secrets; see
  `deploy/helm/anubiswatch/values-ha.example.yaml`.
- **Snapshot format boundary (downgrade restriction)**: large snapshots are now
  stored as immutable encrypted chunks with a small atomic manifest. New
  binaries read legacy snapshots, but **older binaries cannot read the new
  chunk-manifest format** — a Helm or binary rollback does not reverse the
  on-disk format. Before crossing this boundary, restore a pre-upgrade backup
  into a separate data directory; a Helm rollback alone is not a data rollback.
- **Credential rotation**: a global password change or logout is enforced on
  all voters (old passwords and revoked sessions are rejected everywhere
  immediately after the change commits). Local (non-cluster) installs keep
  single-node semantics.
- **Deployment settings required for HA**: explicit image reference (historical
  releases do not gain the shared-auth/Raft fixes from chart values alone),
  persistent storage per voter, autoscaling disabled, `Parallel` pod
  management, verified peer mTLS secret shared by all voters, and anti-affinity
  across workers. The chart rejects unsafe HA topologies and in-place
  standalone↔HA topology changes; config or managed-secret changes trigger a
  rollout. See `docs/deployment/production-runbook.md`.
- **CI**: the `load-tests` job now sets `ANUBIS_RUN_LOAD_TESTS=true` and asserts
  that the six `TestLoad_*` tests actually executed; previously the tests
  self-skipped and the job passed while running nothing.

### Documentation

- OpenAPI request bodies now match what the server reads. Souls take `http`
  (not `http_config`) and `weight`/`timeout` as duration strings, with no
  `interval`; rules take a `conditions` array (not `condition`) and require
  `channels`; journey steps put the method under `http.method`;
  `/auth/workspace` takes `workspace`. The 16 POST/PUT operations that had no
  documented body now have one, channel and soul types list every supported
  value, and the login response no longer claims to return a token (it is set
  as the `auth_token` cookie). A test now fails if a documented field is not a
  JSON field of the Go type the server decodes.

### Fixed

- Storage: the data directory is now locked (`<data>/LOCK`). Client commands
  that open storage directly (`judge` without a login, `backup`, `restore`,
  `souls import`, cluster commands) used to open it next to a running server;
  their startup rewrote `wal.log` and every later server write was lost on
  restart. They now refuse and say to use the API or stop the server.
- Storage: a WAL write that fails halfway (for example on a full disk) is
  cut off again, so it no longer hides every later write on restart. After a
  failed fsync the store refuses further writes until restarted.
- Storage: recovery skips a damaged entry in the middle of the WAL and
  continues with the next valid one, instead of discarding everything after
  it. The damaged original is kept as `wal.log.corrupt-<time>`. The data
  directory is fsynced after the WAL is replaced.
- CLI: commands that open storage directly now use the configured encryption
  key. On an encrypted store, `backup create` used to report no data and
  `restore` wrote plaintext the server could not read.
- Backup: backups (format 1.1) now contain every stored record — dashboards,
  maintenance windows, incidents, verdicts, status-page subscriptions — plus
  local user accounts and the admin password hash (not login sessions).
  `--include-history` also covers uptime summaries, journey runs and alert
  events, and restore now writes judgment history back. Format 1.0 backups
  still restore.
- Config: `storage.retention_days`, which `anubis init` writes, now sets raw
  judgment retention. It was ignored and every install kept 90 days.
- API: alert channels, alert rules, journeys and maintenance windows created
  without an `enabled` field are now enabled, as documented (souls already
  were). They used to be stored disabled and silently do nothing.
- API: updating an alert rule keeps its `created_at`; updating a workspace
  rejects a blank name and no longer lets the client set `deleted_at`.
- Acknowledging or resolving an incident that does not exist (or belongs to
  another workspace) returns 404 instead of 500, and is no longer logged as a
  server error.
- `anubis help` no longer shows internal tracking codes (K6/K7/K9) and names
  the config keys as they are written in the file (`necropolis.cluster_secret`,
  `auth.local.admin_password`) instead of the Go field names. The same codes
  are gone from the warnings the server logs.

## [0.2.1] - 2026-10-18

### Fixed

- Settings: the timezone list now offers every IANA zone the browser knows,
  plus "Viewer's local time" (the new default), and the chosen zone is applied
  to every date the dashboard shows. The binary embeds `time/tzdata`, so zone
  validation no longer depends on the host's zoneinfo.
- Cluster: a follower's peer list shows the real state of each node
  (follower/unreachable/unknown) instead of guessing.

### Removed

- The language setting and the "Digest Emails" entry. Neither was wired to
  anything; the dashboard is English only.

### Changed

- Container images are published to and documented for GHCR only
  (`ghcr.io/anubiswatch/anubiswatch`).
- Built with Go 1.27.2 (toolchain, CI and Dockerfile moved together);
  govulncheck v1.9.0 and golangci-lint v2.15.0 in CI.
- Dependency updates: Go minor/patch group, GitHub Actions, jsdom 30,
  @testing-library/jest-dom 7.

## [0.2.0] - 2026-10-17

This release makes AnubisWatch work end to end — single-node from `anubis init`
onward, the dashboard without data loss, and multi-node clustering — and turns
CI fully green on `main` for the first time (chaos, load and benchmark jobs
included). Details: PR #10.

### Upgrade notes

- Soul `weight`/`timeout` given as bare JSON numbers are nanoseconds; values
  below 1s / 1ms are now rejected. Use duration strings such as `"30s"`.
- Probes and alert channels refuse private/loopback targets unless
  `security.allow_private_targets` or `security.allowed_networks` allows them;
  a blocked soul now records a `dead` judgment explaining why.
- Cluster: the default distribution strategy is `hash`; with `cluster_secret`
  every Raft RPC is signed, so upgrade all nodes together and keep node clocks
  within 60s. `--join` sets `necropolis.raft.join` instead of deriving peer IDs.
- A password changed at runtime now survives restarts; changing the config
  password still takes effect. Recover a lost admin password with
  `anubis admin reset-password` (server stopped).

### Added

- `anubis login` / `logout` / `admin reset-password`; client commands read the
  server address and data directory from the config file.
- Multi-node: follower write forwarding, single-owner souls and journeys,
  replicated judgments/incidents/alert dedup and rate-limit state, signed Raft
  traffic, join by address, `anubis summon` / `banish`
  (`POST/DELETE /api/v1/cluster/nodes`).
- Dashboard: Telegram, OpsGenie, ntfy and SMS channels; IMAP souls; rule
  channel selection with auto-resolve; role-aware controls; SSO button;
  change-password form; degraded and maintenance states; working header search
  and incident bell.
- Live WebSocket events for incidents, alerts and soul changes;
  `GET /api/v1/auth/methods`.

### Fixed

- Dashboard edit screens no longer drop fields they do not show (souls, rules,
  status pages, journeys, channels); soul and journey lists load every page.
- MCP `tools/list`, `resources/list`, `prompts/list` returned empty bodies.
- Backups were empty on a fresh install; logging config was ignored;
  `anubis health` reported healthy with the server down; `config show` printed
  secrets; standalone API-created journeys never ran.
- Email alerts: missing From/Date headers, no implicit TLS, header injection.
- Settings saved to an in-memory map and were lost on restart.
- A data race in Raft log replication.

### Security

- Go 1.26.9 and updated `x/net`, `grpc` and OpenTelemetry dependencies
  (govulncheck and Trivy clean); `install.sh` verifies release checksums.

### Also in this release (accumulated since 0.1.4)

### Security

- The Trivy image gate ran with `ignore-unfixed: false`, so it also blocked on vulnerabilities with no published fix — `GO-2026-5932` (`x/crypto/openpgp`) and `CVE-2026-56852` (`x/text`), both transitive, both already at the newest published version of their module, and both reported by `govulncheck` as unreachable from this code ("affected by 0 vulnerabilities"). No change in this repository could have turned that job green. Now `ignore-unfixed: true`: the gate blocks on actionable findings (it is what caught the openssl CVE below), unfixed ones stay visible through the `always()` SARIF upload to the Security tab, and `govulncheck` remains a hard gate with real reachability analysis
- Base image now takes its pending security updates (`apk --no-cache upgrade`). `alpine:3.24` is pinned for reproducibility, but a pinned tag also freezes the package versions it shipped with: Trivy blocked the build on **CVE-2026-14456** (HIGH — OpenSSL QUIC unbounded memory growth) in `libcrypto3`/`libssl3` `3.5.7-r0`, fixed in `3.5.8-r0`. Image rescans clean; the Go binary itself had zero findings


- **`.dockerignore` excluded nothing on four pattern lines.** Docker does not strip a trailing `# comment` from a `.dockerignore` line, so `anubis.json         # local config with secrets` was parsed as a literal pattern matching no file. The root `anubis.json` — which carries a plaintext `auth.local.admin_password` — therefore reached the builder stage through `COPY . .`, along with `anubis`, `*.test`, and `.wrongstack/`. Comments moved onto their own lines; verified with a `COPY . /ctx` probe before and after
- `scripts/check-dockerignore.sh` had codified the same wrong assumption: its lint regex ended in `(#.*)?$`, explicitly accepting trailing comments, and it only inspected the **final** image — which in a multi-stage build can never observe a builder-stage leak. It now rejects inline comments and probes the build context directly
- Bumped `react-router-dom` 7.18.1 → 7.18.2 (GHSA-qwww-vcr4-c8h2, HIGH). The dashboard uses `BrowserRouter`, not the affected RSC mode, so it was not exploitable here

### Fixed

- `TestIntegration_JudgmentStorage` asserted through `ListJudgments(context.Background(), ...)`, which takes the workspace **only** from the context and so scanned `default` while the fixture wrote to `test-ws` — it read back 0 of 5 judgments. Rewritten to go through `ListJudgmentsNoCtx` (the path the REST API actually uses, which resolves the workspace from the soul) plus an explicitly scoped context, and extended to assert that a different workspace sees none of them. The production API was never affected; the test had simply never run in CI to say it was wrong
- The CI `build` job produced its Docker image without the version build args, so the image handed to `integration-tests` reported `dev`/`unknown`. Stamped like every other build path


- **`server.tls.min_version` was parsed but never read.** Both listeners hardcoded TLS 1.2, so an operator who set `min_version: 4` silently got TLS 1.2. `configs/anubis-prod.json`, the Helm chart's default `values.yaml`, and `values-production.example.yaml` all set it, and the chart templated it into the ConfigMap. It is now resolved through `TLSServerConfig.ResolveMinVersion()` and applied to the REST and gRPC listeners. Selectors `1` (TLS 1.0) and `2` (TLS 1.1) are rejected at config load rather than honoured, so the change can only raise the floor
- **Docker images reported `anubis version` = `dev`/`unknown`/`unknown`.** `Dockerfile` built with `-ldflags "-s -w"` and no `-X main.Version`, and `docker-build.yml` passed no `build-args` — so every published `ghcr.io/anubiswatch/anubiswatch:vX.Y.Z` was unidentifiable at runtime, while `SECURITY.md` asks vulnerability reporters for exactly that output. Added `VERSION`/`COMMIT`/`BUILD_DATE` build args, wired through `docker-build.yml` (both the scan candidate and the published image), and `make docker`
- **`anubis version --json` was documented but not implemented** — the flag was accepted and ignored, emitting the human-readable banner to anything trying to parse it
- Release binaries now stamp `main.Commit` and `main.BuildDate` as well as `main.Version`
- `RESTServer.Stop` closed `rateLimitStopCh` unconditionally, so a second call panicked with "close of closed channel". Guarded with `sync.Once`

### Changed

- **The API is described by one document again.** There were three, and they disagreed: a hand-maintained `docs/api/openapi.yaml` (18 paths) that was never served, a hardcoded `openapiJSON` blob in `rest.go` (42 paths, declaring an `info.version` of `4.0.0` that corresponded to nothing) that was, and a Go map merging a handful more paths in at request time. None covered the 93-route table. The spec now lives at `internal/api/openapi.yaml`, is embedded with `go:embed`, and is served as JSON at `/api/openapi.json`, as YAML at the new `/api/openapi.yaml`, and rendered by `/api/docs`. It documents every registered route — 17 had no entry in any of the three previous documents, including all of password reset, change-password, workspace switching, the OIDC endpoints, and every `/api/v1/alerts/{channels,rules}/{id}` operation
- `TestOpenAPISpecCoversEveryRoute` and `TestOpenAPISpecHasNoPhantomOperations` now fail the build if the router and the spec disagree in either direction, so the drift cannot silently return. Routes that are deliberately unpublished need an entry in `routesNotInSpec` with a reason
- `server.tls.prefer_server` removed from the shipped configs and Helm chart. It mapped onto `tls.Config.PreferServerCipherSuites`, which Go has ignored since 1.17. The Go field remains (deprecated, `omitempty`) so existing configs still load
- Standardised every frontend entry point on pnpm. `Makefile` (`dashboard-dev`), `web/package.json` (`build:embed`), and `scripts/test-e2e.sh` invoked bare `npm`, which has no `package-lock.json` to work from in this repo

### Removed

- Stale generated artifacts (~32 MB): `.tmp/anubis-e2e`, `.temp_files/`, root `coverage*.out`, `web/test-results/`, `web/.tmp-status/`, `web/.journeys-coverage/`, `web/vite.config.{js,d.ts}`
- `.github/RELEASE_v0.0.1.md` — superseded by this changelog and GitHub Releases

### Documentation

- `PRODUCTION_READINESS.md` (root) moved to `docs/`. The pre-existing `docs/PRODUCTION_READINESS.md` was a **different** document — the 2026-05-20 audit of v0.1.1, still listing three CRITICAL findings as open — and now lives at `docs/audits/2026-05-20-security-audit-v0.1.1.md` behind a "superseded" banner
- Fixed 8 broken relative links across `docs/CONFIGURATION.md`, `docs/INDEX.md`, and `docs/WEBSITE.md`; documented the `server.tls` block
- `CLAUDE.md`: corrected the CI job list (12 jobs; no separate `lint`/`security` job), the invalid `pnpm ci` command, stale dependency pins, and the route count; noted the two drifting OpenAPI documents
- `AGENTS.md`: the default port is 8443 in code — 8080 comes from `configs/anubis.yaml` and the container config, which the file stated as a flat fact


### Fixed (multi-node Raft was non-functional)

Re-enabling the six chaos tests in `internal/raft/chaos_test.go` — which carried unconditional `t.Skip("flaky …")` calls, making the CI `chaos-tests` job a green no-op — exposed that multi-node clustering had never actually worked. Five interlocking bugs, each masked by the skipped tests:

- **Inbound Raft RPCs were never wired to the node**: nothing called `TCPTransport.RegisterHandler`, and nothing produced into `Node.rpcCh`, so every incoming `AppendEntries`/`RequestVote`/`PreVote`/`InstallSnapshot`/`Heartbeat` was rejected with "unknown method". Followers could never hear from a leader or candidate. `Node.SetTransport` now registers a handler that feeds the node's RPC loop
- **`startElection` double-unlocked `n.mu`** on the election-win path (manual `Unlock` before `becomeLeader` plus a `defer Unlock`), crashing with `fatal error: sync: Unlock of unlocked RWMutex` the first time a real multi-node election was won. Lock discipline in the election path rewritten; vote RPCs no longer issued while holding the lock
- **`becomeLeader` called `sendHeartbeats` while holding `n.mu`**, which (after the data-race fix below) self-deadlocked the entire node — `LeaderID()`/`IsLeader()` callers then hung forever
- **Heartbeat requests were built off-lock** in per-peer goroutines, racing with `handleAppendEntriesResponse` writing `peer.matchIndex`/`nextIndex` (caught by `-race`). Requests are now built under `n.mu.RLock` before the send goroutines spawn
- **Leader anchored `AppendEntries` on `matchIndex` instead of `nextIndex-1`**: after any leader change (`nextIndex != matchIndex+1`) followers spliced entries at wrong log positions, truncated committed entries, and `processCommitted` span forever on the position/Index mismatch — pegging a core and making the node unresponsive. `PrevLogIndex` is now `nextIndex-1` per the Raft paper, and `processCommitted` advances by log position so a replication bug can no longer become an infinite spin
- **Pre-vote had no leader-stickiness check** (Raft §4.2.3): any follower whose election timer fired could depose a healthy leader, causing perpetual leadership churn — the original "flakiness" that motivated skipping the tests. `handlePreVote` now denies pre-votes while a live leader is known; `startElection` skips campaigning when leader contact is fresh

### Changed

- Chaos tests are gated behind `ANUBIS_RUN_CHAOS_TESTS=1` instead of unconditional skips; the CI job sets the variable, runs them under `-race`, and asserts at least 6 tests actually passed so the job can never silently regress to a no-op. Test assertions rewritten to wait for convergence (stable single leader, equal terms) instead of sampling instants; full suite passes 10/10 consecutive runs under `-race`

### Fixed (business-critical)

End-to-end test of the actual uptime-monitoring workflow (target up → down → up) exposed four interlocking bugs in the alert → incident pipeline. CI was green, audit was green, docs claimed it worked — but no incident had ever actually been created from a failure event. Each bug masked the next:

- **`consecutive_failures` condition was a no-op**: `internal/alert/manager.go` `checkConditions` had no case for the condition type referenced in every doc; rules using it silently never fired. Now tracks a per-(rule, soul) `failureStreak` counter that increments on each dead judgment and resets on any non-dead status (bounded at 1M to prevent overflow on permanently-down souls)
- **`scope.type=""` rejected all souls**: `ruleApplies` switched on `scope.Type` and returned false for the default case. The natural API client shape (`{"scope":{"soul_ids":[...]}}`) sets `soul_ids` without `Type`, so every such rule was silently skipped. Now infers type from which sub-field is populated; empty scope defaults to "all"
- **Incident records were never created**: alert events queued, channels dispatched notifications, but no code path persisted an `Incident`. `/api/v1/incidents` always returned empty regardless of rule firings. `recordIncident` now opens an Incident on first trigger and appends events on subsequent triggers for the same (rule, soul); `autoResolveOpenIncident` closes it on recovery when `rule.AutoResolve` is set
- **`/api/v1/incidents` hid resolved history**: `handleListIncidents` called `ListActiveIncidents()` which filtered out resolved entries — recovered incidents disappeared from the UI. Default now returns every incident; the previous behaviour is reachable via `?status=active`

### Tests

- `TestManager_ConsecutiveFailures_OpensAndAutoResolvesIncident` — full pipeline regression: 2 failures don't trigger, 3rd opens an incident, 4th appends to the same incident, recovery auto-resolves it, and a new failure streak opens a separate incident
- `TestManager_RuleApplies_InfersScopeFromSubfields` (5 sub-cases) — scope inference matrix
- Replaces the hollow `TestManager_ProcessJudgment` whose comment literally said "may or may not trigger depending on condition logic … just verify the method doesn't panic"

## [0.1.4] - 2026-05-22

### Security

- **CRITICAL-2 follow-up** (`internal/core/config.go`): `validate()` now rejects `tls.enabled: false` when `environment` is `"production"`. Operators who terminate TLS at a reverse proxy can use a different environment label (`"staging"`, `"behind-lb"`, etc.) or omit the field. Closes the remaining hardening item from the production-readiness audit
- **MEDIUM-11** (`internal/api/rest.go`): `/api/docs` now emits a scoped Content-Security-Policy that allows `cdn.jsdelivr.net` for Swagger UI scripts/styles/fonts and permits the required inline initialiser. The rest of the application keeps the strict `default-src 'self'` from `securityHeadersMiddleware`
- **MEDIUM-13** (`internal/auth/local.go`): brute-force lockout state is now persisted into the session file. After a restart, the lockout deadline still applies — an attacker can no longer bypass the 15-minute lockout by triggering a process restart. Stale entries (lockout expired AND last attempt outside the reset window) are dropped on load so the file doesn't grow unbounded

### Cleanup ("fazla ise kes at")

- Removed stale dashboard source at `internal/dashboard/` (16 files: old JSX, `package.json`, `pnpm-lock.yaml`, `vite.config.js`, etc.). The canonical dashboard moved to `web/` long ago; only `embed.go`/`embed_test.go`/`dist/` were still in use here. CI `gofmt` filter also dropped the now-pointless `internal/dashboard/node_modules` exclusion
- Removed empty `data/` and orphan root `anubis` binary (both gitignored, no longer needed locally)

### Tests

- `TestLocalAuthenticator_LockoutPersistence` — confirms a triggered lockout survives `NewLocalAuthenticator` restart over the same session file
- `TestLocalAuthenticator_LockoutPersistence_ExpiredDropped` — confirms fully-expired entries are dropped on load
- `TestLocalAuthenticator_LockoutPersistence_ClearedOnSuccess` — confirms successful login clears the failure record from disk
- `TestValidate_ProductionRequiresTLS` (8 sub-cases) — confirms environment + TLS interplay (production with/without TLS, casing, whitespace, non-production environments unaffected)
- `TestHandleOpenAPIDocs` extended to assert the scoped CSP override contains the required directives for Swagger UI's CDN assets

## [0.1.3] - 2026-05-21

### CI / Build

- **Frontend toolchain**: all GitHub Actions jobs (test-frontend, e2e-dashboard, build, release) switched from `npm ci` to `pnpm install --frozen-lockfile`. The frontend dependency upgrade had regenerated `pnpm-lock.yaml` but left `package-lock.json` stale by 16+ packages, so every job that used `npm ci` was failing
- **Dockerfile**: `npm ci` replaced with `pnpm install --frozen-lockfile` (pnpm installed via `npm install -g pnpm@10` since Alpine's nodejs does not bundle corepack)
- **Helm tests**: removed lint/template/guardrail references to `deployments/charts/anubiswatch/` — that chart was deleted in commit e7b82e2 ("clean: remove duplicate dirs...") but the CI workflow still referenced it
- **Orphan lockfile**: `web/package-lock.json` deleted (project uses pnpm)

### Fixed

- **gofmt drift**: eleven files (mostly tests + `core/config.go`, `core/feather.go`, `api/rest.go`, `journey/executor.go`, `telemetry/tracer.go`, `grpcapi/server.go`) had drifted struct field alignment. Go 1.26.3 (CI) is stricter than 1.26.0 (local), so the drift only surfaced in CI's Lint job; `gofmt -w` applied across the tree
- **ESLint**: `eslint-plugin-react-hooks` v7's new strict rules (`set-state-in-effect`, `immutability`, `purity`, `use-memo`) flagged canonical React patterns (fetch-on-mount, derived `Date.now()`, callbacks referencing each other via refs/timers) as errors; these are disabled with rationale documented in `web/eslint.config.js`
- **ErrorBoundary test**: dropped three unused imports/variables (`Component`, `ReactNode`, `err`) that tripped `@typescript-eslint/no-unused-vars`
- **Playwright e2e**: smoke spec headings re-synced with the Egyptian-themed dashboard rename (Souls → Essence, Judgments → Weighings, Alerts → Divine Warnings, Incidents → Cries of Chaos, Maintenance → Sacred Rest, Journeys → Voyages, Cluster → Necropolis, Status Pages → Temple Squares, Settings → Pharaoh's Chamber); the suite went from 1 failure cascading into 7 skipped tests back to 8/8 passing
- **Playwright e2e (CI flakiness)**: `submitSoulEditProtocolPayload` now waits for the `/api/v1/souls/{id}` GET response before asserting on the "Edit Soul" heading, so the 30s deadline doesn't include cold-cache asset load + auth round-trip on slow CI hosts

## [0.1.2] - 2026-05-21

### Security

- **CRITICAL**: SSRF protection — removed `grpc`, `tcp`, `udp` schemes from allowed URL schemes; only `http`, `https`, `ws`, `wss` are now permitted
- **CRITICAL**: LDAP authentication — local fallback now gated on `isConnectionFailure()`; anonymous LDAP bind can no longer silently fall back to local admin account
- **CRITICAL**: Password reset — token prefix removed from structured log output
- **HIGH**: LDAP filter injection — `ldap.EscapeFilter()` now applied in default filter case (empty `UserFilter`)
- **HIGH**: OIDC issuer validation — issuer claim normalized on both provider response and config sides (trailing-slash mismatch fixed)
- **MEDIUM**: REST TLS — explicit `tls.Config{MinVersion: TLS 1.2, PreferServerCipherSuites: true}` on `ListenAndServeTLS`
- **MEDIUM**: HSTS header — `Strict-Transport-Security` now sent only when TLS is enabled
- **MEDIUM**: X-Forwarded-For spoofing — added `TrustedProxies` config; `realIP()` function respects known proxy IPs; XFF is ignored without trusted proxy configured
- **MEDIUM**: Status page deletion — slug and domain index deletion errors now logged instead of silently ignored
- **MEDIUM**: TimeSeries compaction — `stopMu sync.Mutex` added to protect `stopCh` channel from race conditions
- **Configuration**: `api_keys.enabled` set to `false` in container config (no validation implementation)

### Added

- `server.trusted_proxies` config field for X-Forwarded-For validation
- `server.tls.min_version` and `server.tls.prefer_server` fields for TLS cipher control
- `TestSSRFValidator_ValidateTarget_BlockedSchemes` covering disallowed schemes
- `TestIsConnectionFailure` with 12 test cases for LDAP connection vs auth error discrimination

### Helm

- `values.yaml`: added `tls.cert`, `tls.key`, `tls.min_version`, `tls.prefer_server`, `trustedProxies` fields
- `values-production.example.yaml`: documented TLS and `trustedProxies` production configuration
- `templates/configmap.yaml`: wired new TLS and `trustedProxies` fields into `anubis.yaml` configmap

## [0.1.1] - 2026-04-12

### Added
- Comprehensive configuration validation for all config types
- Soul configuration validation with protocol-specific checks
- Channel configuration validation (webhook, slack, telegram, email, etc.)
- Alert rule validation with condition type checking
- Server, storage, auth, and logging config validation

### Changed
- Refactored `cmd/anubis/main.go` into smaller command-group files (`backup.go`, `cluster.go`, `judge.go`, `soul.go`, `system.go`, `util.go`)
- Moved server-specific adapters and helpers from `main.go` into `server.go`
- Enhanced `validate()` to automatically call `setDefaults()` before validation

### Fixed
- Auth config `setDefaults` no longer overrides an explicit `enabled: false` in config files (`AuthConfig.Enabled` changed to `*bool`)

## [0.1.0] - 2026-04-06

### Added
- MCP server integration at `/api/v1/mcp` endpoint for AI agent integration
- 8 built-in MCP tools: list_souls, get_soul, force_check, get_judgments, list_incidents, get_stats, acknowledge_incident, create_soul
- 3 MCP resources: getting-started, api-reference, status/current
- 3 MCP prompts: analyze-soul, incident-summary, create-monitor-guide
- Duat Journey executor for multi-step synthetic monitoring
- Variable extraction from HTTP responses (JSON path, regex, headers, cookies)
- Status page generator with HTML/JSON serving
- Status page custom domain support
- Status page password protection (protected visibility)
- Status page custom themes
- Status page RSS feed support
- Status page SVG badge generation for embedding
- Workspace-based multi-tenancy with namespace isolation
- RBAC with 5 roles: Owner, Admin, Editor, Viewer, API
- Quota management per workspace
- API pagination for all list endpoints
- API rate limiting (100 requests/minute per IP)
- Request validation middleware
- Alert deduplication with configurable cooldown
- Alert escalation policies with multi-stage escalation
- Alert acknowledgment workflow
- Circuit breaker pattern for probe engine (per-soul failure tracking)
- Concurrency limiting for probe checks (default: 100 concurrent)
- Region-based probe filtering
- Health check endpoint at `/health`
- Workspace context middleware for multi-tenant operations

### Changed
- Updated Go version to 1.26
- Updated CI/CD pipelines with security scanning (gosec)
- Improved Raft test coverage from 71.7% to 86.0%
- Dashboard build made optional in Dockerfile

### Documentation
- API.md with complete REST API reference
- TROUBLESHOOTING.md with deployment troubleshooting guide
- docs/adr/ with 8 Architecture Decision Records
- Updated CONTRIBUTING.md with current project structure

### Fixed
- REST server test compatibility with MCP server integration
- Status page uptime calculation
- Soul status tracking
- WebSocket console errors (disabled in favor of REST polling)
- MDNS mutex protection for conn field
- Various lint issues across packages
- Release workflow artifact handling

## [0.0.1] - 2026-04-04

### Added
- Initial release of AnubisWatch
- 10 protocol checkers: HTTP/HTTPS, TCP, UDP, DNS, ICMP, SMTP, IMAP, gRPC, WebSocket, TLS
- Embedded B+Tree storage (CobaltDB) with WAL and MVCC
- Raft consensus for distributed clustering
- Probe engine with adaptive intervals
- Alert engine with compound conditions and rate limiting
- REST API, WebSocket, and gRPC interfaces
- React 19 + Tailwind 4.1 dashboard
- Single binary deployment with zero dependencies
- Multi-tenancy support with workspaces and role-based access control
- Public status pages with custom domains, password protection, and uptime history
- 9 alert notification channels (Slack, Discord, Telegram, Email, PagerDuty, OpsGenie, SMS, Ntfy, Webhook)
- MCP (Model Context Protocol) server for AI integration
- Time-series storage with automatic downsampling
- Dashboard embedding in single binary (React 19 + Tailwind 4.1)
- Status page REST API endpoints
- Kubernetes Helm chart for deployment
- Docker and docker-compose support (single-node and 3-node cluster)
- Installation script for easy Linux/macOS setup
- systemd service file for production deployments
- Homebrew formula for macOS/Linux
- GHCR-exclusive container images (multi-arch: amd64, arm64, arm/v7)

### Documentation
- README.md with features, quick start, and comparison table
- BRANDING.md with complete brand guidelines
- CONFIGURATION.md with full configuration reference
- DEPLOYMENT.md with deployment guides
- GHCR.md with container registry documentation
- openapi.yaml with OpenAPI 3.1.0 specification
- WEBSITE.md with anubis.watch landing page content
- CONTRIBUTING.md with contribution guidelines
- INDEX.md with documentation index
- RELEASE_TEMPLATE.md for GitHub Releases
- MARKETING.md with launch materials

---

[0.1.1]: https://github.com/AnubisWatch/anubiswatch/releases/tag/v0.1.1
[0.1.0]: https://github.com/AnubisWatch/anubiswatch/releases/tag/v0.1.0
[0.0.1]: https://github.com/AnubisWatch/anubiswatch/releases/tag/v0.0.1
