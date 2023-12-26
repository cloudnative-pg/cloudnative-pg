# Storage

Storage is the most critical component in a database workload.
Storage must always be available, scale, perform well,
and guarantee consistency and durability. The same expectations and
requirements that apply to traditional environments, such as virtual machines
and bare metal, are also valid in container contexts managed by Kubernetes.

!!! Important
    When it comes to dynamically provisioned storage,
    Kubernetes has its own specifics. These include *storage classes*, *persistent
    volumes*, and *persistent volume claims*. You need to own these
    concepts, on top of all the valuable knowledge you've built over
    the years in terms of storage for database workloads on VMs and
    physical servers.

There are two primary methods of access to storage:

- **Network** – Either directly or indirectly. (Think of an NFS volume locally
  mounted on a host running Kubernetes.)
- **Local** – Directly attached to the node where a pod is running. This also
  includes directly attached disks on bare metal installations of Kubernetes.

Network storage, which is the most common usage pattern in Kubernetes,
presents the same issues of throughput and latency that you can
experience in a traditional environment. These issues can be accentuated in
a shared environment, where I/O contention with several applications
increases the variability of performance results.

Local storage enables shared-nothing architectures, which is more suitable
for high transactional and very large database (VLDB) workloads, as it
guarantees higher and more predictable performance.

!!! Warning
    Before you deploy a PostgreSQL cluster with CloudNativePG,
    ensure that the storage you're using is recommended for database
    workloads. We recommend clearly setting performance expectations by
    first benchmarking the storage using tools such as [fio](https://fio.readthedocs.io/en/latest/fio_doc.html)
    and then the database using [pgbench](https://www.postgresql.org/docs/current/pgbench.html).

!!! Info
    CloudNativePG doesn't use `StatefulSet` for managing data persistence.
    Rather, it manages persistent volume claims (PVCs) directly. If you want
    to know more, see
    [Custom pod controller](controller.md).

## Backup and recovery

Since CloudNativePG supports volume snapshots for both backup and recovery,
we recommend that you also consider this aspect when you choose your storage
solution, especially if you manage very large databases.

!!! Important
    See the Kubernetes documentation for a list of all
    the supported [container storage interface (CSI) drivers](https://kubernetes-csi.github.io/docs/drivers.html)
    that provide snapshot capabilities.

## Benchmarking CloudNativePG

Before deploying the database in production, we recommend that you benchmark 
CloudNativePG in a controlled Kubernetes environment. Follow
the guidelines in [Benchmarking](benchmarking.md).

Briefly, we recommend operating at two levels:

- Measuring the performance of the underlying storage using fio, with relevant
  metrics for database workloads such as throughput for sequential reads, sequential
  writes, random reads, and random writes
- Measuring the performance of the database using pgbench, the default benchmarking tool
  distributed with PostgreSQL

!!! Important
    You must measure both the storage and database performance
    before putting the database into production. These results
    are extremely valuable not just in the planning phase (for example, capacity planning).
    They are also valuable in the production lifecycle, especially in emergency situations
    when you don't have time to run this kind of test. Databases
    change and evolve over time, and so does the distribution of data, potentially affecting
    performance. Knowing the theoretical maximum throughput of sequential reads or
    writes is extremely useful in those situations. This is true especially in
    shared-nothing contexts, where results don't vary due to the influence of external workloads.

    Know your system: benchmark it.

## Encryption at rest

Encryption at rest is possible with CloudNativePG. The operator delegates that
to the underlying storage class. See the storage class for
information about this important security feature.

## Persistent volume claim

The operator creates a persistent volume claim (PVC) for each PostgreSQL
instance, with the goal of storing the `PGDATA`. It then mounts it into each pod.

Additionally, it supports creating clusters with:

- A separate PVC on which to store PostgreSQL write-ahead log (WAL), as
  explained in [Volume for WAL](#volume-for-wal)
- Additional separate volumes reserved for PostgreSQL tablespaces, as explained
  in [Tablespaces](tablespaces.md)

In CloudNativePG, the volumes attached to a single PostgreSQL instance
are defined as a *PVC group*.

## Configuration via a storage class

!!! Important
    CloudNativePG wasn't designed to work with a specific storage class.
    As usual, we recommend properly benchmarking the storage class
    in a controlled environment before deploying to production.

The easiest way to configure the storage for a PostgreSQL class is to
request storage of a certain size, like in the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: postgresql-storage-class
spec:
  instances: 3
  storage:
    size: 1Gi
```

Using the previous configuration, the generated PVCs are satisfied by the default storage
class. If the target Kubernetes cluster has no default storage class, or even if you need your PVCs
to be satisfied by a known storage class, you can set it into the custom resource:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: postgresql-storage-class
spec:
  instances: 3
  storage:
    storageClass: standard
    size: 1Gi
```

## Configuration via a PVC template

To further customize the generated PVCs, you can provide a PVC template inside the custom resource,
like in the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: postgresql-pvc-template
spec:
  instances: 3

  storage:
    pvcTemplate:
      accessModes:
        - ReadWriteOnce
      resources:
        requests:
          storage: 1Gi
      storageClassName: standard
      volumeMode: Filesystem
```

## Volume for WAL

By default, PostgreSQL stores all its data in the so-called `PGDATA` (a directory).
One of the core directories inside `PGDATA` is `pg_wal`, which contains the log of transactional
changes that occurred in the database, in the form of segment files. (`pq_wal` is historically
known as `pg_xlog` in PostgreSQL.)

!!! Info
    Normally, each segment is 16MB in size, but you can configure the size
    using the `walSegmentSize` option. This option is applied at cluster initialization time, as
    described in [Bootstrap an empty cluster](bootstrap.md#bootstrap-an-empty-cluster-initdb).

In most cases, having `pg_wal` on the same volume where `PGDATA`
resides is fine. However, having WALs stored in a separate
volume has a few benefits:

- **I/O performance** – By storing WAL files on different storage from `PGDATA`,
  PostgreSQL can exploit parallel I/O for WAL operations (normally
  sequential writes) and for data files (tables and indexes for example), thus
  improving vertical scalability.

- **More reliability** – By reserving dedicated disk space to WAL files, you
  can be sure that exhausting space on the `PGDATA` volume
  never interferes with WAL writing. This behavior ensures that your PostgreSQL primary
  is correctly shut down.

- **Finer control** – You can define the amount of space dedicated to both
  `PGDATA` and `pg_wal`, fine tune [WAL
  configuration](https://www.postgresql.org/docs/current/wal-configuration.html)
  and checkpoints, and even use a different storage class for cost optimization.

- **Better I/O monitoring** – You can constantly monitor the load and disk usage
  on both `PGDATA` and `pg_wal`. You can also set alerts that notify you in case,
  for example, `PGDATA` requires resizing.


!!! Seealso "Write-ahead log (WAL)"
    See [Reliability and the Write-Ahead Log](https://www.postgresql.org/docs/current/wal.html)
    in the PostgreSQL documentation for more information.

You can add a separate volume for WAL using the `.spec.walStorage` option.
It follows the same rules described for the `storage` field and provisions a
dedicated PVC. For example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: separate-pgwal-volume
spec:
  instances: 3
  storage:
    size: 1Gi
  walStorage:
    size: 1Gi
```

!!! Important
    Removing `walStorage` isn't supported. Once added, a separate volume for
    WALs can't be removed from an existing Postgres cluster.

## Volumes for tablespaces

CloudNativePG supports declarative tablespaces. You can add one or more
volumes, each dedicated to a single PostgreSQL tablespace.
See [Tablespaces](tablespaces.md) for details.

## Volume expansion

Kubernetes exposes an API allowing [expanding PVCs](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#expanding-persistent-volumes-claims)
that's enabled by default. However, it needs to be supported by the underlying `StorageClass`.

To check if a certain `StorageClass` supports volume expansion, you can read the `allowVolumeExpansion`
field for your storage class:

```
$ kubectl get storageclass -o jsonpath='{$.allowVolumeExpansion}' premium-storage
true
```

### Using the volume expansion Kubernetes feature

Given the storage class supports volume expansion, you can change the size requirement
of the `Cluster`, and the operator applies the change to every PVC.

If the `StorageClass` supports [online volume resizing](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#resizing-an-in-use-persistentvolumeclaim),
the change is immediately applied to the pods. If the underlying storage class doesn't support
that, you must delete the pod to trigger the resize.

The best way to proceed is to delete one pod at a time, starting from replicas and waiting
for each pod to be back up.

### Expanding PVC volumes on AKS

Currently, [Azure can't resize the PVC's volume without restarting the pod](https://github.com/Azure/AKS/issues/1477).
CloudNativePG has overcome this limitation through the
`ENABLE_AZURE_PVC_UPDATES` environment variable in the
[operator configuration](operator_conf.md#available-options).
When set to `true`, CloudNativePG triggers a rolling update of the
Postgres cluster.

Alternatively, you can use the following workaround to manually resize the
volume in AKS.

#### Workaround for volume expansion on AKS

You can manually resize a PVC on AKS.
As an example, suppose you have a cluster with three replicas:

```
$ kubectl get pods
NAME                READY   STATUS    RESTARTS   AGE
cluster-example-1   1/1     Running   0          2m37s
cluster-example-2   1/1     Running   0          2m22s
cluster-example-3   1/1     Running   0          2m10s
```

An Azure disk can be expanded only while in "unattached" state, as described in the
[Kubernetes documentation](https://github.com/kubernetes-sigs/azuredisk-csi-driver/blob/master/docs/known-issues/sizegrow.md).  <!-- wokeignore:rule=master -->
This means that, to resize a disk used by a PostgreSQL cluster, you need to perform a manual rollout,
first cordoning the node that hosts the pod using the PVC bound to the disk. This prevents the operator
from re-creating the pod and immediately reattaching it to its PVC before the background disk resizing is complete.

First, edit the cluster definition, applying the new size. In this example, the new size is `2Gi`.

```
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3

  storage:
    storageClass: default
    size: 2Gi
```

Assuming the `cluster-example-1` pod is the cluster's primary, you can proceed with the replicas first.
For example, start with cordoning the Kubernetes node that hosts the `cluster-example-3` pod:

```
kubectl cordon <node of cluster-example-3>
```

Then delete the `cluster-example-3` pod:

```
$ kubectl delete pod/cluster-example-3
```

Run the following command:

```
kubectl get pvc -w -o=jsonpath='{.status.conditions[].message}' cluster-example-3
```

Wait until you see the following output:

```
Waiting for user to (re-)start a Pod to finish file system resize of volume on node.
```

Then, you can uncordon the node:

```
kubectl uncordon <node of cluster-example-3>
```

Wait for the pod to be re-created correctly and get in a "Running and Ready" state:

```
kubectl get pods -w cluster-example-3
cluster-example-3   0/1     Init:0/1   0          12m
cluster-example-3   1/1     Running   0          12m
```

Verify the PVC expansion by running the following command, which returns `2Gi` as configured:

```
kubectl get pvc cluster-example-3 -o=jsonpath='{.status.capacity.storage}'
```

You can repeat these steps for the remaining pods.

!!! Important
    Leave the resizing of the disk associated with the primary instance as the last disk,
    after promoting through a switchover a new resized pod, using `kubectl cnpg promote`.
    For example, use `kubectl cnpg promote cluster-example 3` to promote `cluster-example-3` to primary.

### Re-creating storage

If the storage class doesn't support volume expansion, you can still regenerate your cluster
on different PVCs. Allocate new PVCs with increased storage and then move the
database there. This operation is feasible only when the cluster contains more than one node.

While you do that, you need to prevent the operator from changing the existing PVC
by disabling the `resizeInUseVolumes` flag, like in the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: postgresql-pvc-template
spec:
  instances: 3

  storage:
    storageClass: standard
    size: 1Gi
    resizeInUseVolumes: False
```

To move the entire cluster to a different storage area, you need to re-create all the PVCs and
all the pods. Suppose you have a cluster with three replicas, like in the following
example:

```
$ kubectl get pods
NAME                READY   STATUS    RESTARTS   AGE
cluster-example-1   1/1     Running   0          2m37s
cluster-example-2   1/1     Running   0          2m22s
cluster-example-3   1/1     Running   0          2m10s
```

To re-create the cluster using different PVCs, you can edit the cluster definition to disable
`resizeInUseVolumes`. Then re-create every instance in a different PVC.

For example, re-create the storage for `cluster-example-3`:

```
$ kubectl delete pvc/cluster-example-3 pod/cluster-example-3
```

!!! Important
    If you created a dedicated WAL volume, both PVCs must be deleted during this process.
    The same procedure applies if you want to regenerate the WAL volume PVC. You can do this
    by also disabling `resizeInUseVolumes` for the `.spec.walStorage` section.

For example, if a PVC dedicated to WAL storage is present:

```
$ kubectl delete pvc/cluster-example-3 pvc/cluster-example-3-wal pod/cluster-example-3
```

Having done that, the operator orchestrates creating another replica with a
resized PVC:

```
$ kubectl get pods
NAME                           READY   STATUS      RESTARTS   AGE
cluster-example-1              1/1     Running     0          5m58s
cluster-example-2              1/1     Running     0          5m43s
cluster-example-4-join-v2      0/1     Completed   0          17s
cluster-example-4              1/1     Running     0          10s
```

## Static provisioning of persistent volumes

CloudNativePG was designed to work with dynamic volume provisioning. This capability
allows storage volumes to be created on demand when requested by
users by way of storage classes and persistent volume claim templates. See [Re-creating storage](#re-creating-storage).

However, in some cases, Kubernetes administrators prefer to manually create
storage volumes and then create the related `PersistentVolume` objects for
their representation inside the Kubernetes cluster. This is also known as
*pre-provisioning* of volumes.

!!! Important
    We recommend that you avoid pre-provisioning volumes, as it
    has an effect on the high availability and self-healing capabilities
    of the operator. It breaks the fully declarative model on which
    CloudNativePG was built.

To use a pre-provisioned volume in CloudNativePG:

1. Manually create the volume outside Kubernetes.
2. Create the `PersistentVolume` object to match this volume using the
   correct parameters as required by the actual CSI driver (that is, `volumeHandle`,
   `fsType`, `storageClassName`, and so on).
3. Create the Postgres `Cluster` using, for each storage section, a coherent
   [`pvcTemplate`](storage.md#configuration-via-a-pvc-template)
   section that can help Kubernetes match the `PersistentVolume`
   and enable CloudNativePG to create the needed `PersistentVolumeClaim`.

!!! Warning
    With static provisioning, it's your responsibility to ensure that 
    Postgres pods can be correctly scheduled
    by Kubernetes where a pre-provisioned volume exists. (The scheduling configuration is based
    on the affinity rules of your cluster.) Make sure you check
    for any pods stuck in `Pending` after you deploy the cluster. 
    If the condition persists, investigate why it's happening.

## Block storage considerations (Ceph/ Longhorn)

Most block storage solutions in Kubernetes are suggested to have multiple replicas of a volume
to improve resiliency. This works well for workloads that don't have resiliency built into the 
application. However, CloudNativePG has this resiliency built directly into the Postgres `Cluster`
through the number of instances and the persistent volumes that are attached to them. 

In these cases, it makes sense to define the storage class used by the Postgres clusters
as one replica. By having additional replicas defined in the storage solution (like 
Longhorn and Ceph), you might incur what's known as write amplification, unnecessarily
increasing disk I/O and space used.
