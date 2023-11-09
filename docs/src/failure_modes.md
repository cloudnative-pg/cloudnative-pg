# Failure modes

PostgreSQL on a Kubernetes cluster can face major failure scenarios during its lifetime.

!!! Important
    If the failure scenario you're experiencing isn't covered here, 
    contact EDB immediately for support and assistance.

!!! Seealso "Postgres instance manager"
    See [Postgres instance manager](instance_manager.md)
    for more information the liveness and readiness probes implemented by
    CloudNativePG.

## Storage space usage

To store the `PGDATA` content, the operator instantiates one PVC for every PostgreSQL instance.
If `.spec.walStorage` is specified during cluster initialization, a second PVC dedicated to the WAL storage is provisioned.

Such storage space is set for reuse in two cases:

- When the corresponding pod is deleted by the user (and a new pod is re-created)
- When the corresponding pod is evicted and scheduled on another node

If you want to prevent the operator from reusing a certain PVC, you need to
remove the PVC before deleting the pod. To do so, you can use the
following command:

```sh
kubectl delete -n [namespace] pvc/[cluster-name]-[serial] pod/[cluster-name]-[serial]
```

!!! Note
    If you specified a dedicated WAL volume, you must also delete it during this process.

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

A pod belonging to a cluster can fail in the following ways:

* The user explicitly deletes the pod.
* The readiness probe on its `postgres` container fails.
* The liveness probe on its `postgres` container fails.
* The Kubernetes worker node is drained.
* The Kubernetes worker node where the pod is scheduled fails.

Each of these failures has a different effect on the cluster and the
services managed by the operator.

### Pod deleted by the user

The operator is notified of the deletion. A new pod belonging to the
cluster is created reusing the existing PVC, if available. Otherwise,
it's created starting from a physical backup of the primary.

!!! Important
    If a pod is deliberately deleted, `PodDisruptionBudget` policies
    aren't enforced.

Self-healing happens as soon as the `apiserver` is notified.

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
    Kubernetes API server without first ensuring that the PID 1 process of
    the `postgres` container (the instance manager) is shut down. This behavior is contrary
    to what would happen in case of a real failure (for example, unplugging the power cord
    cable or network partitioning).
    As a result, the operator doesn't see the pod of the primary anymore. It
    triggers a failover, promoting the most aligned standby without
    the guarantee that the primary was shut down.


### Readiness probe failure

After three failures, the pod is considered *not ready*. The pod is still
part of the cluster, but no new pod is created.

If the cause of the failure can't be fixed, you can delete the pod
manually. Otherwise, the pod resumes the previous role when the failure
is solved.

Self-healing happens after three failures of the probe.

### Liveness probe failure

After three failures, the `postgres` container is considered failed. The
pod is still be part of the cluster, and the kubelet tries to restart
the container. If the cause of the failure can't be fixed, you can
delete the pod manually.

Self-healing happens after three failures of the probe.

### Worker node drained

The pod is evicted from the worker node and removed from the service. 
If the `reusePVC` option of the `nodeMaintenanceWindow` parameter
is set to `off`, a
new pod is created on a different worker node from a physical backup of the
primary. (The default is `on` during maintenance windows, `off` otherwise.)

The `PodDisruptionBudget` might prevent the pod from being evicted if
at least one other pod isn't ready.

!!! Note
    Single-instance clusters prevent node drain when `reusePVC` is
    set to `false`. See [Kubernetes upgrade](kubernetes_upgrade.md).

Self-healing happens as soon as the `apiserver` is notified.

### Worker node failure

Since the node is failed, the kubelet doesn't execute the liveness and
the readiness probes. The pod is marked for deletion after the
toleration seconds configured by the Kubernetes cluster administrator for
that specific failure cause. Based on how the Kubernetes cluster is configured,
the pod might be removed from the service earlier.

A new pod is created on a different worker node from a physical backup
of the primary. The default value for that parameter in a Kubernetes
cluster is 5 minutes.

Self-healing happens after `tolerationSeconds`.

## Self-healing

If the failed pod is a standby, the pod is removed from the `-r` service
and from the `-ro` service.
The pod is then restarted using its PVC, if available. Otherwise, a new
pod is created from a backup of the current primary. The pod
is added again to the `-r` service and to the `-ro` service when ready.

If the failed pod is the primary, the operator promotes the active pod
with status ready and the lowest replication lag and then points the `-rw` service
to it. The failed pod is removed from the `-r` service and from the
`-rw` service.
Other standbys start replicating from the new primary. The former
primary uses `pg_rewind` to synchronize itself with the new one if its
PVC is available. Otherwise, a new standby is created from a backup of the
current primary.

## Manual intervention

In the case of undocumented failure, it might be necessary to intervene
to solve the problem manually.

!!! Important
    In such cases, contact the EDB engineering team for help and support.
    Don't perform any manual operation on your own.

From version 1.11.0 of the operator, you can use the
`cnpg.io/reconciliationLoop` annotation to temporarily disable the
reconciliation loop on a selected PostgreSQL cluster:

``` yaml
metadata:
  name: cluster-example-no-reconcile
  annotations:
    cnpg.io/reconciliationLoop: "disabled"
spec:
  # ...
```

Use `cnpg.io/reconciliationLoop` with extreme care
and only for the duration of the extraordinary or emergency operation.

!!! Warning
    Make sure that you use this annotation only for a limited time
    and that you remove it when the emergency is over. Leaving this annotation
    in a cluster prevents the operator from issuing any self-healing operation,
    such as a failover.
