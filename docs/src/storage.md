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
    Before you deploy a PostgreSQL cluster with Cloud Native PostgreSQL,
    ensure that the storage you are using is recommended for database
    workloads. Our advice is to clearly set performance expectations by
    first benchmarking the storage using tools such as [fio](https://fio.readthedocs.io/en/latest/fio_doc.html),
    and then the database using [pgbench](https://www.postgresql.org/docs/current/pgbench.html).

## Benchmarking Cloud Native PostgreSQL

EDB maintains [cnp-bench](https://github.com/EnterpriseDB/cnp-bench),
an open source set of guidelines and Helm charts for benchmarking Cloud Native PostgreSQL
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

## Persistent Volume Claim

The operator creates a persistent volume claim (PVC) for each PostgreSQL
instance, with the goal to store the `PGDATA`, and then mounts it into each Pod.

## Configuration via a storage class

The easier way to configure the storage for a PostgreSQL class is to just
request storage of a certain size, like in the following example:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
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
to satisfied by a known storage class, you can set it into the custom resource:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
metadata:
  name: postgresql-storage-class
spec:
  instances: 3
  storage:
    storageClass: standard
    size: 1Gi
```

!!! Important
    Cloud Native PostgreSQL has been designed to be storage class agnostic.
    As usual, our recommendation is to properly benchmark the storage class
    in a controlled environment, before hitting production.

## Configuration via a PVC template

To further customize the generated PVCs, you can provide a PVC template inside the Custom Resource,
like in the following example:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
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

### Workaround for volume expansion on AKS

This paragraph covers [Azure issue on AKS storage classes](https://github.com/Azure/AKS/issues/1477), that are supposed to support
online resizing, but they actually require the following workaround.

Let's suppose you have a cluster with three replicas:

```
$ kubectl get pods
NAME                READY   STATUS    RESTARTS   AGE
cluster-example-1   1/1     Running   0          2m37s
cluster-example-2   1/1     Running   0          2m22s
cluster-example-3   1/1     Running   0          2m10s
```

An Azure disk can only be expanded while in "unattached" state, as described in the 
[docs](https://github.com/kubernetes-sigs/azuredisk-csi-driver/blob/master/docs/known-issues/sizegrow.md).  <!-- wokeignore:rule=master -->
This means, that to resize a disk used by a PostgresSQL cluster, you will need to perform a manual rollout,
first cordoning the node that hosts the Pod using the PVC bound to the disk. This will prevent the Operator
to recreate the Pod and immediately reattach it to its PVC before the background disk resizing has been completed.

First step is to edit the cluster definition applying the new size, let's say "2Gi", as follows:

```
apiVersion: postgresql.k8s.enterprisedb.io/v1
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
    after promoting through a switchover a new resized Pod, using `kubectl cnp promote` 
    (e.g. `kubectl cnp promote cluster-example 3` to promote `cluster-example-3` to primary).

### Recreating storage

IF the storage class does not support volume expansion, you can still regenerate your cluster
on different PVCs, by allocating new PVCs with increased storage and then move the
database there. This operation is feasible only when the cluster contains more than one node.

While you do that, you need to prevent the operator from changing the existing PVC
by disabling the `resizeInUseVolumes` flag, like in the following example:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
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

Having done that, the operator will orchestrate the creation of another replica with a
resized PVC:

```
$ kubectl get pods
NAME                           READY   STATUS      RESTARTS   AGE
cluster-example-1              1/1     Running     0          5m58s
cluster-example-2              1/1     Running     0          5m43s
cluster-example-4-join-v2bfg   0/1     Completed   0          17s
cluster-example-4              1/1     Running     0          10s
```
