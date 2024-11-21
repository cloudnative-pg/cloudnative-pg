# Failure Modes

This section provides an overview of the major failure scenarios that
PostgreSQL can face on a Kubernetes cluster during its lifetime.

!!! Important
    In case the failure scenario you are experiencing is not covered by this
    section, please immediately seek for [professional support](https://cloudnative-pg.io/support/).

!!! Seealso "Postgres instance manager"
    Please refer to the ["Postgres instance manager" section](instance_manager.md)
    for more information the liveness and readiness probes implemented by
    CloudNativePG.

## Storage space usage

The operator will instantiate one PVC for every PostgreSQL instance to store the `PGDATA` content.
A second PVC dedicated to the WAL storage will be provisioned in case `.spec.walStorage` is
specified during cluster initialization.

Such storage space is set for reuse in two cases:

- when the corresponding Pod is deleted by the user (and a new Pod will be recreated)
- when the corresponding Pod is evicted and scheduled on another node

If you want to prevent the operator from reusing a certain PVC you need to
remove the PVC before deleting the Pod. For this purpose, you can use the
following command:

```sh
kubectl delete -n [namespace] pvc/[cluster-name]-[serial] pod/[cluster-name]-[serial]
```

!!! Note
    If you specified a dedicated WAL volume, it will also have to be deleted during this process.

```sh
kubectl delete -n [namespace] pvc/[cluster-name]-[serial] pvc/[cluster-name]-[serial]-wal pod/[cluster-name]-[serial]
```

For example:

```sh
$ kubectl delete -n default pvc/cluster-example-1 pvc/cluster-example-1-wal pod/cluster-example-1
persistentvolumeclaim "cluster-example-1" deleted
persistentvolumeclaim "cluster-example-1-wal" deleted
pod "cluster-example-1" deleted
```

## Failure modes

A pod belonging to a `Cluster` can fail in the following ways:

* the pod is explicitly deleted by the user;
* the readiness probe on its `postgres` container fails;
* the liveness probe on its `postgres` container fails;
* the Kubernetes worker node is drained;
* the Kubernetes worker node where the pod is scheduled fails.

Each one of these failures has different effects on the `Cluster` and the
services managed by the operator.

### Pod deleted by the user

The operator is notified of the deletion. A new pod belonging to the
`Cluster` will be automatically created reusing the existing PVC, if available,
or starting from a physical backup of the *primary* otherwise.

!!! Important
    In case of deliberate deletion of a pod, `PodDisruptionBudget` policies
    will not be enforced.

Self-healing will happen as soon as the *apiserver* is notified.

You can trigger a sudden failure on a given pod of the cluster using the
following generic command:

```sh
kubectl delete -n [namespace] \
  pod/[cluster-name]-[serial] --grace-period=1
```

For example, if you want to simulate a real failure on the primary and trigger
the failover process, you can run:

```sh
kubectl delete pod [primary pod] --grace-period=1
```

!!! Warning
    Never use `--grace-period=0` in your failover simulation tests, as this
    might produce misleading results with your PostgreSQL cluster. A grace
    period of 0 guarantees that the pod is immediately removed from the
    Kubernetes API server, without first ensuring that the PID 1 process of
    the `postgres` container (the instance manager) is shut down - contrary
    to what would happen in case of a real failure (e.g. unplug the power cord
    cable or network partitioning).
    As a result, the operator doesn't see the pod of the primary anymore, and
    triggers a failover promoting the most aligned standby, without
    the guarantee that the primary had been shut down.


### Readiness probe failure

After 3 failures, the pod will be considered *not ready*. The pod will still
be part of the `Cluster`, no new pod will be created.

If the cause of the failure can't be fixed, it is possible to delete the pod
manually. Otherwise, the pod will resume the previous role when the failure
is solved.

Self-healing will happen after three failures of the probe.

### Liveness probe failure

After 3 failures, the `postgres` container will be considered failed. The
pod will still be part of the `Cluster`, and the *kubelet* will try to restart
the container. If the cause of the failure can't be fixed, it is possible
to delete the pod manually.

Self-healing will happen after three failures of the probe.

### Worker node drained

The pod will be evicted from the worker node and removed from the service. A
new pod will be created on a different worker node from a physical backup of the
*primary* if the `reusePVC` option of the `nodeMaintenanceWindow` parameter
is set to `off` (default: `on` during maintenance windows, `off` otherwise).

The `PodDisruptionBudget` may prevent the pod from being evicted if there
is at least another pod that is not ready.

!!! Note
    Single instance clusters prevent node drain when `reusePVC` is
    set to `false`. Refer to the [Kubernetes Upgrade section](kubernetes_upgrade.md).

Self-healing will happen as soon as the *apiserver* is notified.

### Worker node failure

Since the node is failed, the *kubelet* won't execute the liveness and
the readiness probes. The pod will be marked for deletion after the
toleration seconds configured by the Kubernetes cluster administrator for
that specific failure cause. Based on how the Kubernetes cluster is configured,
the pod might be removed from the service earlier.

A new pod will be created on a different worker node from a physical backup
of the *primary*. The default value for that parameter in a Kubernetes
cluster is 5 minutes.

Self-healing will happen after `tolerationSeconds`.

## Self-healing

If the failed pod is a standby, the pod is removed from the `-r` service
and from the `-ro` service.
The pod is then restarted using its PVC if available; otherwise, a new
pod will be created from a backup of the current primary. The pod
will be added again to the `-r` service and to the `-ro` service when ready.

If the failed pod is the primary, the operator will promote the active pod
with status ready and the lowest replication lag, then point the `-rw` service
to it. The failed pod will be removed from the `-r` service and from the
`-rw` service.
Other standbys will start replicating from the new primary. The former
primary will use `pg_rewind` to synchronize itself with the new one if its
PVC is available; otherwise, a new standby will be created from a backup of the
current primary.

## Manual intervention

In the case of undocumented failure, it might be necessary to intervene
to solve the problem manually.

!!! Important
    In such cases, please do not perform any manual operation without
    [professional support](https://cloudnative-pg.io/support/).

You can use the `cnpg.io/reconciliationLoop` annotation to temporarily disable
the reconciliation loop for a specific PostgreSQL cluster, as shown below:

``` yaml
metadata:
  name: cluster-example-no-reconcile
  annotations:
    cnpg.io/reconciliationLoop: "disabled"
spec:
  # ...
```

The `cnpg.io/reconciliationLoop` must be used with extreme care
and for the sole duration of the extraordinary/emergency operation.

!!! Warning
    Please make sure that you use this annotation only for a limited period of
    time and you remove it when the emergency has finished. Leaving this annotation
    in a cluster will prevent the operator from issuing any self-healing operation,
    such as a failover.

