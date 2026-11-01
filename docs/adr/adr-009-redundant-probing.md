# ADR-009: Redundant Probing (`necropolis.distribution.redundancy`)

## Status

Accepted (2026-10-31). Implemented: ownership sets, quorum consolidation on the
primary, vote records and the `hasoak -redundancy` check. Differences from the
original proposal are noted under "As built".

## Context

Every soul is probed by exactly one live member (`cluster.PlanOwnership`, run
by `clusterWork.reconcile`). That keeps probe load flat and makes alerting
simple, but it has two weaknesses:

- **One vantage point.** A network problem between the owner and the target
  looks like an outage of the target. The owner alerts, and nothing contradicts
  it.
- **Failover gap.** When the owner dies, its souls are unprobed until the next
  reconcile (up to 30 s) plus the first interval of the new owner.

`redundancy: N` is the obvious knob, and the config already carries it.

## Decision

A soul is probed by **N distinct live members** (its *probers*), and its
status is the **quorum** of their latest judgments. N = 1 behaves exactly as
today.

### 1. Ownership becomes an ordered set

`PlanOwnerSets` returns `soul ID -> [node IDs]`, primary first (`PlanOwnership` is the single-owner view of it). The primary is
what the plan returns today, so an N = 1 cluster and the first element of an
N > 1 plan are identical. The other N-1 probers are chosen under the same
strategy and the same `Soul.Regions` restriction:

- `hash`: the top N nodes by rendezvous weight. A membership change moves only
  the entries that involved the changed node.
- `round-robin` / `load-based` / `region-aware`: the primary as today, then the
  next nodes in the strategy's order, never the same node twice.
- Fewer eligible nodes than N: use all of them and log it once per soul.
  Redundancy degrades, it never blocks probing.

The plan stays a pure function of replicated inputs, so nodes still agree
without coordinating.

### 2. Every prober probes, staggered

Each prober schedules the soul at its own offset (`rank * interval / N`), so N
probers produce evenly spaced probes rather than N simultaneous ones. Probe
cost is N times higher: workspace quotas and the per-target SSRF policy apply
per probe as before; the docs must say the effective rate is `N / interval`.

### 3. Secondaries publish votes, the primary publishes judgments

Making every reader quorum-aware (API, status page, SLA, MCP, WebSocket,
metrics, uptime) would touch dozens of call sites. Instead only the primary
writes judgments:

- A secondary's scheduled result is a **vote**: one small replicated record
  per (soul, prober), `{workspace}/votes/{soul}/{prober}`, overwritten on every
  probe. It is not stored as a judgment, not pushed to clients and not
  evaluated by alert rules.
- The primary, when its own probe finishes, reads the other owners' votes
  and stores **one judgment** whose status is the quorum. Everything downstream
  keeps seeing a single judgment stream per soul.

### 4. Status is a quorum over the primary's result and fresh votes

A vote counts while it is younger than `2 * interval`; an older one means that
prober is silent and it is left out. With the primary's own result:

- a strict majority dead is `dead`;
- some dead (or degraded) short of a majority is `degraded`;
- otherwise the primary's own status stands.

With N = 2 a soul is `dead` only when both agree; one dissent is `degraded`.
That trades slower detection of a partial outage for far fewer false pages.
The judgment's message records the split ("1 of 2 probers report dead").
A later `distribution.quorum` setting (`majority` | `any` | `all`) can change
the trade without changing the mechanism.

### 5. Alerts run on the primary's consolidated judgment

Only the primary's judgments reach alert rules, so nothing changes in the rules
engine. Taking over as primary reseeds alert state from replicated judgments
(`seedAlertState`), as taking over a soul always did; a secondary that becomes
primary is treated the same way. `degraded` is only as alert-worthy as the
rules say; none of the default rules fire on it, so a split does not page.

### As built

- Probers are spread by a per-node start offset when several souls start
  together; a single new soul is first probed by all of its probers at once.
- Manual checks (`TriggerImmediate`) are not consolidated: they run on the
  node that received them and store what they find.
- Vote records are not deleted when a soul is deleted (at most N tiny records
  per soul).
- Historical aggregates (uptime percentage, SLA) read the stored judgment
  stream, which now carries the quorum status.

### Out of scope for the first cut

- **Heartbeat souls** are judged by their owner from replicated ping state;
  more probers add nothing. They stay single-owner.
- **Journeys** keep one runner; multi-step runs are not safely repeatable.
- `rebalance_interval` stays unused.

## Consequences

- N times the probe traffic and the outbound load on targets.
- Better resilience to one node's network trouble and a shorter failover gap
  (a surviving prober is already running).
- Status is no longer "the owner's last judgment": the dashboard, status pages,
  SLA reports and uptime calculations must read the consolidated status. This
  is the largest part of the work.
- The `hasoak` double-probe check must change from "no soul probed more than
  once per window" to "no soul probed more than N times".
- A cluster smaller than N silently runs at lower redundancy (logged).

## Verification

- Unit: owner sets (primary equals the single-owner plan, distinct, sized,
  deterministic, regions honoured, minimal movement under `hash`), quorum
  table, consolidator (vote vs primary, stale and missing votes, non-owner
  votes), engine hook.
- `hasoak -redundancy 2`: every soul must be probed by both probers in the
  quiet phase (no under-probed souls) and by no more than N, with zero lost
  acknowledged writes.

## Resolved questions

- `degraded` does not alert by default.
- One prober per region (true multi-region checks) is a follow-up that reuses
  this mechanism.
