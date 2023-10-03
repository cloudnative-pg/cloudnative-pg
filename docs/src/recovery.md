# Recovery

In PostgreSQL terminology, recovery is the process of starting a PostgreSQL
instance using a previously taken backup. PostgreSQL recovery mechanism
is very solid and rich. It also supports Point In Time Recovery, which allows
you to restore a given cluster up to any point in time from the first available
backup in your catalog to the last archived WAL (as you can see, the WAL
archive is mandatory in this case).

In CloudNativePG, recovery cannot be performed "in-place" on an existing
cluster. Recovery is rather a way to bootstrap a new Postgres cluster
starting from an available physical backup.

!!! Note
    For details on the `bootstrap` stanza, please refer to the
    ["Bootstrap" section](bootstrap.md).

The `recovery` bootstrap mode lets you create a new cluster from an existing
physical base backup, and then reapply the WAL files containing the REDO log
from the archive.

WAL files are pulled from the defined *recovery object store*.

Base backups depend on the actual method used to take them, either object
stores or volume snapshots.

<!-- TODO: this needs to cover volume snapshots -->

Recovery from a *recovery object store* can be achieved in two ways:

- using a recovery object store, that is a backup of another cluster
  created by Barman Cloud and defined via the `barmanObjectStore` option
  in the `externalClusters` section (*recommended*)
- using an existing `Backup` object in the same namespace (this was the
  only option available before version 1.8.0).

Both recovery methods enable either full recovery (up to the last
available WAL) or up to a [point in time](#point-in-time-recovery-pitr).
When performing a full recovery, the cluster can also be started
in replica mode. Also, make sure that the PostgreSQL configuration
(`.spec.postgresql.parameters`) of the recovered cluster is
compatible, from a physical replication standpoint, with the original one.

CloudNativePG is also introducing support for Kubernetes' volume snapshots.
With the current version of CloudNativePG, you can:

- take a consistent cold backup of the Postgres cluster from a standby through
  the `kubectl cnpg snapshot` command - which creates the necessary
  `VolumeSnapshot` objects (currently one or two, if you have WALs in a separate
  volume)
- recover from the above *VolumeSnapshot* objects through the `volumeSnapshots`
  option in the `.spec.bootstrap.recovery` stanza, as described in
  ["Recovery from `VolumeSnapshot` objects"](#recovery-from-volumesnapshot-objects)
  below

## Recovery from an object store

You can recover from a backup created by Barman Cloud and stored on a supported
object storage. Once you have defined the external cluster, including all the
required configuration in the `barmanObjectStore` section, you need to
reference it in the `.spec.recovery.source` option. The following example
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
    By default the `recovery` method strictly uses the `name` of the
    cluster in the `externalClusters` section to locate the main folder
    of the backup data within the object store, which is normally reserved
    for the name of the server. You can specify a different one with the
    `barmanObjectStore.serverName` property (by default assigned to the
    value of `name` in the external clusters definition).

!!! Note
    In the above example we are taking advantage of the parallel WAL restore
    feature, dedicating up to 8 jobs to concurrently fetch the required WAL
    files from the archive. This feature can appreciably reduce the recovery time.
    Make sure that you plan ahead for this scenario and correctly tune the
    value of this parameter for your environment. It will certainly make a
    difference **when** (not if) you'll need it.

## Recovery from `VolumeSnapshot` objects

CloudNativePG can create a new cluster from a `VolumeSnapshot` of a PVC of an
existing `Cluster` that's been taken using the declarative API for
[volume snapshot backups](backup_volumesnapshot.md).
You need to specify the name of the snapshot as in the following example:

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

!!! Warning
    As the development of declarative support for Kubernetes' `VolumeSnapshot` API
    progresses, you'll be able to use this technique in conjunction with a WAL
    archive for Point In Time Recovery operations or replica clusters.

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

## Recovery from a `Backup` object

In case a Backup resource is already available in the namespace in which the
cluster should be created, you can specify its name through
`.spec.bootstrap.recovery.backup.name`, as in the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example-initdb
spec:
  instances: 3

  superuserSecret:
    name: superuser-secret

  bootstrap:
    recovery:
      backup:
        name: backup-example

  storage:
    size: 1Gi
```

This bootstrap method allows you to specify just a reference to the
backup that needs to be restored.

The previous example implies the application database and its owning user to be
the default one, `app`. If the PostgreSQL cluster being restore were using
different names, they can be specified as documented in the [Configure the
application database](#configure-the-application-database) section.

## Additional considerations

Whether you recover from a recovery object store, a volume snapshot, or an
existing `Backup` resource, the following considerations apply:

- The application database name and the application database user are preserved
from the backup that is being restored. The operator does not currently attempt
to back up the underlying secrets, as this is part of the usual maintenance
activity of the Kubernetes cluster itself.
- In case you don't supply any `superuserSecret`, a new one is automatically
generated with a secure and random password. The secret is then used to
reset the password for the `postgres` user of the cluster.
- By default, the recovery will continue up to the latest
available WAL on the default target timeline (`current` for PostgreSQL up to
11, `latest` for version 12 and above).
You can optionally specify a `recoveryTarget` to perform a point in time
recovery (see the ["Point in time recovery" section](#point-in-time-recovery-pitr)).

!!! Important
    Consider using the `barmanObjectStore.wal.maxParallel` option to speed
    up WAL fetching from the archive by concurrently downloading the transaction
    logs from the recovery object store.

## Point in time recovery (PITR)

Instead of replaying all the WALs up to the latest one, we can ask PostgreSQL
to stop replaying WALs at any given point in time, after having extracted a
base backup. PostgreSQL uses this technique to achieve *point-in-time* recovery
(PITR). The presence of a WAL archive is mandatory.

!!! Important
    PITR requires you to specify a **recovery target**, by using the options
    described in the ["Recovery targets" section](#recovery-targets) below.

The operator will generate the configuration parameters required for this
feature to work in case a recovery target is specified.

#### PITR from an object store

The example below uses a recovery object store in Azure that contains both
the base backups and the WAL archive. The recovery target is based on a
requested timestamp:

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

You might have noticed that in the above example you only had to specify
the `targetTime` in the form of a timestamp, without having to worry about
specifying the base backup from which to start the recovery.

The `backupID` option is the one that allows you to specify the base backup
from which to initiate the recovery process. By default, this value is
empty.

If you assign a value to it (in the form of a Barman backup ID), the operator
will use that backup as base for the recovery.

!!! Important
    You need to make sure that such a backup exists and is accessible.

If the backup ID is not specified, the operator will automatically detect the
base backup for the recovery as follows:

- when you use `targetTime` or `targetLSN`, the operator selects the closest
  backup that was completed before that target
- otherwise the operator selects the last available backup in chronological
  order.

### PITR from `VolumeSnapshot` Objects

The example below uses:

- a Kubernetes volume snapshot for the `PGDATA` containing the base backup from
  which to start the recovery process, identified in the
  `recovery.volumeSnapshots` section and called `test-snapshot-1`
- a recovery object store in MinIO containing the WAL archive, identified by
  the `recovery.source` option in the form of an external cluster definition

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
    In case the backed up Cluster had `walStorage` enabled, you also must
    specify the volume snapshot containing the `PGWAL` directory, as mentioned
    in the [Recovery from VolumeSnapshot objects](#recovery-from-volumeSnapshot-objects)
    section.

!!! Warning
    It is your responsibility to ensure that the end time of the base backup in
    the volume snapshot is prior to the recovery target timestamp.

### Recovery targets

Here are the recovery target criteria you can use:

targetTime
:  time stamp up to which recovery will proceed, expressed in
   [RFC 3339](https://datatracker.ietf.org/doc/html/rfc3339) format
   (the precise stopping point is also influenced by the `exclusive` option)

targetXID
:  transaction ID up to which recovery will proceed
   (the precise stopping point is also influenced by the `exclusive` option);
   keep in mind that while transaction IDs are assigned sequentially at
   transaction start, transactions can complete in a different numeric order.
   The transactions that will be recovered are those that committed before
   (and optionally including) the specified one

targetName
:  named restore point (created with `pg_create_restore_point()`) to which
   recovery will proceed

targetLSN
:  LSN of the write-ahead log location up to which recovery will proceed
   (the precise stopping point is also influenced by the `exclusive` option)

targetImmediate
:  recovery should end as soon as a consistent state is reached - i.e. as early
   as possible. When restoring from an online backup, this means the point where
   taking the backup ended


!!! Important
    While the operator is able to automatically retrieve the closest backup
    when either `targetTime` or `targetLSN` is specified, this is not possible
    for the remaining targets: `targetName`, `targetXID`, and `targetImmediate`.
    In such cases, it is important to specify `backupID`, unless you are OK with
    the last available backup in the catalog.

The example below uses a `targetName` based recovery target:

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

You can choose only a single one among the targets above in each
`recoveryTarget` configuration.

Additionally, you can specify `targetTLI` force recovery to a specific
timeline.

By default, the previous parameters are considered to be inclusive, stopping
just after the recovery target, matching [the behavior in PostgreSQL](https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-RECOVERY-TARGET-INCLUSIVE)
You can request exclusive behavior,
stopping right before the recovery target, by setting the `exclusive` parameter to
`true` like in the following example relying on a blob container in Azure
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

For the recovered cluster, we can configure the application database name and
credentials with additional configuration. To update application database
credentials, we can generate our own passwords, store them as secrets, and
update the database use the secrets. Or we can also let the operator generate a
secret with randomly secure password for use. Please reference the
["Bootstrap an empty cluster"](bootstrap.md#bootstrap-an-empty-cluster-initdb)
section for more information about secrets.

The following example configure the application database `app` with owner
`app`, and supplied secret `app-secret`.

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

With the above configuration, the following will happen after recovery is completed:

1. if database `app` does not exist, a new database `app` will be created.
2. if user `app` does not exist, a new user `app` will be created.
3. if user `app` is not the owner of database, user `app` will be granted
as owner of database `app`.
4. If value of `username` match value of `owner` in secret, the password of
application database will be changed to the value of `password` in secret.

!!! Important
    For a replica cluster with replica mode enabled, the operator will not
    create any database or user in the PostgreSQL instance, as these will be
    recovered from the original cluster.

## How recovery works under the hood

<!-- TODO: do we need this section? -->

You can use the data uploaded to the object storage to *bootstrap* a
new cluster from a previously taken backup.
The operator will orchestrate the recovery process using the
`barman-cloud-restore` tool (for the base backup) and the
`barman-cloud-wal-restore` tool (for WAL files, including parallel support, if
requested).

For details and instructions on the `recovery` bootstrap method, please refer
to the ["Bootstrap from a backup" section](bootstrap.md#bootstrap-from-a-backup-recovery).

!!! Important
    If you are not familiar with how [PostgreSQL PITR](https://www.postgresql.org/docs/current/continuous-archiving.html#BACKUP-PITR-RECOVERY)
    works, we suggest that you configure the recovery cluster as the original
    one when it comes to `.spec.postgresql.parameters`. Once the new cluster is
    restored, you can then change the settings as desired.

Under the hood, the operator will inject an init container in the first
instance of the new cluster, and the init container will start recovering the
backup from the object storage.

!!! Important
    The duration of the base backup copy in the new PVC depends on
    the size of the backup, as well as the speed of both the network and the
    storage.

When the base backup recovery process is completed, the operator starts the
Postgres instance in recovery mode: in this phase, PostgreSQL is up, albeit not
able to accept connections, and the pod is healthy according to the
liveness probe. Through the `restore_command`, PostgreSQL starts fetching WAL
files from the archive (you can speed up this phase by setting the
`maxParallel` option and enable the parallel WAL restore capability).

This phase terminates when PostgreSQL reaches the target (either the end of the
WAL or the required target in case of Point-In-Time-Recovery). Indeed, you can
optionally specify a `recoveryTarget` to perform a point in time recovery. If
left unspecified, the recovery will continue up to the latest available WAL on
the default target timeline (`current` for PostgreSQL up to 11, `latest` for
version 12 and above).

Once the recovery is complete, the operator will set the required
superuser password into the instance. The new primary instance will start
as usual, and the remaining instances will join the cluster as replicas.

The process is transparent for the user and it is managed by the instance
manager running in the Pods.

## Restoring into a cluster with a backup section

<!-- TODO: do we need this section? -->

A manifest for a cluster restore may include a `backup` section.
This means that the new cluster, after recovery, will start archiving WAL's and
taking backups if configured to do so.

For example, the section below could be part of a manifest for a Cluster
bootstrapping from Cluster `cluster-example-backup`, and would create a
new folder in the storage bucket named `recoveredCluster` where the base backups
and WAL's of the recovered cluster would be stored.

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

You should not re-use the exact same `barmanObjectStore` configuration
for different clusters. There could be cases where the existing information
in the storage buckets could be overwritten by the new cluster.

!!! Warning
    The operator includes a safety check to ensure a cluster will not
    overwrite a storage bucket that contained information. A cluster that would
    overwrite existing storage will remain in state `Setting up primary` with
    Pods in an Error state.
    The pod logs will show:
    `ERROR: WAL archive check failed for server recoveredCluster: Expected empty archive`

!!! Important
    If you set the `cnpg.io/skipEmptyWalArchiveCheck` annotation to `enabled` in
    the recovered cluster, you can skip the above check. This is not recommended
    as for the general use case the above check works fine. Please don't do
    this unless you are familiar with PostgreSQL recovery system, as this can lead
    you to severe data loss.

