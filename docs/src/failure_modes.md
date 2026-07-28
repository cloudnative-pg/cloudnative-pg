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

### Failover Responsiveness Budget

Once a new primary is elected, the former primary must shut down and release
the primary lease before promotion can complete. If its PostgreSQL is
unresponsive rather than merely gone, for example the underlying node has
frozen while the instance manager process is still reachable, the shutdown
goes through a bounded sequence rather than blocking indefinitely:

1. A `CHECKPOINT` is attempted before shutting down.
2. A `pg_ctl stop -m fast` is attempted.
3. If that does not succeed, a `pg_ctl stop -m immediate` is attempted.
4. If that also fails to report success, the PostgreSQL process group is
   killed, guaranteeing that the instance manager can exit and release the
   lease.

Steps 1 and 2 together are bounded by `.spec.switchoverDelay` (or
`.spec.smartShutdownTimeout` for a smart shutdown), step 3 is bounded by
PostgreSQL's own `pg_ctl` default of 60 seconds, and step 4 follows
immediately once step 3 reports failure. With the defaults left untouched,
this means a cluster can still take up to about an hour in the worst case
before a stuck failover completes, since `switchoverDelay` defaults to 3600
seconds: this is intentional, since only the cluster owner knows how long
their database may legitimately need to checkpoint and shut down. What
changed is that the wait is now finite and always ends with the lease
released, where previously an unresponsive-but-alive PostgreSQL could stall
the failover forever. Lowering `.spec.switchoverDelay` shortens this budget
for clusters where a faster worst case matters more than giving a large,
busy database the full hour to checkpoint.

## Manual Intervention

For failure scenarios not covered by automated recovery, manual intervention
may be required.

:::info[Important]
    Do not perform manual operations without [professional support](https://cloudnative-pg.io/support/).
:::

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
