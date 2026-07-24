---
id: failure_modes
sidebar_position: 140
title: Failure Modes
---

# Failure Modes
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

:::note
    In previous versions of CloudNativePG, this page included specific failure
    scenarios. Since these largely follow standard Kubernetes behavior, we have
    streamlined the content to avoid duplication of information that belongs to the
    underlying Kubernetes stack and is not specific to CloudNativePG.
:::

CloudNativePG adheres to standard Kubernetes principles for self-healing and
high availability. We assume familiarity with core Kubernetes concepts such as
storage classes, PVCs, nodes, and Pods. For CloudNativePG-specific details,
refer to the ["Postgres Instance Manager" section](instance_manager.md), which
covers startup, liveness, and readiness probes, as well as the
[self-healing](#self-healing) section below.

:::info[Important]
    If you are running CloudNativePG in production, we strongly recommend
    seeking [professional support](https://cloudnative-pg.io/support/).
:::

## Self-Healing

### Primary Failure

If the primary Pod fails:

- The operator promotes the most up-to-date standby with the lowest replication
  lag.
- The `-rw` service is updated to point to the new primary.
- The failed Pod is removed from the `-r` and `-rw` services.
- Standby Pods begin replicating from the new primary.
- The former primary uses `pg_rewind` to re-synchronize if its PVC is available;
  otherwise, a new standby is created from a backup of the new primary.

### Standby Failure

If a standby Pod fails:

- It is removed from the `-r` and `-ro` services.
- The Pod is restarted using its PVC if available; otherwise, a new Pod is
  created from a backup of the current primary.
- Once ready, the Pod is re-added to the `-r` and `-ro` services.

## Manual Intervention

For failure scenarios not covered by automated recovery, manual intervention
may be required.

:::info[Important]
    Do not perform manual operations without [professional support](https://cloudnative-pg.io/support/).
:::

### Recovering a stranded replica

After an abrupt primary loss (see
[Abrupt primary loss: data loss window and stranded replicas](failover.md#abrupt-primary-loss-data-loss-window-and-stranded-replicas)),
a surviving replica can be left on a divergent history: it received more data
than the point from which the newly promoted primary continued, so it can no
longer follow the new primary and stops replicating.

CloudNativePG detects this automatically: once the replica's WAL receiver has
stalled behind the current primary's timeline for a grace period, the
operator confirms the divergence and marks the instance unhealthy, fences it
(stopping PostgreSQL so it no longer retries a broken WAL stream and is
removed from the read services), and drops its replication slot on the
primary. This is reflected in the `Cluster` status and reported with a
`ReplicaDiverged` event. Signs of a stranded replica are:

- it stays behind the primary and never catches up;
- it is not connected to the primary for streaming replication;
- its PostgreSQL log repeats an error similar to
  `requested starting point ... is not in this server's history`; and
- the operator has fenced it and reports it as unhealthy.

Recovery is still a manual step in this version: the operator does not yet
rewind or re-clone the instance automatically. To recover, rebuild the
affected instance from the current primary. The
[`cnpg` plugin](kubectl-plugin.md#destroy) removes the instance together with its
storage so the operator re-creates it as a fresh replica aligned with the current
primary:

```sh
kubectl cnpg destroy CLUSTER INSTANCE
```

Replace `CLUSTER` with the cluster name and `INSTANCE` with the stranded Pod's
name.

### Disabling Reconciliation

The `cnpg.io/reconciliationLoop` annotation allows you to temporarily disable
the reconciliation loop for CloudNativePG resources. When set to `"disabled"`,
the operator will stop processing updates for the annotated resource, preventing
any automated changes or self-healing actions.

Use this annotation **with extreme caution** and only during emergency
operations.

:::warning
    This annotation should be removed as soon as the issue is resolved. Leaving
    it in place prevents the operator from managing the annotated resource. On a
    Cluster, this includes self-healing actions and failover.
:::

The following resources support this annotation:

- **Cluster**: Disables reconciliation of the PostgreSQL cluster
- **Backup**: Disables reconciliation of backup operations

Example usage:

```yaml
metadata:
  name: cluster-example-no-reconcile
  annotations:
    cnpg.io/reconciliationLoop: "disabled"
spec:
  # ...
```
