---
id: troubleshooting
sidebar_position: 410
title: Troubleshooting
---

# Troubleshooting
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

In this page, you can find some basic information on how to troubleshoot
CloudNativePG in your Kubernetes cluster deployment.

:::tip[Hint]
    As a Kubernetes administrator, you should have the
    [`kubectl` Cheat Sheet](https://kubernetes.io/docs/reference/kubectl/cheatsheet/) page
    bookmarked!
:::

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
- the situation of continuous backup, in particular if it's in place and working
  correctly: in case it is not, make sure you take an [emergency backup](#emergency-backup)
  before performing any potential disrupting operation

### Useful utilities

On top of the mandatory `kubectl` utility, for troubleshooting, we recommend the
following plugins/utilities to be available in your system:

- [`cnpg` plugin](kubectl-plugin.md) for `kubectl`
- [`jq`](https://stedolan.github.io/jq/), a lightweight and flexible command-line JSON processor
- [`grep`](https://www.gnu.org/software/grep/), searches one or more input files
  for lines containing a match to a specified pattern. It is already available in most \*nix distros.
  If you are on Windows OS, you can use [`findstr`](https://docs.microsoft.com/en-us/windows-server/administration/windows-commands/findstr) as an alternative to `grep` or directly use [`wsl`](https://docs.microsoft.com/en-us/windows/wsl/)
  and install your preferred *nix distro and use the tools mentioned above.

## First steps

To quickly get an overview of the cluster or installation, the `kubectl` plugin
is the primary tool to use:

1. the [status subcommand](kubectl-plugin.md#status) provides an overview of a
  cluster
2. the [report subcommand](kubectl-plugin.md#report) provides the manifests
  for clusters and the operator deployment. It can also include logs using
  the `--logs` option.
  The report generated via the plugin will include the full cluster manifest.

The plugin can be installed on air-gapped systems via packages.
Please refer to the [plugin document](kubectl-plugin.md) for complete instructions.

## Are there backups?

After getting the cluster manifest with the plugin, you should verify if backups
are set up and working.

Before proceeding with troubleshooting operations, it may be advisable
to perform an emergency backup depending on your findings regarding backups.
Refer to the following section for instructions.

It is **extremely risky** to operate a production database without keeping
regular backups.

## Emergency backup

In some emergency situations, you might need to take an emergency logical
backup of the main `app` database.

:::info[Important]
    The instructions you find below must be executed only in emergency situations
    and the temporary backup files kept under the data protection policies
    that are effective in your organization. The dump file is indeed stored
    in the client machine that runs the `kubectl` command, so make sure that
    all protections are in place and you have enough space to store the
    backup file.
:::

The following example shows how to take a logical backup of the `app` database
in the `cluster-example` Postgres cluster, from the `cluster-example-1` pod:

```sh
kubectl exec cluster-example-1 -c postgres \
  -- pg_dump -Fc -d app > app.dump
```

:::note
    You can easily adapt the above command to backup your cluster, by providing
    the names of the objects you have used in your environment.
:::

The above command issues a `pg_dump` command in custom format, which is the most
versatile way to take [logical backups in PostgreSQL](https://www.postgresql.org/docs/current/app-pgdump.html).

The next step is to restore the database. We assume that you are operating
on a new PostgreSQL cluster that's been just initialized (so the `app` database
is empty).

The following example shows how to restore the above logical backup in the
`app` database of the `new-cluster-example` Postgres cluster, by connecting to
the primary (`new-cluster-example-1` pod):

```sh
kubectl exec -i new-cluster-example-1 -c postgres \
  -- pg_restore --no-owner --role=app -d app --verbose < app.dump
```

:::info[Important]
    The example in this section assumes that you have no other global objects
    (databases and roles) to dump and restore, as per our recommendation. In case
    you have multiple roles, make sure you have taken a backup using `pg_dumpall -g`
    and you manually restore them in the new cluster. In case you have multiple
    databases, you need to repeat the above operation one database at a time, making
    sure you assign the right ownership. If you are not familiar with PostgreSQL,
    we advise that you do these critical operations under the guidance of
    a professional support company.
:::

The above steps might be integrated into the `cnpg` plugin at some stage in the future.

## Logs

All resources created and managed by CloudNativePG log to standard output in
accordance with Kubernetes conventions, using [JSON format](logging.md).

While logs are typically processed at the infrastructure level and include
those from CloudNativePG, accessing logs directly from the command line
interface is critical during troubleshooting. You have three primary options
for doing so:

- Use the `kubectl logs` command to retrieve logs from a specific resource, and
  apply `jq` for better readability.
- Use the [`kubectl cnpg logs` command](kubectl-plugin.md#logs) for
  CloudNativePG-specific logging.
- Leverage specialized open-source tools like
  [stern](https://github.com/stern/stern), which can aggregate logs from
  multiple resources (e.g., all pods in a PostgreSQL cluster by selecting the
  `cnpg.io/clusterName` label), filter log entries, customize output formats,
  and more.

:::note
    The following sections provide examples of how to retrieve logs for various
    resources when troubleshooting CloudNativePG.
:::

## Operator information

By default, the CloudNativePG operator is installed in the
`cnpg-system` namespace in Kubernetes as a `Deployment`
(see the ["Details about the deployment" section](installation_upgrade.md#details-about-the-deployment)
for details).

You can get a list of the operator pods by running:

```shell
kubectl get pods -n cnpg-system
```

:::note
    Under normal circumstances, you should have one pod where the operator is
    running, identified by a name starting with `cnpg-controller-manager-`.
    In case you have set up your operator for high availability, you should have more entries.
    Those pods are managed by a deployment named `cnpg-controller-manager`.
:::

Collect the relevant information about the operator that is running in pod
`<POD>` with:

```shell
kubectl describe pod -n cnpg-system <POD>
```

Then get the logs from the same pod by running:

```shell
kubectl logs -n cnpg-system <POD>
```

### Gather more information about the operator

Get logs from all pods in CloudNativePG operator Deployment
(in case you have a multi operator deployment) by running:

```shell
kubectl logs -n cnpg-system \
  deployment/cnpg-controller-manager --all-containers=true
```

:::tip
    You can add `-f` flag to above command to follow logs in real time.
:::

Save logs to a JSON file by running:

```shell
kubectl logs -n cnpg-system \
  deployment/cnpg-controller-manager --all-containers=true | \
  jq -r . > cnpg_logs.json
```

Get CloudNativePG operator version by using `kubectl-cnpg` plugin:

```shell
kubectl-cnpg status <CLUSTER>
```

Output:

```shell
Cluster in healthy state
Name:               cluster-example
Namespace:          default
System ID:          7044925089871458324
PostgreSQL Image:   ghcr.io/cloudnative-pg/postgresql:18.1-system-trixie
Primary instance:   cluster-example-1
Instances:          3
Ready instances:    3
Current Write LSN:  0/5000000 (Timeline: 1 - WAL File: 000000010000000000000004)

Continuous Backup status
Not configured

Streaming Replication status
Name               Sent LSN   Write LSN  Flush LSN  Replay LSN  Write Lag       Flush Lag       Replay Lag      State      Sync State  Sync Priority
----               --------   ---------  ---------  ----------  ---------       ---------       ----------      -----      ----------  -------------
cluster-example-2  0/5000000  0/5000000  0/5000000  0/5000000   00:00:00        00:00:00        00:00:00        streaming  async       0
cluster-example-3  0/5000000  0/5000000  0/5000000  0/5000000   00:00:00.10033  00:00:00.10033  00:00:00.10033  streaming  async       0

Instances status
Name               Database Size  Current LSN  Replication role  Status  QoS         Manager Version
----               -------------  -----------  ----------------  ------  ---         ---------------
cluster-example-1  33 MB          0/5000000    Primary           OK      BestEffort  1.12.0
cluster-example-2  33 MB          0/5000000    Standby (async)   OK      BestEffort  1.12.0
cluster-example-3  33 MB          0/5000060    Standby (async)   OK      BestEffort  1.12.0
```

## Cluster information

You can check the status of the `<CLUSTER>` cluster in the `NAMESPACE`
namespace with:

```shell
kubectl get cluster -n <NAMESPACE> <CLUSTER>
```

Output:

```shell
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
`cnpg` plugin:

```shell
kubectl cnpg status -n <NAMESPACE> <CLUSTER>
```

:::tip
    You can print more information by adding the `--verbose` option.
:::

Get PostgreSQL container image version:

```shell
kubectl describe cluster <CLUSTER_NAME> -n <NAMESPACE> | grep "Image Name"
```

Output:

```shell
  Image Name:    ghcr.io/cloudnative-pg/postgresql:18.1-system-trixie
```

:::note
    Also you can use `kubectl-cnpg status -n <NAMESPACE> <CLUSTER_NAME>`
    to get the same information.
:::

## Pod information

You can retrieve the list of instances that belong to a given PostgreSQL
cluster with:

```shell
kubectl get pod -l cnpg.io/cluster=<CLUSTER> -L role -n <NAMESPACE>
```

Output:

```shell
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
kubectl logs -n <NAMESPACE> <CLUSTER>-<N> | \
  jq 'select(.logger=="postgres") | .record.message'
```

The following example also adds the timestamp:

```shell
kubectl logs -n <NAMESPACE> <CLUSTER>-<N> | \
  jq -r 'select(.logger=="postgres") | [.ts, .record.message] | @csv'
```

If the timestamp is displayed in Unix Epoch time, you can convert it to a user-friendly format:

```shell
kubectl logs -n <NAMESPACE> <CLUSTER>-<N> | \
  jq -r 'select(.logger=="postgres") | [(.ts|strflocaltime("%Y-%m-%dT%H:%M:%S %Z")), .record.message] | @csv'
```

### Gather and filter extra information about PostgreSQL pods

Check logs from a specific pod that has crashed:

```shell
kubectl logs -n <NAMESPACE> --previous <CLUSTER>-<N>
```

Get FATAL errors from a specific PostgreSQL pod:

```shell
kubectl logs -n <NAMESPACE> <CLUSTER>-<N> | \
  jq -r '.record | select(.error_severity == "FATAL")'
```

Output:

```json
{
  "log_time": "2021-11-08 14:07:44.520 UTC",
  "user_name": "streaming_replica",
  "process_id": "68",
  "connection_from": "10.244.0.10:60616",
  "session_id": "61892f30.44",
  "session_line_num": "1",
  "command_tag": "startup",
  "session_start_time": "2021-11-08 14:07:44 UTC",
  "virtual_transaction_id": "3/75",
  "transaction_id": "0",
  "error_severity": "FATAL",
  "sql_state_code": "28000",
  "message": "role \"streaming_replica\" does not exist",
  "backend_type": "walsender"
}
```

Filter PostgreSQL DB error messages in logs for a specific pod:

```shell
kubectl logs -n <NAMESPACE> <CLUSTER>-<N> | jq -r '.err | select(. != null)'
```

Output:

```shell
dial unix /controller/run/.s.PGSQL.5432: connect: no such file or directory
```

Get messages matching `err` word from a specific pod:

```shell
kubectl logs -n <NAMESPACE> <CLUSTER>-<N> | jq -r '.msg' | grep "err"
```

Output:

```shell
2021-11-08 14:07:39.610 UTC [15] LOG:  ending log output to stderr
```

Get all logs from PostgreSQL process from a specific pod:

```shell
kubectl logs -n <NAMESPACE> <CLUSTER>-<N> | \
  jq -r '. | select(.logger == "postgres") | select(.msg != "record") | .msg'
```
Output:

```shell
2021-11-08 14:07:52.591 UTC [16] LOG:  redirecting log output to logging collector process
2021-11-08 14:07:52.591 UTC [16] HINT:  Future log output will appear in directory "/controller/log".
2021-11-08 14:07:52.591 UTC [16] LOG:  ending log output to stderr
2021-11-08 14:07:52.591 UTC [16] HINT:  Future log output will go to log destination "csvlog".
```

Get pod logs filtered by fields with values and join them separated by `|` running:

```shell
kubectl logs -n <NAMESPACE> <CLUSTER>-<N> | \
  jq -r '[.level, .ts, .logger, .msg] | join(" | ")'
```

Output:

```shell
info | 1636380469.5728037 | wal-archive | Backup not configured, skip WAL archiving
info | 1636383566.0664876 | postgres | record
```

## Backup information

You can list the backups that have been created for a named cluster with:

```shell
kubectl get backup -l cnpg.io/cluster=<CLUSTER>
```

## Storage information

Sometimes is useful to double-check the StorageClass used by the cluster to have
some more context during investigations or troubleshooting, like this:

```shell
STORAGECLASS=$(kubectl get pvc <POD> -o jsonpath='{.spec.storageClassName}')
kubectl get storageclasses $STORAGECLASS -o yaml
```

We are taking the StorageClass from one of the cluster pod here since often
clusters are created using the default StorageClass.

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
kubectl get pod -l cnpg.io/cluster=<CLUSTER> \
  -L role -n <NAMESPACE> -o wide
```

The latter is important to understand where your pods are distributed - very
useful if you are using [affinity/anti-affinity rules and/or tolerations](scheduling.md).

## Conditions

Like many native kubernetes
objects [like here](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-conditions), 
Cluster exposes `status.conditions` as well. This allows one to 'wait' for a particular 
event to occur instead of relying on the overall cluster health state. Available conditions as of now are:

- LastBackupSucceeded
- ContinuousArchiving
- Ready

`LastBackupSucceeded` is reporting the status of the latest backup. If set to `True` the
last backup has been taken correctly, it is set to `False` otherwise.

`ContinuousArchiving` is reporting the status of the WAL archiving. If set to `True` the
last WAL archival process has been terminated correctly, it is set to `False` otherwise.

`Ready` is `True` when the cluster has the number of instances specified by the user
and the primary instance is ready. This condition can be used in scripts to wait for
the cluster to be created.

### How to wait for a particular condition

- Backup:
```bash
$ kubectl wait --for=condition=LastBackupSucceeded cluster/<CLUSTER-NAME> -n <NAMESPACE>
```

- ContinuousArchiving:
```bash
$ kubectl wait --for=condition=ContinuousArchiving cluster/<CLUSTER-NAME> -n <NAMESPACE>
```

- Ready (Cluster is ready or not):
```bash
$ kubectl wait --for=condition=Ready cluster/<CLUSTER-NAME> -n <NAMESPACE>
```
Below is a snippet of a `cluster.status` that contains a failing condition.

```bash
$ kubectl get cluster/<cluster-name> -o yaml
.
.
.
  status:
    conditions:
    - message: 'unexpected failure invoking barman-cloud-wal-archive: exit status
        2'
      reason: ContinuousArchivingFailing
      status: "False"
      type: ContinuousArchiving

    - message: exit status 2
      reason: LastBackupFailed
      status: "False"
      type: LastBackupSucceeded

    - message: Cluster Is Not Ready
      reason: ClusterIsNotReady
      status: "False"
      type: Ready


```

## Networking

CloudNativePG requires basic networking and connectivity in place.
You can find more information in the [networking](networking.md) section.

If installing CloudNativePG in an existing environment, there might be
network policies in place, or other network configuration made specifically
for the cluster, which could have an impact on the required connectivity
between the operator and the cluster pods and/or the between the pods.

You can look for existing network policies with the following command:

``` sh
kubectl get networkpolicies
```

There might be several network policies set up by the Kubernetes network
administrator.

``` sh
$ kubectl get networkpolicies                       
NAME                   POD-SELECTOR                      AGE
allow-prometheus       cnpg.io/cluster=cluster-example   47m
default-deny-ingress   <none>                            57m
```

## PostgreSQL core dumps

Although rare, PostgreSQL can sometimes crash and generate a core dump
in the `PGDATA` folder. When that happens, normally it is a bug in PostgreSQL
(and most likely it has already been solved - this is why it is important
to always run the latest minor version of PostgreSQL).

CloudNativePG allows you to control what to include in the core dump through
the `cnpg.io/coredumpFilter` annotation.

:::info
    Please refer to ["Labels and annotations"](labels_annotations.md)
    for more details on the standard annotations that CloudNativePG provides.
:::

By default, the `cnpg.io/coredumpFilter` is set to `0x31` in order to
exclude shared memory segments from the dump, as this is the safest
approach in most cases.

:::info
    Please refer to
    ["Core dump filtering settings" section of "The `/proc` Filesystem" page of the Linux Kernel documentation](https://docs.kernel.org/filesystems/proc.html#proc-pid-coredump-filter-core-dump-filtering-settings).
    for more details on how to set the bitmask that controls the core dump filter.
:::

:::info[Important]
    Beware that this setting only takes effect during Pod startup and that changing
    the annotation doesn't trigger an automated rollout of the instances.
:::

Although you might not personally be involved in inspecting core dumps,
you might be asked to provide them so that a Postgres expert can look
into them. First, verify that you have a core dump in the `PGDATA`
directory with the following command (please run it against the
correct pod where the Postgres instance is running):

```sh
kubectl exec -ti POD -c postgres \
  -- find /var/lib/postgresql/data/pgdata -name 'core.*'
```

Under normal circumstances, this should return an empty set. Suppose, for
example, that we have a core dump file:

```
/var/lib/postgresql/data/pgdata/core.14177
```

Once you have verified the space on disk is sufficient, you can collect the
core dump on your machine through `kubectl cp` as follows:

```sh
kubectl cp POD:/var/lib/postgresql/data/pgdata/core.14177 core.14177
```

You now have the file. Make sure you free the space on the server by
removing the core dumps.

## Visualizing and Analyzing Profiling Data

CloudNativePG integrates with [pprof](https://github.com/google/pprof) to
collect and analyze profiling data at two levels:

- **Operator level** – enable by adding the `--pprof-server=true` option to the
  operator deployment (see [Operator configuration](operator_conf.md#profiling-tools)).
- **Postgres cluster level** – enable by adding the
  `alpha.cnpg.io/enableInstancePprof` annotation to a `Cluster` resource
  (described below).

When the `alpha.cnpg.io/enableInstancePprof` annotation is set to `"true"`,
each instance pod exposes a Go pprof HTTP server provided by the instance
manager.

- The server listens on `0.0.0.0:6060` inside the pod.
- A container port named `pprof` (`6060/TCP`) is automatically added to the pod
  spec.

You can disable pprof at any time by either removing the annotation or setting
it to `"false"`. The operator will roll out changes automatically to remove the
pprof port and flag.

:::info[Important]
    The pprof server only serves plain HTTP on port `6060`.
:::

### Example

Enable pprof on a cluster by adding the annotation:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
  annotations:
    alpha.cnpg.io/enableInstancePprof: "true"
spec:
  instances: 3
  # ...
```

Changing this annotation updates the instance pod spec (adds port `6060` and
the corresponding flag) and triggers a rolling update.

:::warning
    The example below uses `kubectl port-forward` for local testing only.
    This is **not** the intended way to expose the feature in production.
    Treat pprof as a sensitive debugging interface and never expose it publicly.
    If you must access it remotely, secure it with proper network policies and access controls.
:::

Use port-forwarding to access the pprof endpoints:

```bash
kubectl port-forward -n <namespace> pod/<instance-pod> 6060
curl -sS http://localhost:6060/debug/pprof/
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
```

You can also access pprof using the browser at [http://localhost:6060/debug/pprof/](http://localhost:6060/debug/pprof/).

### Troubleshooting

First, verify that the cluster has the `alpha.cnpg.io/enableInstancePprof:
"true"` annotation set.

Next, check that the instance manager command includes the `--pprof-server`
flag and that port `6060/TCP` is exposed. You can do this by running:

```bash
kubectl -n <namespace> describe pod <instance-pod>
```

Then review the **Command** and **Ports** sections in the output.

Finally, if you are not using port-forwarding, make sure that your
NetworkPolicies allow access to port `6060/TCP`.

## Some known issues

### Storage is full

In case the storage is full, the PostgreSQL pods will not be able to write new
data, or, in case of the disk containing the WAL segments being full, PostgreSQL
will shut down.

If you see messages in the logs about the disk being full, you should increase
the size of the affected PVC. You can do this by editing the PVC and changing
the `spec.resources.requests.storage` field. After that, you should also update
the Cluster resource with the new size to apply the same change to all the pods.
Please look at the ["Volume expansion" section](storage.md#volume-expansion) in the documentation.

If the space for WAL segments is exhausted, the pod will be crash-looping and
the cluster status will report `Not enough disk space`. Increasing the size in
the PVC and then in the Cluster resource will solve the issue. See also
the ["Disk Full Failure" section](instance_manager.md#disk-full-failure)


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

In these cases, pods cannot become ready anymore, and you are required to delete
the PVC and let the operator rebuild the replica.

If you rely on dynamically provisioned Persistent Volumes, and you are confident
in deleting the PV itself, you can do so with:

```shell
PODNAME=<POD>
VOLNAME=$(kubectl get pv -o json | \
  jq -r '.items[]|select(.spec.claimRef.name=='\"$PODNAME\"')|.metadata.name')

kubectl delete pod/$PODNAME pvc/$PODNAME pvc/$PODNAME-wal pv/$VOLNAME
```

### Cluster stuck in `Creating new replica`

Cluster is stuck in "Creating a new replica", while pod logs don't show
relevant problems.
This has been found to be related to the next issue
[on connectivity](#networking-is-impaired-by-installed-network-policies).
Networking issues are reflected in the status column as follows:

``` text
Instance Status Extraction Error: HTTP communication issue
```

### Understanding stuck reconciliation error messages

CloudNativePG automatically detects and recovers from stuck reconciliation states
by cleaning up failed or stuck jobs. When this happens, detailed troubleshooting
information is preserved in the cluster status that you can view with:

```sh
kubectl cnpg status <CLUSTER_NAME>
```

Or directly from the cluster custom resource:

```sh
kubectl get cluster <CLUSTER_NAME> -o yaml | grep -A5 -B5 "phase\|phaseReason"
```

For a more focused view of just the status information:

```sh
kubectl get cluster <CLUSTER_NAME> -o jsonpath='{.status.phase}: {.status.phaseReason}{"\n"}'
```

The enhanced error messages help you understand what went wrong before the
automatic cleanup occurred:

**Failed Jobs**: When a job fails during scaling operations:
``` text
Phase Reason: Recovering from failed job cluster-4-snapshot-recovery (active:0 succeeded:0 failed:1 age:15m2s)
```

**Stuck Jobs with Missing PVCs**: When jobs can't start due to missing storage:
``` text
Phase Reason: Recovering from stuck job cluster-4-snapshot-recovery - missing PVCs: [cluster-4-pgdata] (age:12m)
```

**Long-running Jobs**: When jobs run too long without progress:
``` text
Phase Reason: Long-running jobs detected: equilibrium state detected: job cluster-4-snapshot-recovery stuck due to missing PVCs: missing PVCs: [cluster-4-pgdata]
```

**Stuck Jobs without PVC Issues**: When jobs are pending for other reasons:
``` text
Phase Reason: Recovering from stuck job cluster-4-snapshot-recovery (no active pods for 10m)
```

These messages provide crucial information for troubleshooting:

- **Job name**: Which specific job encountered issues
- **Job statistics**: Active, succeeded, and failed pod counts
- **Age**: How long the job was running before cleanup
- **Root cause**: Specific issues like missing PVCs

If you see these messages, check:

1. **Storage**: Ensure PVCs mentioned in the error exist and are bound
2. **Resources**: Verify sufficient CPU/memory for job pods
3. **Node availability**: Check if nodes can schedule the workload
4. **Recent changes**: Review any recent cluster or storage configuration changes

The operator will automatically retry the operation after cleanup, but addressing
the root cause will prevent future occurrences.

### Networking is impaired by installed Network Policies

As pointed out in the [networking section](#networking), local network policies
could prevent some of the required connectivity.

A tell-tale sign that connectivity is impaired is the presence in the operator
logs of messages like:

``` text
"Cannot extract Pod status", […snipped…] "Get \"http://<pod IP>:8000/pg/status\": dial tcp <pod IP>:8000: i/o timeout"
```

You should list the network policies, and look for any policies restricting
connectivity.

``` sh
$ kubectl get networkpolicies                       
NAME                   POD-SELECTOR                      AGE
allow-prometheus       cnpg.io/cluster=cluster-example   47m
default-deny-ingress   <none>                            57m
```

For example, in the listing above, `default-deny-ingress` seems a likely culprit.
You can drill into it:

``` sh
$ kubectl get networkpolicies default-deny-ingress -o yaml
<…snipped…>
spec:
  podSelector: {}
  policyTypes:
  - Ingress
```

In the [networking page](networking.md) you can find a network policy file
that you can customize to create a `NetworkPolicy` explicitly allowing the
operator to connect cross-namespace to cluster pods.

### Error while bootstrapping the data directory

If your Cluster's initialization job crashes with a "Bus error (core dumped)
child process exited with exit code 135", you likely need to fix the Cluster
hugepages settings.

The reason is the incomplete support of hugepages in the cgroup v1 that should
be fixed in v2. For more information, check the PostgreSQL [BUG #17757: Not
honoring huge_pages setting during initdb causes DB crash in
Kubernetes](https://www.postgresql.org/message-id/17757-dbdfc1f1c954a6db%40postgresql.org).

To check whether hugepages are enabled, run `grep HugePages /proc/meminfo` on
the Kubernetes node and check if hugepages are present, their size, and how many
are free.

If the hugepages are present, you need to configure how much hugepages memory
every PostgreSQL pod should have available.

For example:

``` yaml
  postgresql:
    parameters:
      shared_buffers: "128MB"

  resources:
    requests:
      memory: "512Mi"
    limits:
      hugepages-2Mi: "512Mi"
```

Please remember that you must have enough hugepages memory available to schedule
every Pod in the Cluster (in the example above, at least 512MiB per Pod must be
free).

### Bootstrap job hangs in running status

If your Cluster's initialization job hangs while in `Running` status with the
message:
"error while waiting for the API server to be reachable", you probably have
a network issue preventing communication with the Kubernetes API server.
Initialization jobs (like most of jobs) need to access the Kubernetes
API. Please check your networking.

Another possible cause is when you have sidecar injection configured. Sidecars
such as Istio may make the network temporarily unavailable during startup. If
you have sidecar injection enabled, retry with injection disabled.

### Replicas take over two minutes to reconnect after a failover

When the primary instance fails, the operator promotes the most advanced standby
to the primary role. Other standby instances then attempt to reconnect to the
`-rw` service for replication. However, during this reconnection process,
`kube-proxy` may not have updated its routing information yet. As a result, the
initial `SYN` packet sent by the standby instances might fail to reach its
intended destination.

If the network is configured to silently drop packets instead of rejecting them,
standby instances will not receive a response and will retry the connection
after an exponential backoff period. On Linux systems, the default value for the
`tcp_syn_retries` kernel parameter is 6, meaning the system will attempt to
establish the connection for approximately 127 seconds before giving up. This
prolonged retry period can significantly delay the reconnection process.
For more details, consult the
[tcp_syn_retries documentation](https://www.kernel.org/doc/Documentation/networking/ip-sysctl.txt).

You can work around this issue by setting `STANDBY_TCP_USER_TIMEOUT` in the
[operator configuration](operator_conf.md#available-options). This will cause
the standby instances to close the TCP connection if the initial `SYN` packet
is not acknowledged within the specified timeout, allowing them to retry the
connection more quickly.
