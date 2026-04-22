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

## Detection of node-level failures

When the node hosting the primary becomes unreachable (for example, due to a
kubelet crash or a network partition between the node and the Kubernetes API
server), the operator relies on the pod's `Ready` condition to decide that the
primary is no longer serviceable. While the node is healthy the kubelet keeps
that condition up to date from the readiness probe; once the node stops
reporting, the Kubernetes node lifecycle controller is the one that flips the
condition to `False` as soon as it declares the node `Unknown`.

With stock kube-controller-manager settings, the transition is governed by
`--node-monitor-grace-period` (default `40s` on Kubernetes 1.29-1.31, raised
to `50s` in 1.32 and later): after that window the controller marks the node
`Unknown` and, in the same monitoring pass, issues a patch per pod on that
node to flip the `Ready` condition. In practice the operator observes the
primary as unready about **40 to 55 seconds** after the node becomes
unreachable (the grace period plus up to one `--node-monitor-period` poll,
default `5s`). Managed Kubernetes distributions (GKE, EKS, AKS) may tune
these values; consult the provider's documentation if the observed timing
does not match. After that, the failover procedure starts (further gated by
`.spec.failoverDelay`).

The `Ready` condition flip is not subject to the rate limiters that throttle
pod *eviction* during partial-zonal or large-cluster disruptions
(`--node-eviction-rate`, `--secondary-node-eviction-rate`,
`--unhealthy-zone-threshold`). The operator reacts to the condition flip as
soon as the controller emits the patch, regardless of the zone or cluster-wide
health state.

Pod *eviction* (actual deletion from the unreachable node) is a separate
mechanism, driven by `tolerationSeconds` on the
`node.kubernetes.io/unreachable` `NoExecute` taint (`300s` by default). That
timer does not hold up the operator's failover decision; CloudNativePG
promotes a new primary as soon as the `Ready` condition flips. By that point
the kubelet on the isolated node has already stopped the old PostgreSQL
container locally: with the default
`.spec.probes.liveness.isolationCheck.enabled: true`, the instance manager
fails its own liveness probe once it can reach neither the API server nor
the rest of the cluster, and the kubelet kills the container within
approximately three probe periods (`~30s`). Full high availability
(recreation of the old primary on a healthy node by the operator) is still
gated on the taint-based eviction actually deleting the pod.
