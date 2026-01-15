---
id: failover
sidebar_position: 400
title: Automated failover
---

# Automated failover
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

In the case of unexpected errors on the primary for longer than the
`.spec.failoverDelay` (by default `0` seconds), the cluster will go into
**failover mode**. This may happen, for example, when:

- The primary pod has a disk failure
- The primary pod is deleted
- The `postgres` container on the primary has any kind of sustained failure

In the failover scenario, the primary cannot be assumed to be working properly.

After cases like the ones above, the readiness probe for the primary pod will start
failing. This will be picked up in the controller's reconciliation loop. The
controller will initiate the failover process, in two steps:

1. First, it will mark the `TargetPrimary` as `pending`. This change of state will
   force the primary pod to shutdown, to ensure the WAL receivers on the replicas
   will stop. The cluster will be marked in failover phase ("Failing over").
2. Once all WAL receivers are stopped, there will be a leader election, and a
   new primary will be named. The chosen instance will initiate promotion to
   primary, and, after this is completed, the cluster will resume normal operations.
   Meanwhile, the former primary pod will restart, detect that it is no longer
   the primary, and become a replica node.

:::info[Important]
    The two-phase procedure helps ensure the WAL receivers can stop in an orderly
    fashion, and that the failing primary will not start streaming WALs again upon
    restart. These safeguards prevent timeline discrepancies between the new primary
    and the replicas.
:::

During the time the failing primary is being shut down:

1. It will first try a PostgreSQL's *fast shutdown* with
   `.spec.switchoverDelay` seconds as timeout. This graceful shutdown will attempt
   to archive pending WALs.
2. If the fast shutdown fails, or its timeout is exceeded, a PostgreSQL's
   *immediate shutdown* is initiated.

:::info
    "Fast" mode does not wait for PostgreSQL clients to disconnect and will
    terminate an online backup in progress. All active transactions are rolled back
    and clients are forcibly disconnected, then the server is shut down.
    "Immediate" mode will abort all PostgreSQL server processes immediately,
    without a clean shutdown.
:::

## RTO and RPO impact

Failover may result in the service being impacted ([RTO](before_you_start.md#postgresql-terminology))
and/or data being lost ([RPO](before_you_start.md#postgresql-terminology)):

1. During the time when the primary has started to fail, and before the controller
   starts failover procedures, queries in transit, WAL writes, checkpoints and
   similar operations, may fail.
2. Once the fast shutdown command has been issued, the cluster will no longer
   accept connections, so service will be impacted but no data
   will be lost.
3. If the fast shutdown fails, the immediate shutdown will stop any pending
   processes, including WAL writing. Data may be lost.
4. During the time the primary is shutting down and a new primary hasn't yet
   started, the cluster will operate without a primary and thus be impaired - but
   with no data loss.

:::note
    The timeout that controls fast shutdown is set by `.spec.switchoverDelay`,
    as in the case of a switchover. Increasing the time for fast shutdown is safer
    from an RPO point of view, but possibly delays the return to normal operation -
    negatively affecting RTO.
:::

:::warning
    As already mentioned in the ["Instance Manager" section](instance_manager.md)
    when explaining the switchover process, the `.spec.switchoverDelay` option
    affects the RPO and RTO of your PostgreSQL database. Setting it to a low value,
    might favor RTO over RPO but lead to data loss at cluster level and/or backup
    level. On the contrary, setting it to a high value, might remove the risk of
    data loss while leaving the cluster without an active primary for a longer time
    during the switchover.
:::

## Delayed failover

As anticipated above, the `.spec.failoverDelay` option allows you to delay the start
of the failover procedure by a number of seconds after the primary has been
detected to be unhealthy. By default, this setting is set to `0`, triggering the
failover procedure immediately.

Sometimes failing over to a new primary can be more disruptive than waiting
for the primary to come back online. This is especially true of network
disruptions where multiple tiers are affected (i.e., downstream logical
subscribers) or when the time to perform the failover is longer than the
expected outage.

Enabling a new configuration option to delay failover provides a mechanism to
prevent premature failover for short-lived network or node instability.

:::warning[Important Relationship with smartShutdownTimeout]
The `.spec.failoverDelay` value must be significantly larger than `.spec.smartShutdownTimeout`
to ensure controlled shutdown operations complete before failover is considered.
A recommended ratio is at least 5:1, meaning `failoverDelay` should be at least
5 times larger than `smartShutdownTimeout`. This prevents premature failover
during planned maintenance operations or controlled shutdowns.

### Preventing Split-Brain Scenarios

This relationship is critical to prevent **split-brain scenarios** that can occur when:

1. **Smart shutdown fails** due to open transactions blocking the shutdown process
2. **The primary remains running** but is unable to accept new connections during the smart shutdown attempt
3. **Failover triggers prematurely** because `failoverDelay` is too short
4. **Two primaries operate simultaneously** - the original primary (still running but blocked) and the newly promoted replica

When smart shutdown fails due to long-running transactions, PostgreSQL waits for those transactions to complete before proceeding. If `failoverDelay` expires during this waiting period, the cluster may incorrectly assume the primary is unhealthy and promote a replica, leading to data inconsistency and potential corruption.
:::

## Failover Quorum (Quorum-based Failover)

Failover quorum is a mechanism that enhances data durability and safety during
failover events in CloudNativePG-managed PostgreSQL clusters.

Quorum-based failover allows the controller to determine whether to promote a replica
to primary based on the state of a quorum of replicas.
This is useful when stronger data durability is required than the one offered
by [synchronous replication](replication.md#synchronous-replication) and
default automated failover procedures.

When synchronous replication is not enabled, some data loss is expected and
accepted during failover, as a replica may lag behind the primary when
promoted.

With synchronous replication enabled, the guarantee is that the application
will not receive explicit acknowledgment of the successful commit of a
transaction until the WAL data is known to be safely received by all required
synchronous standbys.
This is not enough to guarantee that the operator is able to promote the most
advanced replica.

For example, in a three-node cluster with synchronous replication set to `ANY 1
(...)`, data is written to the primary and one standby before a commit is
acknowledged. If both the primary and the aligned standby become unavailable
(such as during a network partition), the remaining replica may not have the
latest data. Promoting it could lose some data that the application considered
committed.

Quorum-based failover addresses this risk by ensuring that failover only occurs
if the operator can confirm the presence of all synchronously committed data in
the instance to promote, and it does not occur otherwise.

This feature allows users to choose their preferred trade-off between data
durability and data availability.

Failover quorum can be enabled by setting the
`.spec.postgresql.synchronous.failoverQuorum` field to `true`:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3

  postgresql:
    synchronous:
      method: any
      number: 1
      failoverQuorum: true

  storage:
    size: 1Gi
```

For backward compatibility, the legacy annotation
`alpha.cnpg.io/failoverQuorum` is still supported by the admission webhook and
takes precedence over the `Cluster` spec option:

- If the annotation evaluates to `"true"` and a synchronous replication stanza
  is present, the webhook automatically sets
  `.spec.postgresql.synchronous.failoverQuorum` to `true`.
- If the annotation evaluates to `"false"`, the feature is always disabled

:::info[Important]
    Because the annotation overrides the spec, we recommend that users of this
    experimental feature migrate to the native
    `.spec.postgresql.synchronous.failoverQuorum` option and remove the annotation
    from their manifests. The annotation is **deprecated** and will be removed in a
    future release.
:::

### How it works

Before promoting a replica to primary, the operator performs a quorum check,
following the principles of the Dynamo `R + W > N` consistency model[^1].

In the quorum failover, these values assume the following meaning:

- `R` is the number of *promotable replicas* (read quorum);
- `W` is the number of replicas that must acknowledge the write before the
  `COMMIT` is returned to the client (write quorum);
- `N` is the total number of potentially synchronous replicas;

*Promotable replicas* are replicas that have these properties:

  - are part of the cluster;
  - are able to report their state to the operator;
  - are potentially synchronous;

If `R + W > N`, then we can be sure that among the promotable replicas there is
at least one that has confirmed all the synchronous commits, and we can safely
promote it to primary. If this is not the case, the controller will not promote
any replica to primary, and will wait for the situation to change.

Users can force a promotion of a replica to primary through the
`kubectl cnpg promote` command even if the quorum check is failing.

:::warning
    Manual promotion should only be used as a last resort. Before proceeding,
    make sure you fully understand the risk of data loss and carefully consider the
    consequences of prioritizing the resumption of write workloads for your
    applications.
:::

An additional CRD is used to track the quorum state of the cluster. A `Cluster`
with the quorum failover enabled will have a `FailoverQuorum` resource with the same
name as the `Cluster` resource. The `FailoverQuorum` CR is created by the
controller when the quorum failover is enabled, and it is updated by the primary
instance during its reconciliation loop, and read by the operator during quorum
checks. It is used to track the latest known configuration of the synchronous
replication.

:::info[Important]
    Users should not modify the `FailoverQuorum` resource directly. During
    PostgreSQL configuration changes, when it is not possible to determine the
    configuration, the `FailoverQuorum` resource will be reset, preventing any
    failover until the new configuration is applied.
:::

The `FailoverQuorum` resource works in conjunction with PostgreSQL synchronous
replication.

:::warning
    There is no guarantee that `COMMIT` operations returned to the
    client but that have not been performed synchronously, such as those made
    explicitly disabling synchronous replication with
    `SET synchronous_commit TO local`, will be present on a promoted replica.
:::

### Quorum Failover Example Scenarios

In the following scenarios, `R` is the number of promotable replicas, `W` is
the number of replicas that must acknowledge a write before commit, and `N` is
the total number of potentially synchronous replicas. The "Failover" column
indicates whether failover is allowed under quorum failover rules.

#### Scenario 1: Three-node cluster, failing pod(s)

A cluster with `instances: 3`, `synchronous.number=1`, and
`dataDurability=required`.

- If only the primary fails, two promotable replicas remain (R=2).
  Since `R + W > N` (2 + 1 > 2), failover is allowed and safe.
- If both the primary and one replica fail, only one promotable replica
  remains (R=1). Since `R + W = N` (1 + 1 = 2), failover is not allowed to
  prevent possible data loss.

| R | W | N | Failover |
|:-:|:-:|:-:|:--------:|
| 2 | 1 | 2 | ✅       |
| 1 | 1 | 2 | ❌       |

#### Scenario 2: Three-node cluster, network partition

A cluster with `instances: 3`, `synchronous.number: 1`, and
`dataDurability: required` experiences a network partition.

- If the operator can communicate with the primary, no failover occurs. The
  cluster can be impacted if the primary cannot reach any standby, since it
  won't commit transactions due to synchronous replication requirements.
- If the operator cannot reach the primary but can reach both replicas (R=2),
  failover is allowed. If the operator can reach only one replica (R=1),
  failover is not allowed, as the synchronous one may be the other one.

| R | W | N | Failover |
|:-:|:-:|:-:|:--------:|
| 2 | 1 | 2 | ✅       |
| 1 | 1 | 2 | ❌       |

#### Scenario 3: Five-node cluster, network partition

A cluster with `instances: 5`, `synchronous.number=2`, and
`dataDurability=required` experiences a network partition.

- If the operator can communicate with the primary, no failover occurs. The
  cluster can be impacted if the primary cannot reach at least two standbys,
  as since it won't commit transactions due to synchronous replication
  requirements.
- If the operator cannot reach the primary but can reach at least three
  replicas (R=3), failover is allowed. If the operator can reach only two
  replicas (R=2), failover is not allowed, as the synchronous one may be the
  other one.

| R | W | N | Failover |
|:-:|:-:|:-:|:--------:|
| 3 | 2 | 4 | ✅       |
| 2 | 2 | 4 | ❌       |

#### Scenario 4: Three-node cluster with remote synchronous replicas

A cluster with `instances: 3` and remote synchronous replicas defined in
`standbyNamesPre` or `standbyNamesPost`. We assume that the primary is failing.

This scenario requires an important consideration. Replicas listed in
`standbyNamesPre` or `standbyNamesPost` are not counted in
`R` (they cannot be promoted), but are included in `N` (they may have received
synchronous writes). So, if
`synchronous.number <= len(standbyNamesPre) + len(standbyNamesPost)`, failover
is not possible, as no local replica can be guaranteed to have the required
data. The operator prevents such configurations during validation, but some
invalid configurations are shown below for clarity.

**Example configurations:**

Configuration #1 (valid):
```yaml
instances: 3
postgresql:
  synchronous:
    method: any
    number: 2
    standbyNamesPre:
      - angus
```
In this configuration, when the primary fails, `R = 2` (the local replicas),
`W = 2`, and `N = 3` (2 local replicas + 1 remote), allowing failover.
In case of an additional replica failing (`R = 1`) failover is not allowed.

| R | W | N | Failover |
|:-:|:-:|:-:|:--------:|
| 3 | 2 | 4 | ✅       |
| 2 | 2 | 4 | ❌       |

Configuration #2 (invalid):
```yaml
instances: 3
postgresql:
  synchronous:
    method: any
    number: 1
    maxStandbyNamesFromCluster: 1
    standbyNamesPre:
      - angus
```
In this configuration, `R = 2` (the local replicas), `W = 1`, and `N = 3`
(2 local replicas + 1 remote).
Failover is not possible in this setup, so quorum failover can not be
enabled with this configuration.

| R | W | N | Failover |
|:-:|:-:|:-:|:--------:|
| 1 | 1 | 2 | ❌       |

Configuration #3 (invalid):
```yaml
instances: 3
postgresql:
  synchronous:
    method: any
    number: 1
    maxStandbyNamesFromCluster: 0
    standbyNamesPre:
      - angus
      - malcolm
```
In this configuration, `R = 0` (the local replicas), `W = 1`, and `N = 2`
(0 local replicas + 2 remote).
Failover is not possible in this setup, so quorum failover can not be
enabled with this configuration.

| R | W | N | Failover |
|:-:|:-:|:-:|:--------:|
| 0 | 1 | 2 | ❌       |

#### Scenario 5: Three-node cluster, preferred data durability, network partition

Consider a cluster with `instances: 3`, `synchronous.number=1`, and
`dataDurability=preferred` that experiences a network partition.

- If the operator can communicate with both the primary and the API server,
  the primary continues to operate, removing unreachable standbys from the
  `synchronous_standby_names` set.
- If the primary cannot reach the operator or API server, a quorum check is
  performed. The `FailoverQuorum` status cannot have changed, as the primary cannot
  have received new configuration. If the operator can reach both replicas,
  failover is allowed (`R=2`). If only one replica is reachable (`R=1`),
  failover is not allowed.

| R | W | N | Failover |
|:-:|:-:|:-:|:--------:|
| 2 | 1 | 2 | ✅       |
| 1 | 1 | 2 | ❌       |

[^1]: [Dynamo: Amazon’s highly available key-value store](https://www.amazon.science/publications/dynamo-amazons-highly-available-key-value-store)
