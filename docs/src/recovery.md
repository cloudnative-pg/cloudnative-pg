# Recovery

In PostgreSQL terminology, recovery is the process of starting a PostgreSQL
instance using an existing backup. The PostgreSQL recovery mechanism
is very solid and rich. It also supports point-in-time recovery (PITR), which allows
you to restore a given cluster up to any point in time, from the first available
backup in your catalog to the last archived WAL. (The WAL
archive is mandatory in this case.)

In CloudNativePG, you can't perform recovery in place on an existing
cluster. Recovery is instead a way to bootstrap a new Postgres cluster
starting from an available physical backup.

!!! Note
    For details on the `bootstrap` stanza, see
    [Bootstrap](bootstrap.md).

The `recovery` bootstrap mode lets you create a cluster from an existing
physical base backup. You then reapply the WAL files containing the REDO log
from the archive.

WAL files are pulled from the defined *recovery object store*.

Base backups can be taken either on object stores or using volume snapshots
(from version 1.21).

!!! Warning
    Recovery using volume snapshots had an initial release on 1.20.1. Because of
    the amount of progress on the feature for 1.21.0, to use volume
    snapshots, we strongly advise
    you to upgrade to 1.21.0 or more advanced releases.

You can achieve recovery from a *recovery object store* in two ways:

- We recommend using a recovery object store, that is, a backup of another cluster
  created by Barman Cloud and defined by way of the `barmanObjectStore` option
  in the `externalClusters` section.
- Alternatively, you can use an existing `Backup` object in the same namespace. This was the
  only option available before version 1.8.0.

Both recovery methods enable either full recovery (up to the last
available WAL) or up to a [point in time](#point-in-time-recovery-pitr).
When performing a full recovery, you can also start the cluster
in replica mode (see [replica clusters](replica_cluster.md) for reference).
If using replica mode, make sure that the PostgreSQL configuration
(`.spec.postgresql.parameters`) of the recovered cluster is
compatible with the original one from a physical replication standpoint.

For recovery using volume snapshots:

- Use a consistent set of `VolumeSnapshot` objects that all belong to the
  same backup and are identified by the same `cnpg.io/cluster` and
  `cnpg.io/backupName` labels. Then, recover through the `volumeSnapshots`
  option in the `.spec.bootstrap.recovery` stanza, as described in
  [Recovery from `VolumeSnapshot` objects](#recovery-from-volumesnapshot-objects).

## Recovery from an object store

You can recover from a backup created by Barman Cloud and stored on a supported
object store. After you define the external cluster, including all the
required configuration in the `barmanObjectStore` section, you need to
reference it in the `.spec.recovery.source` option. 

This example
defines a recovery object store in a blob container in Azure:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-restore
spec:
  [...]
  
  superuserSecret:
    name: superuser-secret
    
  bootstrap:
    recovery:
      source: clusterBackup

  externalClusters:
    - name: clusterBackup
      barmanObjectStore:
        destinationPath: https://STORAGEACCOUNTNAME.blob.core.windows.net/CONTAINERNAME/
        azureCredentials:
          storageAccount:
            name: recovery-object-store-secret
            key: storage_account_name
          storageKey:
            name: recovery-object-store-secret
            key: storage_account_key
        wal:
          maxParallel: 8
```

!!! Important
    By default, the `recovery` method strictly uses the `name` of the
    cluster in the `externalClusters` section as the name of the main folder
    of the backup data within the object store. This name is normally reserved
    for the name of the server. You can specify a different folder name
    using the `barmanObjectStore.serverName` property.

!!! Note
    This example takes advantage of the parallel WAL restore
    feature, dedicating up to 8 jobs to concurrently fetch the required WAL
    files from the archive. This feature can appreciably reduce the recovery time.
    Make sure that you plan ahead for this scenario and correctly tune the
    value of this parameter for your environment. It will make a
    difference when you need it, and you will.

## Recovery from `VolumeSnapshot` objects

!!! Warning
    When creating replicas after recovering the primary instance from
    the volume snapshot, the operator might end up using `pg_basebackup`
    to synchronize them. This behavior results in a slower process, depending on the size
    of the database. This limitation will be lifted in the future when support
    for online backups and PVC cloning are introduced.

CloudNativePG can create a new cluster from a `VolumeSnapshot` of a PVC of an
existing `Cluster` that's been taken using the declarative API for
[volume snapshot backups](backup_volumesnapshot.md).
You must specify the name of the snapshot, as in the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-restore
spec:
  [...]

bootstrap:
    recovery:
      volumeSnapshots:
        storage:
          name: <snapshot name>
          kind: VolumeSnapshot
          apiGroup: snapshot.storage.k8s.io
```

In case the backed-up cluster was using a separate PVC to store the WAL files,
the recovery must include that too:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-restore
spec:
  [...]

bootstrap:
    recovery:
      volumeSnapshots:
        storage:
          name: <snapshot name>
          kind: VolumeSnapshot
          apiGroup: snapshot.storage.k8s.io

        walStorage:
          name: <snapshot name>
          kind: VolumeSnapshot
          apiGroup: snapshot.storage.k8s.io
```

!!! Warning
    If bootstrapping a replica-mode cluster from snapshots, to leverage
    snapshots for the standby instances and not just the primary,
    we recommend that you:

    1. Start with a single instance replica cluster. The primary instance will
      be recovered using the snapshot, and available WALs form the source cluster.
    2. Take a snapshot of the primary in the replica cluster.
    3. Increase the number of instances in the replica cluster as desired.

## Recovery from a `Backup` object

If a `Backup` resource is already available in the namespace in which you need to create the
cluster, you can specify the name using
`.spec.bootstrap.recovery.backup.name`, as in the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example-initdb
spec:
  instances: 3

  bootstrap:
    recovery:
      backup:
        name: backup-example

  storage:
    size: 1Gi
```

This bootstrap method allows you to specify just a reference to the
backup that needs to be restored.

The previous example implies the application database and its owning user is
the default one, `app`. If the PostgreSQL cluster being restored was using
different names, you can specify them as documented in [Configure the
application database](#configure-the-application-database).

## Additional considerations

Whether you recover from a recovery object store, a volume snapshot, or an
existing `Backup` resource, the following considerations apply:

- The application database name and the application database user are preserved
  from the backup that's being restored. The operator doesn't currently attempt
  to back up the underlying secrets, as this is part of the usual maintenance
  activity of the Kubernetes cluster.
- To preserve the original postgres user password, you need to properly
  configure `enableSuperuserAccess` and supply a `superuserSecret`.
- By default, the recovery continues up to the latest
  available WAL on the default target timeline (`current` for PostgreSQL up to
  11, `latest` for version 12 and later).
  You can optionally specify a `recoveryTarget` to perform a point-in-time
  recovery (see [Point in time recovery (PITR)](#point-in-time-recovery-pitr)).

!!! Important
    Consider using the `barmanObjectStore.wal.maxParallel` option to speed
    up WAL fetching from the archive by concurrently downloading the transaction
    logs from the recovery object store.

## Point in time recovery (PITR)

Instead of replaying all the WALs up to the latest one, after extracting a
base backup, you can ask PostgreSQL
to stop replaying WALs at any given point in time. PostgreSQL uses this technique to achieve 
PITR. The presence of a WAL archive is mandatory.

!!! Important
    PITR requires you to specify a recovery target by using the options
    described in [Recovery targets](#recovery-targets).

The operator generates the configuration parameters required for this
feature to work if you specify a recovery target.

### PITR from an object store

This example uses a recovery object store in Azure that contains both
the base backups and the WAL archive. The recovery target is based on a
requested timestamp.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-restore-pitr
spec:
  instances: 3

  storage:
    size: 5Gi

  bootstrap:
    recovery:
      # Recovery object store containing WAL archive and base backups
      source: clusterBackup
      recoveryTarget:
        # Time base target for the recovery
        targetTime: "2023-08-11 11:14:21.00000+02"

  externalClusters:
    - name: clusterBackup
      barmanObjectStore:
        destinationPath: https://STORAGEACCOUNTNAME.blob.core.windows.net/CONTAINERNAME/
        azureCredentials:
          storageAccount:
            name: recovery-object-store-secret
            key: storage_account_name
          storageKey:
            name: recovery-object-store-secret
            key: storage_account_key
        wal:
          maxParallel: 8
```

In this example, you had to specify only
the `targetTime` in the form of a timestamp. You didn't have to
specify the base backup from which to start the recovery.

The `backupID` option is the one that allows you to specify the base backup
from which to initiate the recovery process. By default, this value is
empty.

If you assign a value to it (in the form of a Barman backup ID), the operator
uses that backup as the base for the recovery.

!!! Important
    You need to make sure that such a backup exists and is accessible.

If you don't specify the backup ID, the operator detects the
base backup for the recovery as follows:

- When you use `targetTime` or `targetLSN`, the operator selects the closest
  backup that was completed before that target.
- Otherwise, the operator selects the last available backup, in chronological
  order.

### PITR from `VolumeSnapshot` objects

The example that follows uses:

- A Kubernetes volume snapshot for the `PGDATA` containing the base backup from
  which to start the recovery process. This snapshot is identified in the
  `recovery.volumeSnapshots` section and called `test-snapshot-1`.
- A recovery object store in MinIO containing the WAL archive. The object store is identified by
  the `recovery.source` option in the form of an external cluster definition.

The recovery target is based on a requested timestamp.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example-snapshot
spec:
  # ...
  bootstrap:
    recovery:
      source: cluster-example-with-backup
      volumeSnapshots:
        storage:
          name: test-snapshot-1
          kind: VolumeSnapshot
          apiGroup: snapshot.storage.k8s.io
      recoveryTarget:
        targetTime: "2023-07-06T08:00:39"
  externalClusters:
    - name: cluster-example-with-backup
      barmanObjectStore:
        destinationPath: s3://backups/
        endpointURL: http://minio:9000
        s3Credentials:
          accessKeyId:
            name: minio
            key: ACCESS_KEY_ID
          secretAccessKey:
            name: minio
            key: ACCESS_SECRET_KEY
```

!!! Note
    If the backed-up cluster had `walStorage` enabled, you also must
    specify the volume snapshot containing the `PGWAL` directory, as mentioned
    in [Recovery from VolumeSnapshot objects](#recovery-from-volumesnapshot-objects).

!!! Warning
    It's your responsibility to ensure that the end time of the base backup in
    the volume snapshot is before the recovery target timestamp.

### Recovery targets

Here are the recovery target criteria you can use:

targetTime
:  Time stamp up to which recovery proceeds, expressed in
   [RFC 3339](https://datatracker.ietf.org/doc/html/rfc3339) format.
   (The precise stopping point is also influenced by the `exclusive` option.)

targetXID
:  Transaction ID up to which recovery proceeds.
   (The precise stopping point is also influenced by the `exclusive` option.)
   Keep in mind that while transaction IDs are assigned sequentially at
   transaction start, transactions can complete in a different numeric order.
   The transactions that are recovered are those that committed before
   (and optionally including) the specified one.

targetName
:  Named restore point (created with `pg_create_restore_point()`) to which
   recovery proceeds.

targetLSN
:  LSN of the write-ahead log location up to which recovery proceeds.
   (The precise stopping point is also influenced by the `exclusive` option.)

targetImmediate
:  Recovery ends as soon as a consistent state is reached, that is, as early
   as possible. When restoring from an online backup, this means the point where
   taking the backup ended.


!!! Important
    The operator can retrieve the closest backup
    when you specify either `targetTime` or `targetLSN`. However, this isn't possible
    for the remaining targets: `targetName`, `targetXID`, and `targetImmediate`.
    In such cases, it's important to specify `backupID`, unless
    the last available backup in the catalog is acceptable.

This example uses a `targetName`-based recovery target:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
  bootstrap:
    recovery:
      source: clusterBackup
      recoveryTarget:
        backupID: 20220616T142236
        targetName: 'restore_point_1'
[...]
```

You can choose only a single one among the targets in each
`recoveryTarget` configuration.

Additionally, you can specify `targetTLI` to force recovery to a specific
timeline.

By default, the previous parameters are considered to be inclusive, stopping
just after the recovery target, matching [the behavior in PostgreSQL](https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-RECOVERY-TARGET-INCLUSIVE).
You can request exclusive behavior,
stopping right before the recovery target, by setting the `exclusive` parameter to
`true`. The following example shows this behavior, relying on a blob container in Azure
for both base backups and the WAL archive:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-restore-pitr
spec:
  instances: 3

  storage:
    size: 5Gi

  bootstrap:
    recovery:
      source: clusterBackup
      recoveryTarget:
        backupID: 20220616T142236
        targetName: "maintenance-activity"
        exclusive: true

  externalClusters:
    - name: clusterBackup
      barmanObjectStore:
        destinationPath: https://STORAGEACCOUNTNAME.blob.core.windows.net/CONTAINERNAME/
        azureCredentials:
          storageAccount:
            name: recovery-object-store-secret
            key: storage_account_name
          storageKey:
            name: recovery-object-store-secret
            key: storage_account_key
        wal:
          maxParallel: 8
```

## Configure the application database

For the recovered cluster, you can configure the application database name and
credentials with additional configuration. To update application database
credentials, you can generate your own passwords, store them as secrets, and
update the database to use the secrets. Or you can also let the operator generate a
secret with a randomly secure password for use. See
[Bootstrap an empty cluster](bootstrap.md#bootstrap-an-empty-cluster-initdb)
for more information about secrets.

This example configures the application database `app` with owner
`app` and supplied secret `app-secret`.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  bootstrap:
    recovery:
      database: app
      owner: app
      secret:
        name: app-secret
      [...]
```

With this configuration, the following happens after recovery is complete:

1. If database `app` doesn't exist, a new database `app` is created.
2. If user `app` doesn't exist, a new user `app` is created.
3. if user `app` isn't the owner of the database, user `app` is granted
as owner of database `app`.
4. If the value of `username` matches the value of `owner` in the secret, the password of
application database is changed to the value of `password` in the secret.

!!! Important
    For a replica cluster with replica mode enabled, the operator doesn't
    create any database or user in the PostgreSQL instance. These are
    recovered from the original cluster.

## How recovery works under the hood

<!-- TODO: do we need this section? -->
<!-- Also, "under the hood" is an idiom and should be replaced with something more precise -->

You can use the data uploaded to the object storage to *bootstrap* a
new cluster from an existing backup.
The operator orchestrates the recovery process using the
`barman-cloud-restore` tool (for the base backup) and the
`barman-cloud-wal-restore` tool (for WAL files, including parallel support, if
requested).

For details and instructions on the `recovery` bootstrap method, see
[Bootstrap from a backup](bootstrap.md#bootstrap-from-a-backup-recovery).

!!! Important
    If you're not familiar with how [PostgreSQL PITR](https://www.postgresql.org/docs/current/continuous-archiving.html#BACKUP-PITR-RECOVERY)
    works, we suggest that you configure the recovery cluster as the original
    one when it comes to `.spec.postgresql.parameters`. Once the new cluster is
    restored, you can then change the settings as desired.

The way it works is that the operator injects an init container in the first
instance of the new cluster, and the init container starts recovering the
backup from the object storage.

!!! Important
    The duration of the base backup copy in the new PVC depends on
    the size of the backup, as well as the speed of both the network and the
    storage.

When the base backup recovery process is complete, the operator starts the
Postgres instance in recovery mode. In this phase, PostgreSQL is up, though not
able to accept connections, and the pod is healthy according to the
liveness probe. By way of the `restore_command`, PostgreSQL starts fetching WAL
files from the archive. (You can speed up this phase by setting the
`maxParallel` option and enabling the parallel WAL restore capability.)

This phase terminates when PostgreSQL reaches the target (either the end of the
WAL or the required target in case of PITR. You can
optionally specify a `recoveryTarget` to perform a PITR. If
left unspecified, the recovery continues up to the latest available WAL on
the default target timeline (`current` for PostgreSQL up to 11, `latest` for
version 12 and later).

Once the recovery is complete, the operator sets the required
superuser password into the instance. The new primary instance starts
as usual, and the remaining instances join the cluster as replicas.

The process is transparent for the user and is managed by the instance
manager running in the pods.

## Restoring into a cluster with a backup section

<!-- TODO: do we need this section? -->

A manifest for a cluster restore might include a `backup` section.
This means that,after recovery, the new cluster starts archiving WALs and
taking backups if configured to do so.

For example, this section is part of a manifest for a cluster
bootstrapping from the cluster `cluster-example-backup`. In the storage bucket, it creates a
folder named `recoveredCluster`, where the base backups
and WALs of the recovered cluster are stored.

``` yaml
  backup:
    barmanObjectStore:
      destinationPath: s3://backups/
      endpointURL: http://minio:9000
      serverName: "recoveredCluster"
      s3Credentials:
        accessKeyId:
          name: minio
          key: ACCESS_KEY_ID
        secretAccessKey:
          name: minio
          key: ACCESS_SECRET_KEY
    retentionPolicy: "30d"

  externalClusters:
  - name: cluster-example-backup
    barmanObjectStore:
      destinationPath: s3://backups/
      endpointURL: http://minio:9000
      s3Credentials:
```

Don't reuse the same `barmanObjectStore` configuration
for different clusters. There might be cases where the existing information
in the storage buckets could be overwritten by the new cluster.

!!! Warning
    The operator includes a safety check to ensure a cluster doesn't
    overwrite a storage bucket that contained information. A cluster that would
    overwrite existing storage remains in the state `Setting up primary` with
    pods in an error state.
    The pod logs show:
    `ERROR: WAL archive check failed for server recoveredCluster: Expected empty archive`

!!! Important
    If you set the `cnpg.io/skipEmptyWalArchiveCheck` annotation to `enabled` in
    the recovered cluster, you can skip the safety check. We don't recommend skipping the check
    because, for the general use case, the check works fine. Skip this check
    only if you're familiar with the PostgreSQL recovery system, as
    severe data loss can occur.
