# Reliable failover

Kubegres promotes a Replica to Primary when the Primary stops being ready. By default that
choice is made on Kubernetes readiness alone: the first ready Replica, in instance-index order,
wins.

Readiness means "this Pod is accepting connections". It says nothing about whether that Pod's
replication stream is intact, or how far behind the failed Primary it is. A Replica whose WAL
receiver died on `requested WAL segment ... has already been removed` stays ready indefinitely,
frozen at whatever it last replayed. Promoting it silently discards every transaction the
Primary committed after that point.

This page describes the opt-in gates that close those gaps, and how to operate them.

## The gates

All three default to off. A Kubegres resource written before these fields existed behaves
exactly as it did.

| Field | What it does |
| --- | --- |
| `spec.failover.intelligentFailover` | Promotes on PostgreSQL replication state instead of readiness |
| `spec.failover.primaryStabilityWindow` | Debounces the failover trigger |
| `spec.failover.minHealthyReplicas` | Holds a failover open until redundancy is restored |

### WAL-aware selection

```yaml
apiVersion: kubegres.reactive-tech.io/v1
kind: Kubegres
metadata:
  name: my-postgres
spec:
  replicas: 3
  image: postgres:16.1

  failover:
    intelligentFailover:
      enabled: true
      maxReplicationLag: 16Mi
      healthCheckTimeout: 5s
      fallbackToLegacy: true

  replicationSlots:
    enabled: true
```

When a failover starts, the operator queries every eligible Replica concurrently for its
recovery state, timeline and WAL positions, then applies these rules in order:

1. **Structural eligibility.** The Replica must be ready and its replication-slot configuration
   must match the cluster's — the check Kubegres has always applied.
2. **Must be a standby.** An instance that has left recovery has already been promoted and has
   forked its own timeline. Promoting it again would make that fork authoritative.
3. **Must have WAL.** An instance that has neither replayed nor received anything is excluded.
4. **Timeline grouping.** Every promotion forks a new PostgreSQL timeline, so after an earlier
   emergency failover the surviving Replicas can sit on divergent histories. Candidates below
   the highest observed timeline are discarded. **This is why the rule is not simply "highest
   LSN":** a Replica numerically ahead on an abandoned lineage carries data the cluster has
   already agreed to discard, and promoting it is a worse outcome than readiness-only selection.
5. **Furthest advanced wins.** Within the surviving group, the highest `GREATEST(receive,
   replay)` position wins — promotion replays whatever WAL is already on disk, so the received
   position is what predicts where the instance ends up. Ties break on the lower instance index
   so that repeated elections agree.
6. **Lag ceiling.** If the winner trails the last known Primary position by more than
   `maxReplicationLag`, nothing is promoted and the cluster reports that it needs manual
   intervention.

#### Where the lag reference point comes from

While the cluster is healthy, the operator records `pg_current_wal_lsn()` from the Primary into
`status.failOver.lastKnownPrimaryWalLsn` (at most once every 30 seconds). That is the reference
point lag is measured against once the Primary is gone and can no longer be asked.

If no such observation survives — a fresh operator that has not yet seen this cluster healthy —
lag is measured against the furthest-advanced candidate instead. That still bounds divergence
*between* Replicas, but it cannot detect that they are all equally far behind the Primary that
died. This degradation is deliberate: refusing every failover after an operator restart would be
worse than promoting the best available candidate.

#### When the operator cannot reach the Replicas

`fallbackToLegacy` states the availability-versus-durability preference the operator cannot make
on its own. It only applies when **no** candidate could be queried at all — typically a network
partition between the operator and the databases.

* `true` (default): fall back to readiness-based selection. The cluster recovers, at the risk of
  promoting a Replica that is behind. A `FailOverWalVerificationUnavailable` event is emitted.
* `false`: refuse to promote. The cluster stays down until a human intervenes, and
  `status.failOver.blockedReason` is set to `unreachable`.

If even one candidate answers, the election runs on the candidates that did answer; the
unreachable ones are simply not promotable.

#### Other options

* `requireStreamingReplica` (default `false`) rejects candidates without a live WAL stream. Leave
  it off for ordinary failover: once the Primary has gone, *every* surviving Replica has lost its
  stream, so requiring one would block every failover. It is useful when Replicas are expected to
  keep streaming from a Primary that is unhealthy but still alive.
* `allowUnsafeManualPromotion` (default `false`) lets `spec.failover.promotePod` bypass the
  health checks. See below.

### Manual promotion

`spec.failover.promotePod` names a Pod to promote. It now goes through the same safety checks as
an automatic election:

* The requested Replica's replication-slot configuration must match the cluster's. **This check
  applies whether or not `intelligentFailover` is enabled** and is the one behaviour change that
  is not behind a flag. It closes a real gap: with replication slots enabled, Kubegres creates
  the new generation of Replicas before deleting the old one, and promoting a pre-rotation
  Replica loses everything written through the new slots. The automatic path has always rejected
  these; the manual path checked nothing at all.
* With `intelligentFailover` enabled, the requested Replica is also checked for recovery state,
  WAL position and lag. Set `allowUnsafeManualPromotion: true` to promote it regardless.

A manual promotion is never delayed by `primaryStabilityWindow` — the window exists to filter out
failovers triggered by a readiness blip, and an explicit request is not a blip.

### Primary stability window

```yaml
spec:
  failover:
    primaryStabilityWindow: 30s
```

Failover otherwise triggers the instant readiness flips, with no debounce beyond whatever the
readiness probe's own `failureThreshold` provides. A misconfigured probe, a brief restart or
momentary resource pressure all look identical to a dead Primary — and an unnecessary failover is
not merely churn, it deletes a Primary that would have recovered and opens a window in which
committed transactions can be lost.

With a window set, the Primary must be continuously unhealthy for that long before promotion
starts. `status.failOver.primaryUnhealthySinceEpochInSeconds` holds the start of the current
unhealthy stretch and is cleared as soon as the Primary recovers, so a blip never accumulates
towards a later, unrelated one. It lives in the resource's status rather than in operator memory
so that it survives an operator restart.

Pick a window longer than a probe-driven blip but well short of your recovery objective; `30s`
is a reasonable starting point. This applies **on top of**, not instead of, the readiness probe's
own thresholds.

### Minimum healthy replicas

```yaml
spec:
  failover:
    minHealthyReplicas: 1
```

Nothing otherwise requires a healthy Replica to exist before a promoted Primary is treated as
fully operational. A cluster can come out of a failover as a single unreplicated node, and if
that node fails again there is no candidate to fail over to at all.

Setting this holds the failover blocking operation open — which also keeps the other enforcers
out of the way — until that many Replicas are ready again.

**The failover operation still times out after 300 seconds.** If the Replicas cannot be rebuilt
in that window, the cluster reports a failover time-out and most Kubegres features stay disabled
until it is fixed manually. Only set this if your Replicas reliably rebuild inside that window;
without replication slots a recreated Replica must take a full basebackup and often will not.

Whatever this is set to, Kubegres emits a `FailOverReducedDurability` **warning event** when a
failover completes with no ready Replica, so the window is always visible.

## Metrics

Exposed on the manager's metrics endpoint (`--metrics-bind-address`, `:8080` by default).

| Metric | Type | Meaning |
| --- | --- | --- |
| `kubegres_cluster_failover_ready` | Gauge | **1** if a Replica could be safely promoted right now; **0** if a failover would block. Only populated when `intelligentFailover` is enabled. |
| `kubegres_replica_wal_lag_bytes` | Gauge | WAL bytes by which each Replica trails the last known Primary position. |
| `kubegres_failover_decision_total` | Counter | Promotions, by `reason`: `highest_lsn`, `fallback`, `legacy`, `manual`. |
| `kubegres_failover_blocked_total` | Counter | Refusals to promote, by `reason`: `lag_exceeded`, `stale_timeline`, `no_healthy_candidate`, `unreachable`, `unsafe_manual_promotion`. |
| `kubegres_failover_candidates` | Gauge | Replicas eligible at the last election. Zero explains why a failover could not happen. |
| `kubegres_failover_duration_seconds` | Histogram | Start of failover to the promoted Primary being ready. |
| `kubegres_replica_query_errors_total` | Counter | Failures to read a Replica's replication state. |

### Alerts worth having

```promql
# A failover would not succeed right now. This is the signal that matters:
# it fires while the cluster is still healthy, not after it has already failed.
min by (namespace, cluster) (kubegres_cluster_failover_ready) == 0

# Kubegres refused to promote and the cluster needs a human.
increase(kubegres_failover_blocked_total[15m]) > 0

# A Replica is drifting towards the lag ceiling.
kubegres_replica_wal_lag_bytes > 8 * 1024 * 1024

# The operator is losing visibility of the Replicas.
rate(kubegres_replica_query_errors_total[10m]) > 0
```

`reason="fallback"` on `kubegres_failover_decision_total` is worth watching too: it means a
promotion happened without verification and the new Primary may be behind.

## Status fields

`status.failOver` records what the operator knows:

| Field | Meaning |
| --- | --- |
| `primaryUnhealthySinceEpochInSeconds` | Start of the current unhealthy stretch; 0 while healthy |
| `lastKnownPrimaryWalLsn` | Reference point for lag, in PostgreSQL's `X/Y` notation |
| `lastKnownPrimaryWalLsnEpochInSeconds` | When that was observed |
| `lastPromotedPod` | Pod promoted by the most recent failover |
| `lastPromotionReason` | `highest_lsn`, `fallback`, `legacy` or `manual` |
| `lastPromotionEpochInSeconds` | When that promotion started |
| `blockedReason` | Set when promotion was refused; cleared on the next successful promotion |

## Recovering from a blocked failover

When `blockedReason` is set, Kubegres has decided that every candidate would lose more data than
you said was acceptable. It will keep refusing until something changes. Your options:

1. **Wait.** If the Replicas are still streaming from a Primary that is unhealthy but alive, they
   may catch up on their own and the next reconciliation will promote normally.
2. **Promote explicitly.** Set `spec.failover.promotePod` to the Pod you have decided to accept,
   plus `spec.failover.intelligentFailover.allowUnsafeManualPromotion: true` if it fails the
   checks. Read `kubegres_replica_wal_lag_bytes` and the `FailOverBlocked` event first: they tell
   you how much history you are choosing to discard.
3. **Raise the ceiling.** Increase `maxReplicationLag`, or set it to `0` to accept any candidate.

`blockedReason: unreachable` is different — it means the operator cannot see the databases at
all. Fix the connectivity, or set `fallbackToLegacy: true` to accept an unverified promotion.

## How the operator reaches Replicas

Both Kubegres Services are headless, and the Replica Service fans out across every Replica, so
there is no stable name addressing one specific instance. Replica connections therefore use the
Pod IP directly as the DSN host; everything else — credentials, database, TLS material — is
inherited from the primary connection the `DBConnectionReconciler` already maintains.

Because the Replica is addressed by IP, `spec.tls.mode: verify-full` will reject these
connections unless the server certificate carries a matching IP SAN. The probes then fail, every
Replica looks unreachable, and selection follows `fallbackToLegacy`. Watch
`kubegres_replica_query_errors_total` after enabling the feature on a TLS cluster.

Connections are created on demand, cached per instance index in the shared `ConnectionStore`, and
re-pointed rather than replaced when a Replica is rescheduled onto a new Pod IP. Connections and
metric series for Replicas that no longer exist are pruned during steady-state reconciliation.

This gives the operator no privileges it did not already have: it uses the same superuser
credentials it already holds for replication-slot management, over the same TLS configuration.

## Limitations

* **Network partitions.** The operator is a single point of failure for health determination. If
  it is partitioned from the Replicas it cannot verify them, and `fallbackToLegacy` decides what
  happens. Distributed consensus is out of scope; a specialised HA tool such as Patroni is the
  long-term answer.
* **Timeline divergence is detected, not repaired.** A Replica on an abandoned timeline is
  excluded from selection. Bringing it back into the cluster still requires `pg_rewind` or a
  fresh basebackup.
* **The lag ceiling degrades gracefully rather than strictly.** See "Where the lag reference
  point comes from" above.
* **`verify-full` TLS is not supported for Replica probing**, because Replicas are addressed by
  Pod IP. See "How the operator reaches Replicas" above.
