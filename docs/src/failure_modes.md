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

### Timeline Divergence Recovery

After a failover, a replica may encounter a timeline divergence error if its
checkpoint is ahead of the fork point on the new primary's timeline. This can
occur when:

- Network issues delay replication during a failover
- WAL archiving lag prevents the replica from receiving all changes
- The replica was isolated during the failover

When PostgreSQL detects this condition, it fails to start with the error
"requested timeline X is not a child of this server's history". CloudNativePG
automatically recovers from this scenario:

1. The instance manager detects the timeline divergence during startup
2. The Pod exits with a specific exit code
3. The operator detects this exit code and marks the instance as unrecoverable
4. The PVC is deleted automatically
5. A new Pod is created and re-clones from the current primary via `pg_basebackup`

This automated recovery ensures the cluster returns to a healthy state without
manual intervention.

## Manual Intervention

For failure scenarios not covered by automated recovery, manual intervention
may be required.

:::info[Important]
    Do not perform manual operations without [professional support](https://cloudnative-pg.io/support/).
:::

### Disabling Reconciliation

To temporarily disable the reconciliation loop for a PostgreSQL cluster, use
the `cnpg.io/reconciliationLoop` annotation:

```yaml
metadata:
  name: cluster-example-no-reconcile
  annotations:
    cnpg.io/reconciliationLoop: "disabled"
spec:
  # ...
```

Use this annotation **with extreme caution** and only during emergency
operations.

:::warning
    This annotation should be removed as soon as the issue is resolved. Leaving
    it in place prevents the operator from executing self-healing actions,
    including failover.
:::