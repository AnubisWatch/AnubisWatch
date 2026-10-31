# ADR-009: Redundant Probing (`necropolis.distribution.redundancy`)

## Status

Proposed (2026-10-31). Nothing here is implemented: `redundancy` is still
parsed and ignored, and the node warns at startup when it is above 1.

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

`PlanOwnership` returns `soul ID -> [node IDs]`, primary first. The primary is
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

### 3. Judgments already identify their prober

`Judgment.JackalID` exists and all judgments replicate with unique IDs, so no
storage change is needed. Time-series summaries keep folding every judgment.

### 4. Status is a quorum over the freshest judgment per prober

A prober's judgment counts while it is younger than `2 * interval`; an older
one means that prober is silent and it is dropped from the vote. The soul is:

- `dead` when a strict majority of voting probers report dead;
- `alive` when no prober reports dead;
- `degraded` otherwise (some but not a majority report dead). The per-prober
  detail is shown in the soul's judgment list.

With N = 2 this means a soul is `dead` only when both agree; one dissent is
`degraded`. That trades slower detection of a partial outage for far fewer
false pages, which is the point of the feature. A later `distribution.quorum`
setting (`majority` | `any` | `all`) can change the trade without changing the
mechanism.

### 5. Alerts evaluate the quorum, on the primary only

Rules see one consolidated stream per soul, evaluated on the live prober with
the lowest rank. The existing cooldown and streak replication
(`seedAlertState`) already lets a new evaluator continue where the old one
stopped, so a primary change does not re-notify. Non-primary probers record
judgments but never run rules.

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

## Plan

1. `PlanOwnership` returns ordered owner sets (N = 1 identical to today);
   property tests: determinism, distinctness, region restriction, minimal
   movement under `hash`.
2. Engine runs a soul at a rank offset; reconcile assigns by membership in the
   set. No behavior change at N = 1.
3. Consolidated status function with the freshness and quorum rules; one place
   all readers (API, status page, SLA, MCP, WebSocket) call.
4. Alerts evaluated on the lowest-rank live prober over consolidated status.
5. `hasoak -redundancy 2`: kill and stop probers, assert quorum behaviour, no
   lost judgments, at most N probes per window.
6. Docs; the startup warning is removed once step 4 lands.

## Open questions

- Should `degraded` raise an alert at all? Proposal: no, only a quorum `dead`.
- Should a soul's `regions` list be allowed to *require* one prober per region
  (true multi-region checks) rather than "any N"? Natural follow-up, same
  mechanism.
