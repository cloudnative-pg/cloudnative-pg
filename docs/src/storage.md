---
id: storage
sidebar_position: 280
title: Storage
---

# Storage
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

Storage is the most critical component in a database workload.
Storage must always be available, scale, perform well,
and guarantee consistency and durability. The same expectations and
requirements that apply to traditional environments, such as virtual machines
and bare metal, are also valid in container contexts managed by Kubernetes.

:::info[Important]
    When it comes to dynamically provisioned storage,
    Kubernetes has its own specifics. These include *storage classes*, *persistent
    volumes*, and *Persistent Volume Claims (PVCs)*. You need to own these
    concepts, on top of all the valuable knowledge you've built over
    the years in terms of storage for database workloads on VMs and
    physical servers.
:::

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

:::warning
    Before you deploy a PostgreSQL cluster with CloudNativePG,
    ensure that the storage you're using is recommended for database
    workloads. We recommend clearly setting performance expectations by
    first benchmarking the storage using tools such as [fio](https://fio.readthedocs.io/en/latest/fio_doc.html)
    and then the database using [pgbench](https://www.postgresql.org/docs/current/pgbench.html).
:::

:::info
    CloudNativePG doesn't use `StatefulSet` for managing data persistence.
    Rather, it manages PVCs directly. If you want
    to know more, see
    [Custom pod controller](controller.md).
:::

## Backup and recovery

Since CloudNativePG supports volume snapshots for both backup and recovery,
we recommend that you also consider this aspect when you choose your storage
solution, especially if you manage very large databases.

:::info[Important]
    See the Kubernetes documentation for a list of all
    the supported [container storage interface (CSI) drivers](https://kubernetes-csi.github.io/docs/drivers.html)
    that provide snapshot capabilities.
:::

## Benchmarking CloudNativePG

Before deploying the database in production, we recommend that you benchmark
CloudNativePG in a controlled Kubernetes environment. Follow the guidelines in
[Benchmarking](benchmarking.md).

Briefly, we recommend operating at two levels:

- Measuring the performance of the underlying storage using fio, with relevant
  metrics for database workloads such as throughput for sequential reads, sequential
  writes, random reads, and random writes
- Measuring the performance of the database using pgbench, the default benchmarking tool
  distributed with PostgreSQL

:::info[Important]
    You must measure both the storage and database performance before putting
    the database into production. These results are extremely valuable not just in
    the planning phase (for example, capacity planning). They are also valuable in
    the production lifecycle, particularly in emergency situations when you don't
    have time to run this kind of test. Databases change and evolve over time, and
    so does the distribution of data, potentially affecting performance. Knowing
    the theoretical maximum throughput of sequential reads or writes is extremely
    useful in those situations. This is true especially in shared-nothing contexts,
    where results don't vary due to the influence of external workloads.

    Know your system: benchmark it.
:::

## Encryption at rest

Encryption at rest is possible with CloudNativePG. The operator delegates that
to the underlying storage class. See the storage class for
information about this important security feature.

## Persistent Volume Claim (PVC)

The operator creates a PVC for each PostgreSQL instance, with the goal of
storing the `PGDATA`. It then mounts it into each pod.

Additionally, it supports creating clusters with:

- A separate PVC on which to store PostgreSQL WAL, as explained in
  [Volume for WAL](#volume-for-wal)
- Additional separate volumes reserved for PostgreSQL tablespaces, as explained
  in [Tablespaces](tablespaces.md)

In CloudNativePG, the volumes attached to a single PostgreSQL instance are
defined as a *PVC group*.

## Configuration via a storage class

:::info[Important]
    CloudNativePG was designed to work interchangeably with all storage classes.
    As usual, we recommend properly benchmarking the storage class in a
    controlled environment before deploying to production.
:::

The easiest way to configure the storage for a PostgreSQL class is to request
storage of a certain size, like in the following example:

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

Using the previous configuration, the generated PVCs are satisfied by the
default storage class. If the target Kubernetes cluster has no default storage
class, or even if you need your PVCs to be satisfied by a known storage class,
you can set it into the custom resource:

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
You can update the storageClass by [Re-creating the PVCs](#Re-creating-storage)


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

By default, PostgreSQL stores all its data in the so-called `PGDATA` (a
directory). One of the core directories inside `PGDATA` is `pg_wal`, which
contains the log of transactional changes that occurred in the database, in the
form of segment files. (`pg_wal` is historically known as `pg_xlog` in
PostgreSQL.)

:::info
    Normally, each segment is 16MB in size, but you can configure the size
    using the `walSegmentSize` option. This option is applied at cluster
    initialization time, as described in
    [Bootstrap an empty cluster](bootstrap.md#bootstrap-an-empty-cluster-initdb).
:::

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

:::note[Write-Ahead Log (WAL)]
    See [Reliability and the Write-Ahead Log](https://www.postgresql.org/docs/current/wal.html)
    in the PostgreSQL documentation for more information.
:::

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

:::info[Important]
    Removing `walStorage` isn't supported. Once added, a separate volume for
    WALs can't be removed from an existing Postgres cluster.
:::

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

Given the storage class supports volume expansion, you can change the size
requirement of the `Cluster`, and the operator applies the change to every PVC.

If the `StorageClass` supports [online volume resizing](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#resizing-an-in-use-persistentvolumeclaim),
the change is immediately applied to the pods. If the underlying storage class
doesn't support that, you must delete the pod to trigger the resize.

The best way to proceed is to delete one pod at a time, starting from replicas
and waiting for each pod to be back up.

## Re-creating storage

If the storage class doesn't support volume expansion or you need to migrate to a new storageClass, you can still regenerate
your cluster on different PVCs. Allocate new PVCs with increased storage and then move the database there. This operation is feasible only when the cluster
contains more than one node.

While you do that, you need to prevent the operator from changing the existing
PVC by disabling the `resizeInUseVolumes` flag, like in the following example:

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

To move the entire cluster to a different storage area, you need to re-create
all the PVCs and all the pods. Suppose you have a cluster with three replicas,
like in the following example:

```
$ kubectl get pods
NAME                READY   STATUS    RESTARTS   AGE
cluster-example-1   1/1     Running   0          2m37s
cluster-example-2   1/1     Running   0          2m22s
cluster-example-3   1/1     Running   0          2m10s
```

To re-create the cluster using different PVCs, you can edit the cluster
definition to disable `resizeInUseVolumes`. Then re-create every instance in a
different PVC.

For example, re-create the storage for `cluster-example-3`:

```
$ kubectl delete pvc/cluster-example-3 pod/cluster-example-3
```

:::info[Important]
    If you created a dedicated WAL volume, both PVCs must be deleted during
    this process. The same procedure applies if you want to regenerate the WAL
    volume PVC. You can do this by also disabling `resizeInUseVolumes` for the
    `.spec.walStorage` section.
:::

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

For recreating the PVC's on a new storageClass edit the cluster definition to include it:
```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: postgresql-storage-class
spec:
  instances: 3
  storage:
    storageClass: database
    size: 1Gi
```
Then delete the pods and PVC's one by one starting with the replicas, then promote the updated running replica to primary, after the new primary is healthy you can delete the old primary pod and PVCs.

If using NFS refer to [CREATING-CLUSTER-NFS](https://www.postgresql.org/docs/current/creating-cluster.html#CREATING-CLUSTER-NFS)


## Static provisioning of persistent volumes

CloudNativePG was designed to work with dynamic volume provisioning. This
capability allows storage volumes to be created on demand when requested by
users by way of storage classes and PVC templates.
See [Re-creating storage](#re-creating-storage).

However, in some cases, Kubernetes administrators prefer to manually create
storage volumes and then create the related `PersistentVolume` objects for
their representation inside the Kubernetes cluster. This is also known as
*pre-provisioning* of volumes.

:::info[Important]
    We recommend that you avoid pre-provisioning volumes, as it has an effect
    on the high availability and self-healing capabilities of the operator. It
    breaks the fully declarative model on which CloudNativePG was built.
:::

To use a pre-provisioned volume in CloudNativePG:

1. Manually create the volume outside Kubernetes.
2. Create the `PersistentVolume` object to match this volume using the
   correct parameters as required by the actual CSI driver (that is, `volumeHandle`,
   `fsType`, `storageClassName`, and so on).
3. Create the Postgres `Cluster` using, for each storage section, a coherent
   [`pvcTemplate`](storage.md#configuration-via-a-pvc-template)
   section that can help Kubernetes match the `PersistentVolume`
   and enable CloudNativePG to create the needed `PersistentVolumeClaim`.

:::warning
    With static provisioning, it's your responsibility to ensure that Postgres
    pods can be correctly scheduled by Kubernetes where a pre-provisioned volume
    exists. (The scheduling configuration is based on the affinity rules of your
    cluster.) Make sure you check for any pods stuck in `Pending` after you deploy
    the cluster. If the condition persists, investigate why it's happening.
:::

## Block storage considerations (Ceph/Longhorn)

Most block storage solutions in Kubernetes, such as Longhorn and Ceph,
recommend having multiple replicas of a volume to enhance resiliency. This
approach works well for workloads that lack built-in resiliency.

However, CloudNativePG integrates this resiliency directly into the Postgres
`Cluster` through the number of instances and the persistent volumes attached
to them, as explained in ["Synchronizing the state"](architecture.md#synchronizing-the-state).

As a result, defining additional replicas at the storage level can lead to
write amplification, unnecessarily increasing disk I/O and space usage.

For CloudNativePG usage, consider reducing the number of replicas at the block storage
level to one, while ensuring that no single point of failure (SPoF) exists at
the storage level for the entire `Cluster` resource. This typically means
ensuring that a single storage host—and ultimately, a physical disk—does not
host blocks from different instances of the same `Cluster`, in alignment with
the broader *shared-nothing architecture* principle.

In Longhorn, you can mitigate this risk by enabling strict-local data locality
when creating a custom storage class. Detailed instructions for creating a
volume with strict-local data locality are available [here](https://longhorn.io/docs/1.7.0/high-availability/data-locality/).
This setting ensures that a pod’s data volume resides on the same node as the
pod itself.

Additionally, your Postgres `Cluster` should have [pod anti-affinity rules](scheduling.md#isolating-postgresql-workloads)
in place to ensure that the operator deploys pods across different nodes,
allowing Longhorn to place the data volumes on the corresponding hosts. If
needed, you can manually relocate volumes in Longhorn by temporarily setting
the volume replica count to 2, reducing it afterward, and then removing the old
replica. If a host becomes corrupted, you can use the [`cnpg` plugin to destroy](kubectl-plugin.md#destroy)
the affected instance. CloudNativePG will then recreate the instance on another
host and replicate the data.

In Ceph, this can be configured through CRUSH rules. The documentation for
configuring CRUSH rules is available
[here](https://rook.io/docs/rook/latest-release/CRDs/Cluster/external-cluster/topology-for-external-mode/?h=topology).
These rules aim to ensure one volume per pod per node. You can also relocate
volumes by importing them into a different pool.
