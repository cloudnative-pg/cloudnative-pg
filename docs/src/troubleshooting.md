# Troubleshooting

In this page, you can find some basic information on how to troubleshoot Cloud
Native PostgreSQL in your Kubernetes cluster deployment.

!!! Hint
    As a Kubernetes administrator, you should have the
    [`kubectl` Cheat Sheet](https://kubernetes.io/docs/reference/kubectl/cheatsheet/) page
    bookmarked!

## Before you start

### Kubernetes environment

What can make a difference in a troubleshooting activity is to provide
clear information about the underlying Kubernetes system.

Make sure you know:

- the Kubernetes distribution and version you are using
- the specifications of the nodes where PostgreSQL is running
- as much as you can about the actual [storage](storage.md), including storage
  class and benchmarks you have done before going into production.
- which relevant Kubernetes applications you are using in your cluster (i.e.
  Prometheus, Grafana, Istio, Certmanager, ...)

### Useful utilities

On top of the mandatory `kubectl` utility, for troubleshooting, we recommend the
following plugins/utilities to be available in your system:

- [`cnp` plugin](cnp-plugin.md) for `kubectl`
- [`jq`](https://stedolan.github.io/jq/), a lightweight and flexible command-line JSON processor

### Logs

Every resource created and controlled by Cloud Native PostgreSQL logs to
standard output, as expected by Kubernetes, and directly in [JSON
format](logging.md). As a result, you should rely on the `kubectl logs`
command to retrieve logs from a given resource.

For more information, type:

```shell
kubectl logs --help
```

!!! Hint
    JSON logs are great for machine reading, but hard to read for human beings.
    Our recommendation is to use the `jq` command to improve usability. For
    example, you can *pipe* the `kubectl logs` command with `| jq -C`.

!!! Note
    In the sections below, we will show some examples on how to retrieve logs
    about different resources when it comes to troubleshooting Cloud Native
    PostgreSQL.

## Operator information

By default, the Cloud Native PostgreSQL operator is installed in the
`postgresql-operator-system` namespace in Kubernetes as a `Deployment`
(see the ["Details about the deployment" section](installation_upgrade.md#details-about-the-deployment)
for details).

You can get a list of the operator pods by running:

```shell
kubectl get pods -n postgresql-operator-system
```

!!! Note
    Under normal circumstances, you should have one pod where the operator is
    running, identified by a name starting with `postgresql-operator-controller-manager-`.
    In case you have set up your operator for high availability, you should have more entries.
    Those pods are managed by a deployment named `postgresql-operator-controller-manager`.

Collect the relevant information about the operator that is running in pod
`<POD>` with:

```shell
kubectl describe pod -n postgresql-operator-system <POD>
```

Then get the logs from the same pod by running:

```shell
kubectl get logs -n postgresql-operator-system <POD>
```

## Cluster information

You can check the status of the `<CLUSTER>` cluster in the `NAMESPACE`
namespace with:

```shell
kubectl get cluster -n <NAMESPACE> <CLUSTER>
```

Output:

```console
NAME        AGE        INSTANCES   READY   STATUS                     PRIMARY
<CLUSTER>   10d4h3m    3           3       Cluster in healthy state   <CLUSTER>-1
```

The above example reports a healthy PostgreSQL cluster of 3 instances, all in
*ready* state, and with `<CLUSTER>-1` being the primary.

In case of unhealthy conditions, you can discover more by getting the manifest
of the `Cluster` resource:

```shell
kubectl get cluster -o yaml -n <NAMESPACE> <CLUSTER>
```

Another important command to gather is the `status` one, as provided by the
`cnp` plugin:

```shell
kubectl cnp status -n <NAMESPACE> <CLUSTER>
```

!!! Tip
    You can print more information by adding the `--verbose` option.

## Pod information

You can retrieve the list of instances that belong to a given PostgreSQL
cluster with:

```shell
# using labels available from CNP 1.10.0
kubectl get pod -l k8s.enterprisedb.io/cluster=<CLUSTER> -L role -n <NAMESPACE>
# using legacy labels
kubectl get pod -l postgresql=<CLUSTER> -L role -n <NAMESPACE>
```

Output:

```console
NAME          READY   STATUS    RESTARTS   AGE       ROLE
<CLUSTER>-1   1/1     Running   0          10d4h5m   primary
<CLUSTER>-2   1/1     Running   0          10d4h4m   replica
<CLUSTER>-3   1/1     Running   0          10d4h4m   replica
```

You can check if/how a pod is failing by running:

```shell
kubectl get pod -n <NAMESPACE> -o yaml <CLUSTER>-<N>
```

You can get all the logs for a given PostgreSQL instance with:

```shell
kubectl logs -n <NAMESPACE> <CLUSTER>-<N>
```

If you want to limit the search to the PostgreSQL process only, you can run:

```shell
kubectl logs -n <NAMESPACE> <CLUSTER>-<N> \
  | jq 'select(.logger=="postgres") | .record.message'
```

The following example also adds the timestamp in a user-friendly format:

```shell
kubectl logs -n <NAMESPACE> <CLUSTER>-<N> \
  | jq -r 'select(.logger=="postgres") | [(.ts|strflocaltime("%Y-%m-%dT%H:%M:%S %Z")), .record.message] | @csv'
```

## Backup information

You can list the backups that have been created for a named cluster with:

```shell
kubectl get backup -l k8s.enterprisedb.io/cluster=<CLUSTER>
```

!!! Important
    Backup labelling has been introduced in version 1.10.0 of Cloud Native
    PostgreSQL. So only those resources that have been created with that version or
    a higher one will contain such a label.

## Storage information

Sometimes it might be useful to gather more information about the underlying
storage class used in the cluster. You can execute the following operation on
any of the pods that are part of the PostgreSQL cluster:

```shell
STORAGECLASS=$(kubectl get pvc <POD> -o jsonpath='{.spec.storageClassName}')
kubectl get storageclasses $STORAGECLASS -o yaml
```

## Node information

Kubernetes nodes is where ultimately PostgreSQL pods will be running. It's
strategically important to know as much as we can about them.

You can get the list of nodes in your Kubernetes cluster with:

```shell
# look at the worker nodes and their status
kubectl get nodes -o wide
```

Additionally, you can gather the list of nodes where the pods of a given
cluster are running with:

```shell
kubectl get pod -l k8s.enterprisedb.io/clusterName=<CLUSTER> \
  -L role -n <NAMESPACE> -o wide
```

The latter is important to understand where your pods are distributed - very
useful if you are using [affinity/anti-affinity rules and/or tolerations](affinity.md).

## Some common issues

### Storage is full

If one or more pods in the cluster are in `CrashloopBackoff` and logs
suggest this could be due to a full disk, you probably have to increase the
size of the instance's `PersistentVolumeClaim`. Please look at the
["Volume expansion" section](storage.md#volume-expansion) in the documentation.

### Pods are stuck in `Pending` state

In case a Cluster's instance is stuck in the `Pending` phase, you should check
the pod's `Events` section to get an idea of the reasons behind this:

```shell
kubectl describe pod -n <NAMESPACE> <POD>
```

Some of the possible causes for this are:

- No nodes are matching the `nodeSelector`
- Tolerations are not correctly configured to match the nodes' taints
- No nodes are available at all: this could also be related to
  `cluster-autoscaler` hitting some limits, or having some temporary issues

In this case, it could also be useful to check events in the namespace:

```shell
kubectl get events -n <NAMESPACE>
# list events in chronological order
kubectl get events -n <NAMESPACE> --sort-by=.metadata.creationTimestamp
```

### Replicas out of sync when no backup is configured

Sometimes replicas might be switched off for a bit of time due to maintenance
reasons (think of when a Kubernetes nodes is drained). In case your cluster
does not have backup configured, when replicas come back up, they might
require a WAL file that is not present anymore on the primary (having been
already recycled according to the WAL management policies as mentioned in
["The `postgresql` section"](postgresql_conf.md#the-postgresql-section)), and
fall out of synchronization.

Similarly, when `pg_rewind` might require a WAL file that is not present
anymore in the former primary, reporting `pg_rewind: error: could not open file`.

In these cases, pods cannot become ready anymore and you are required to delete
the PVC and let the operator rebuild the replica.

If you rely on dynamically provisioned Persistent Volumes, and you are confident
in deleting the PV itself, you can do so with:

```shell
PODNAME=<POD>
VOLNAME=$(kubectl get pv -o json | \
  jq -r '.items[]|select(.spec.claimRef.name=='\"$PODNAME\"')|.metadata.name')

kubectl delete pod/$PODNAME pvc/$PODNAME pv/$VOLNAME
```
