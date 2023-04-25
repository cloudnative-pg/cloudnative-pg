# Storage

**Storage is the most critical component in a database workload**.
Storage should be always available, scale, perform well,
and guarantee consistency and durability. The same expectations and
requirements that apply to traditional environments, such as virtual machines
and bare metal, are also valid in container contexts managed by Kubernetes.

!!! Important
    Kubernetes has its own specificities, when it comes to dynamically
    provisioned storage. These include *storage classes*, *persistent
    volumes*, and *persistent volume claims*. You need to own these
    concepts, on top of all the valuable knowledge you have built over
    the years in terms of storage for database workloads on VMs and
    physical servers.

There are two primary methods of access to storage:

- **network**: either directly or indirectly (think of an NFS volume locally mounted on a host running Kubernetes)
- **local**: directly attached to the node where a Pod is running (this also includes directly attached disks on bare metal installations of Kubernetes)

Network storage, which is the most common usage pattern in Kubernetes,
presents the same issues of throughput and latency that you can
experience in a traditional environment. These can be accentuated in
a shared environment, where I/O contention with several applications
increases the variability of performance results.

Local storage enables shared-nothing architectures, which is more suitable
for high transactional and Very Large DataBase (VLDB) workloads, as it
guarantees higher and more predictable performance.

!!! Warning
    Before you deploy a PostgreSQL cluster with CloudNativePG,
    ensure that the storage you are using is recommended for database
    workloads. Our advice is to clearly set performance expectations by
    first benchmarking the storage using tools such as [fio](https://fio.readthedocs.io/en/latest/fio_doc.html),
    and then the database using [pgbench](https://www.postgresql.org/docs/current/pgbench.html).

!!! Info
    CloudNativePG does not use `StatefulSet`s for managing data persistence.
    Rather, it manages persistent volume claims (PVCs) directly. If you want
    to know more, please read the
    ["Custom Pod Controller"](controller.md) document.

## Benchmarking CloudNativePG

EDB maintains [cnp-bench](https://github.com/EnterpriseDB/cnp-bench),
an open source set of guidelines and Helm charts for benchmarking CloudNativePG
in a controlled Kubernetes environment, before deploying the database in production.

Briefly, `cnp-bench` is designed to operate at two levels:

- measuring the performance of the underlying storage using `fio`, with relevant
  metrics for database workloads such as throughput for sequential reads, sequential
  writes, random reads and random writes
- measuring the performance of the database using the default benchmarking tool
  distributed along with PostgreSQL: `pgbench`

!!! Important
    Measuring both the storage and database performance is an activity that
    must be done **before the database goes in production**. However, such results
    are extremely valuable not only in the planning phase (e.g., capacity planning),
    but also in the production lifecycle, especially in emergency situations
    (when we don't have the luxury anymore to run this kind of tests). Databases indeed
    change and evolve over time, so does the distribution of data, potentially affecting
    performance: knowing the theoretical maximum throughput of sequential reads or
    writes will turn out to be extremely useful in those situations. Especially in
    shared-nothing contexts, where results do not vary due to the influence of external workloads.
    **Know your system, benchmark it.**

## Encryption at rest

Encryption at rest is possible with CloudNativePG. The operator delegates that
to the underlying storage class. Please refer to the storage class for
information about this important security feature.

## Persistent Volume Claim

The operator creates a persistent volume claim (PVC) for each PostgreSQL
instance, with the goal to store the `PGDATA`, and then mounts it into each Pod.

Additionally, it supports the creation of clusters with a separate PVC
on which to store PostgreSQL Write-Ahead Log (WAL), as explained in the
["Volume for WAL" section](#volume-for-wal) below.

In CloudNativePG, the volumes attached to a single PostgreSQL instance
are defined as **PVC group**.

## Configuration via a storage class

!!! Important
    CloudNativePG has been designed to be storage class agnostic.
    As usual, our recommendation is to properly benchmark the storage class
    in a controlled environment, before deploying to production.

The easier way to configure the storage for a PostgreSQL class is to just
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

Using the previous configuration, the generated PVCs will be satisfied by the default storage
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

To further customize the generated PVCs, you can provide a PVC template inside the Custom Resource,
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
One of the core directories inside `PGDATA` is `pg_wal` (historically
known as `pg_xlog` in PostgreSQL), which contains the log of transactional
changes occurred in the database, in the form of segment files.

!!! Info
    Normally, each segment is 16 MB in size, but the size can be configured
    through the `walSegmentSize` option, applied at cluster initialization time, as
    described in ["Bootstrap an empty cluster"](bootstrap.md#bootstrap-an-empty-cluster-initdb).

While in most cases, having `pg_wal` on the same volume where `PGDATA`
resides is fine, there are a few benefits from having WALs stored in a separate
volume:

- **I/O performance**: by storing WAL files on different storage than `PGDATA`,
  PostgreSQL can exploit parallel I/O for WAL operations (normally
  sequential writes) and for data files (tables and indexes for example), thus
  improving vertical scalability

- **more reliability**: by reserving dedicated disk space to WAL files, you
  can always be sure that exhaustion of space on the `PGDATA` volume will
  never interfere with WAL writing, ensuring that your PostgreSQL primary
  is correctly shut down.

- **finer control**: you can define the amount of space dedicated to both
  `PGDATA` and `pg_wal`, fine tune [WAL
  configuration](https://www.postgresql.org/docs/current/wal-configuration.html)
  and checkpoints, even use a different storage class for cost optimization

- **better I/O monitoring**: you can constantly monitor the load and disk usage
  on both `PGDATA` and `pg_wal`, and set proper alerts that notify you in case,
  for example, `PGDATA` requires resizing


!!! Seealso "Write-Ahead Log (WAL)"
    Please refer to the ["Reliability and the Write-Ahead Log" page](https://www.postgresql.org/docs/current/wal.html)
    from the official PostgreSQL documentation for more information.

You can add a separate volume for WAL through the `.spec.walStorage` option,
which follows the same rules described for the `storage` field and provisions a
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
    Removing `walStorage` is not supported: once added, a separate volume for
    WALs cannot be removed from an existing Postgres cluster.

## Volume expansion

Kubernetes exposes an API allowing [expanding PVCs](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#expanding-persistent-volumes-claims)
that is enabled by default but needs to be supported by the underlying `StorageClass`.

To check if a certain `StorageClass` supports volume expansion, you can read the `allowVolumeExpansion`
field for your storage class:

```
$ kubectl get storageclass -o jsonpath='{$.allowVolumeExpansion}' premium-storage
true
```

### Using the volume expansion Kubernetes feature

Given the storage class supports volume expansion, you can change the size requirement
of the `Cluster`, and the operator will apply the change to every PVC.

If the `StorageClass` supports [online volume resizing](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#resizing-an-in-use-persistentvolumeclaim)
the change is immediately applied to the Pods. If the underlying Storage Class doesn't support
that, you will need to delete the Pod to trigger the resize.

The best way to proceed is to delete one Pod at a time, starting from replicas and waiting
for each Pod to be back up.

### Expanding PVC volumes on AKS

At the moment, [Azure is not able to resize the PVC's volume without restarting the pod](https://github.com/Azure/AKS/issues/1477).
CloudNativePG has overcome this limitation through the
`ENABLE_AZURE_PVC_UPDATES` environment variable in the
[operator configuration](operator_conf.md#available-options).
When set to `'true'`, CloudNativePG triggers a rolling update of the
Postgres cluster.

Alternatively, you can follow the workaround below to manually resize the
volume in AKS.

#### Workaround for volume expansion on AKS

You can manually resize a PVC on AKS by following these procedures.
As an example, let's suppose you have a cluster with 3 replicas:

```
$ kubectl get pods
NAME                READY   STATUS    RESTARTS   AGE
cluster-example-1   1/1     Running   0          2m37s
cluster-example-2   1/1     Running   0          2m22s
cluster-example-3   1/1     Running   0          2m10s
```

An Azure disk can only be expanded while in "unattached" state, as described in the
[docs](https://github.com/kubernetes-sigs/azuredisk-csi-driver/blob/master/docs/known-issues/sizegrow.md).  <!-- wokeignore:rule=master -->
This means, that to resize a disk used by a PostgreSQL cluster, you will need to perform a manual rollout,
first cordoning the node that hosts the Pod using the PVC bound to the disk. This will prevent the Operator
to recreate the Pod and immediately reattach it to its PVC before the background disk resizing has been completed.

First step is to edit the cluster definition applying the new size, let's say "2Gi", as follows:

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

Assuming the `cluster-example-1` Pod is the cluster's primary, we can proceed with the replicas first.
For example start with cordoning the kubernetes node that hosts the `cluster-example-3` Pod:

```
kubectl cordon <node of cluster-example-3>
```

Then delete the `cluster-example-3` Pod:

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

Wait for the Pod to be recreated correctly and get in Running and Ready state:

```
kubectl get pods -w cluster-example-3
cluster-example-3   0/1     Init:0/1   0          12m
cluster-example-3   1/1     Running   0          12m
```

Now verify the PVC expansion by running the following command, which should return "2Gi" as configured:

```
kubectl get pvc cluster-example-3 -o=jsonpath='{.status.capacity.storage}'
```

So, you can repeat these steps for the remaining Pods.

!!! Important
    Please leave the resizing of the disk associated with the primary instance as last disk,
    after promoting through a switchover a new resized Pod, using `kubectl cnpg promote`
    (e.g. `kubectl cnpg promote cluster-example 3` to promote `cluster-example-3` to primary).

### Recreating storage

If the storage class does not support volume expansion, you can still regenerate your cluster
on different PVCs, by allocating new PVCs with increased storage and then move the
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

In order to move the entire cluster to a different storage area, you need to recreate all the PVCs and
all the Pods. Let's suppose you have a cluster with three replicas like in the following
example:

```
$ kubectl get pods
NAME                READY   STATUS    RESTARTS   AGE
cluster-example-1   1/1     Running   0          2m37s
cluster-example-2   1/1     Running   0          2m22s
cluster-example-3   1/1     Running   0          2m10s
```

To recreate the cluster using different PVCs, you can edit the cluster definition to disable
`resizeInUseVolumes`, and then recreate every instance in a different PVC.

As an example, to recreate the storage for `cluster-example-3` you can:

```
$ kubectl delete pvc/cluster-example-3 pod/cluster-example-3
```

!!! Important
    In case you have created a dedicated WAL volume, both PVCs will have to be deleted during this process.
    Additionally, the same procedure applies in case you want to regenerate the WAL volume PVC, which can be done
    by disabling `resizeInUseVolumes` also for the `.spec.walStorage` section.

For example (in case a PVC dedicated to WAL storage is present):

```
$ kubectl delete pvc/cluster-example-3 pvc/cluster-example-3-wal pod/cluster-example-3
```

Having done that, the operator will orchestrate the creation of another replica with a
resized PVC:

```
$ kubectl get pods
NAME                           READY   STATUS      RESTARTS   AGE
cluster-example-1              1/1     Running     0          5m58s
cluster-example-2              1/1     Running     0          5m43s
cluster-example-4-join-v2      0/1     Completed   0          17s
cluster-example-4              1/1     Running     0          10s
```
