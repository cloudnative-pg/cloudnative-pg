# Labels and annotations

Resources in Kubernetes are organized in a flat structure, with no hierarchical
information or relationship between them. However, such resources and objects
can be linked together and put in relationship through *labels* and
*annotations*.

!!! info
    For more information, see the Kubernetes documentation on
    [annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/) and
    [labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/).

In brief:

- An annotation is used to assign additional non-identifying information to
  resources with the goal of facilitating integration with external tools.
- A label is used to group objects and query them through the Kubernetes native
  selector capability.

You can select one or more labels or annotations to use
in your CloudNativePG deployments. Then you need to configure the operator
so that when you define these labels or annotations in a cluster's metadata,
they're inherited by all resources created by it (including pods).

!!! Note
    Label and annotation inheritance is the technique adopted by CloudNativePG
    instead of alternative approaches such as pod templates.

## Predefined labels

These predefined labels are managed by CloudNativePG.

`cnpg.io/backupDate`
: The date of the backup in ISO 8601 format (`YYYYMMDD`)

`cnpg.io/backupName`
: Backup identifier, available only on `Backup` and `VolumeSnapshot`
  resources

`cnpg.io/backupMonth`
: The year/month when a backup was taken

`cnpg.io/backupTimeline`
: The timeline of the instance when a backup was taken

`cnpg.io/backupYear`
: The year a backup was taken

`cnpg.io/cluster`
: Name of the cluster

`cnpg.io/immediateBackup`
: Applied to a `Backup` resource if the backup is the first one created from
  a `ScheduledBackup` object having `immediate` set to `true`

`cnpg.io/instanceName`
: Name of the PostgreSQL instance (replaces the old and
  deprecated `postgresql` label)

`cnpg.io/jobRole`
: Role of the job (that is, `import`, `initdb`, `join`, ...)

`cnpg.io/onlineBackup`
: Whether the backup is online (hot) or taken when Postgres is down (cold)

`cnpg.io/podRole`
: Distinguishes pods dedicated to pooler from those for instance

`cnpg.io/poolerName`
: Name of the PgBouncer pooler

`cnpg.io/pvcRole`
: Purpose of the PVC, such as `PG_DATA` or `PG_WAL`

`cnpg.io/reload`
: Available on `ConfigMap` and `Secret` resources. When set to `true`,
  a change in the resource is automatically reloaded by the operator.

`role` - **deprecated**
:  Whether the instance running in a pod is a `primary` or a `replica`.
   This label is deprecated, you should use `cnpg.io/instanceRole` instead.

`cnpg.io/scheduled-backup`
:  When available, name of the `ScheduledBackup` resource that created a given
   `Backup` object

`cnpg.io/instanceRole`
: Whether the instance running in a pod is a `primary` or a `replica`.


## Predefined annotations

These predefined annotations are managed by CloudNativePG.

`container.apparmor.security.beta.kubernetes.io/*`
:   Name of the AppArmor profile to apply to the named container.
    See [AppArmor](security.md#restricting-pod-access-using-apparmor)
    for details.

`cnpg.io/backupEndTime`
: The time a backup ended.

`cnpg.io/backupEndWAL`
: The WAL at the conclusion of a backup.

`cnpg.io/backupStartTime`
: The time a backup started.

`cnpg.io/backupStartWAL`
: The WAL at the start of a backup.

`cnpg.io/coredumpFilter`
:   Filter to control the coredump of Postgres processes, expressed with a
    bitmask. By default it's set to `0x31` to exclude shared memory
    segments from the dump. See [PostgreSQL core dumps](troubleshooting.md#postgresql-core-dumps)
    for more information.

`cnpg.io/clusterManifest`
:   Manifest of the `Cluster` owning this resource (such as a PVC). This label
    replaces the old, deprecated `cnpg.io/hibernateClusterManifest` label.

`cnpg.io/fencedInstances`
:   List of the instances that need to be fenced, expressed in JSON format.
    The whole cluster is fenced if the list contains the `*` element.

`cnpg.io/forceLegacyBackup`
:   Applied to a `Cluster` resource for testing purposes only, to
    simulate the behavior of `barman-cloud-backup` prior to version 3.4 (Jan 2023)
    when the `--name` option wasn't available.

`cnpg.io/hash`
:   The hash value of the resource.

`cnpg.io/hibernation`
:   Applied to a `Cluster` resource to control the [declarative hibernation feature](declarative_hibernation.md).
    Allowed values are `on` and `off`.

`cnpg.io/managedSecrets`
:   Pull secrets managed by the operator and automatically set in the
    `ServiceAccount` resources for each Postgres cluster.

`cnpg.io/nodeSerial`
:   On a pod resource, identifies the serial number of the instance within the
    Postgres cluster.

`cnpg.io/operatorVersion`
:   Version of the operator.

`cnpg.io/pgControldata`
:   Output of the `pg_controldata` command. This annotation replaces the old,
    deprecated `cnpg.io/hibernatePgControlData` annotation.

`cnpg.io/podEnvHash`
:   Deprecated, as the `cnpg.io/podSpec` annotation now also contains the pod environment.

`cnpg.io/podSpec`
:   Snapshot of the `spec` of the pod generated by the operator. This annotation replaces
    the old, deprecated `cnpg.io/podEnvHash` annotation.

`cnpg.io/poolerSpecHash`
:   Hash of the pooler resource.

`cnpg.io/pvcStatus`
:   Current status of the PVC: `initializing`, `ready`, or `detached`.

`cnpg.io/reconciliationLoop`
:   When set to `disabled` on a `Cluster`, the operator prevents the
    reconciliation loop from running.

`cnpg.io/reloadedAt`
:   Contains the latest cluster `reload` time. `reload` is triggered by the user through a plugin.

`cnpg.io/skipEmptyWalArchiveCheck`
:   When set to `true` on a `Cluster` resource, the operator disables the check
    that ensures that the WAL archive is empty before writing data. Use at your own
    risk.

`cnpg.io/skipEmptyWalArchiveCheck`
:   When set to `true` on a `Cluster` resource, the operator disables WAL archiving.
    This will set `archive_mode` to `off` and require a restart of all PostgreSQL
    instances. Use at your own risk.

`cnpg.io/snapshotStartTime`
:   The time a snapshot started.

`cnpg.io/snapshotEndTime`
:   The time a snapshot was marked as ready to use.

`kubectl.kubernetes.io/restartedAt`
:   When available, the time of last requested restart of a Postgres cluster.

## Prerequisites

By default, no label or annotation defined in the cluster's metadata is
inherited by the associated resources.
To enable label/annotation inheritance, follow the
instructions provided in [Operator configuration](operator_conf.md).

The following continues from that example and limits it to the following:

- Annotations: `categories`
- Labels: `app`, `environment`, and `workload`

!!! Note
    Feel free to select the names that most suit your context for both
    annotations and labels. You can also use wildcards
    in naming and adopt strategies like using `mycompany/*` for all labels
    or setting annotations starting with `mycompany/` to be inherited.

## Defining cluster's metadata

When defining the cluster, before any resource is deployed, you can
set the metadata as follows:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
  annotations:
    categories: database
  labels:
    environment: production
    workload: database
    app: sso
spec:
     # ... <snip>
```

Once the cluster is deployed, you can verify, for example, that the labels
were correctly set in the pods:

```shell
kubectl get pods --show-labels
```

## Current limitations

Currently, CloudNativePG doesn't automatically propagate labels or
annotations deletions. Therefore, when an annotation or label is removed from
a cluster that was previously propagated to the underlying pods, the operator
doesn't remove it on the associated resources.
