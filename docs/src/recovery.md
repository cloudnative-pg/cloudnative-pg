---
id: recovery
sidebar_position: 200
title: Recovery
---

# Recovery
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

In PostgreSQL, **recovery** refers to the process of starting an instance from
an existing physical backup. PostgreSQL's recovery system is robust and
feature-rich, supporting **Point-In-Time Recovery (PITR)**—the ability to
restore a cluster to any specific moment, from the earliest available backup to
the latest archived WAL file.

:::info[Important]
    A valid WAL archive is required to perform PITR.
:::

In CloudNativePG, recovery is **not performed in-place** on an existing
cluster. Instead, it is used to **bootstrap a new cluster** from a physical
backup.

:::note
    For more details on configuring the `bootstrap` stanza, refer to
    [Bootstrap](bootstrap.md).
:::

The `recovery` bootstrap mode allows you to initialize a cluster from a
physical base backup and replay the associated WAL files to bring the system to
a consistent and optionally point-in-time state.

CloudNativePG supports recovery via:

- A **pluggable backup and recovery interface (CNPG-I)**, enabling integration
  with external tools such as the [Barman Cloud Plugin](https://cloudnative-pg.io/plugin-barman-cloud/).
- **Native recovery from volume snapshots**, where supported by the underlying
  Kubernetes storage infrastructure.
- **Native recovery from object stores via Barman Cloud**, which is
  **deprecated** as of version 1.26 in favor of the plugin-based approach.

With the deprecation of native Barman Cloud support in version 1.26, this
section now focuses on two supported recovery methods: using the **Barman Cloud
Plugin** for recovery from object stores, and the **native interface** for
recovery from volume snapshots.

:::info[Important]
    For legacy documentation, see
    [Appendix B – Recovery from an Object Store](appendixes/backup_barmanobjectstore.md#recovery-from-an-object-store).
:::

## Recovery from an Object Store with the Barman Cloud Plugin

This section outlines how to recover a PostgreSQL cluster from an object store
using the recommended Barman Cloud Plugin.

:::info[Important]
    The object store must contain backup data produced by a CloudNativePG
    `Cluster`—either using the **deprecated native Barman Cloud integration** or
    the **Barman Cloud Plugin**.
:::

:::info
    For full details, refer to the
    [“Recovery of a Postgres Cluster” section in the Barman Cloud Plugin documentation](https://cloudnative-pg.io/plugin-barman-cloud/docs/concepts/#recovery-of-a-postgres-cluster).
:::

Begin by defining the object store that holds both your base backups and WAL
files. The Barman Cloud Plugin uses a custom `ObjectStore` resource for this
purpose. The following example shows how to configure one for Azure Blob
Storage:

```yaml
apiVersion: barmancloud.cnpg.io/v1
kind: ObjectStore
metadata:
  name: cluster-example-backup
spec:
  configuration:
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

Next, configure the `Cluster` resource to use the `ObjectStore` you defined. In
the `bootstrap` section, specify the recovery source, and define an
`externalCluster` entry that references the plugin:

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
      source: origin

  externalClusters:
    - name: origin
      plugin:
        name: barman-cloud.cloudnative-pg.io
        parameters:
          barmanObjectName: cluster-example-backup
          serverName: cluster-example
```

## Recovery from `VolumeSnapshot` Objects

:::warning
    When creating replicas after recovering a primary instance from a
    `VolumeSnapshot`, the operator may fall back to using `pg_basebackup` to
    synchronize them. This process can be significantly slower—especially for large
    databases—because it involves a full base backup. This limitation will be
    addressed in the future with support for online backups and PVC cloning in
    the scale-up process.
:::

CloudNativePG allows you to create a new cluster from a `VolumeSnapshot` of a
`PersistentVolumeClaim` (PVC) that belongs to an existing `Cluster`.
These snapshots are created using the declarative API for
[volume snapshot backups](appendixes/backup_volumesnapshot.md).

To complete the recovery process, the new cluster must also reference an
external cluster that provides access to the WAL archive needed to reapply
changes and finalize the recovery.

The following example shows a cluster being recovered using both a
`VolumeSnapshot` for the base backup and a WAL archive accessed through the
Barman Cloud Plugin:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-restore
spec:
  [...]

  bootstrap:
    recovery:
      source: origin
      volumeSnapshots:
        storage:
          name: <snapshot name>
          kind: VolumeSnapshot
          apiGroup: snapshot.storage.k8s.io

  externalClusters:
    - name: origin
      plugin:
        name: barman-cloud.cloudnative-pg.io
        parameters:
          barmanObjectName: cluster-example-backup
          serverName: cluster-example
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

The previous example assumes that the application database and its owning user
are named `app` by default. If the PostgreSQL cluster being restored uses
different names, you must specify these names before exiting the recovery phase,
as documented in ["Configure the application database"](#configure-the-application-database).

:::warning
    If bootstrapping a replica-mode cluster from snapshots, to leverage
    snapshots for the standby instances and not just the primary,
    we recommend that you:

    1. Start with a single instance replica cluster. The primary instance will
      be recovered using the snapshot, and available WALs from the source cluster.
    2. Take a snapshot of the primary in the replica cluster.
    3. Increase the number of instances in the replica cluster as desired.
:::

## Recovery from a `Backup` object

If a `Backup` resource is already available in the namespace in which you need
to create the cluster, you can specify the name using
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

The previous example assumes that the application database and its owning user
are named `app` by default. If the PostgreSQL cluster being restored uses
different names, you must specify these names before exiting the recovery phase,
as documented in ["Configure the application database"](#configure-the-application-database).

## Additional Considerations

Whether you recover from an object store, a volume snapshot, or an existing
`Backup` resource, no changes to the database, including the catalog, are
permitted until the `Cluster` is fully promoted to primary and accepts write
operations. This restriction includes any role overrides, which are deferred
until the `Cluster` transitions to primary.
As a result, the following considerations apply:

- The application database name and user are copied from the backup being
  restored. The operator does not currently back up the underlying secrets, as
  this is part of the usual maintenance activity of the Kubernetes cluster.
- To preserve the original postgres user password, configure
  `enableSuperuserAccess` and supply a `superuserSecret`.

By default, recovery continues up to the latest available WAL on the default
target timeline (`latest`). You can optionally specify a `recoveryTarget` to
perform a point-in-time recovery (see [Point in Time Recovery (PITR)](#point-in-time-recovery-pitr)).

:::info[Important]
    Consider using the `barmanObjectStore.wal.maxParallel` option to speed
    up WAL fetching from the archive by concurrently downloading the transaction
    logs from the recovery object store.
:::

## Point in time recovery (PITR)

Instead of replaying all the WALs up to the latest one, after extracting a base
backup, you can ask PostgreSQL to stop replaying WALs at any given point in
time. PostgreSQL uses this technique to achieve PITR. The presence of a WAL
archive is mandatory.

:::info[Important]
    PITR requires you to specify a recovery target by using the options
    described in [Recovery targets](#recovery-targets).
:::

The operator generates the configuration parameters required for this
feature to work if you specify a recovery target.

### PITR from an object store

This example uses the same recovery object store in Azure defined earlier for
the Barman Cloud plugin, containing both the base backups and the WAL archive.
The recovery target is based on a requested timestamp.

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
      source: origin
      recoveryTarget:
        # Time base target for the recovery
        targetTime: "2023-08-11 11:14:21.00000+02"

  externalClusters:
    - name: origin
      plugin:
        name: barman-cloud.cloudnative-pg.io
        parameters:
          barmanObjectName: cluster-example-backup
          serverName: cluster-example
```

In this example, you had to specify only the `targetTime` in the form of a
timestamp. You didn't have to specify the base backup from which to start the
recovery.

The `backupID` option is the one that allows you to specify the base backup
from which to initiate the recovery process. By default, this value is
empty.

If you assign a value to it (in the form of a Barman backup ID), the operator
uses that backup as the base for the recovery.

:::info[Important]
    You need to make sure that such a backup exists and is accessible.
:::

If you don't specify the backup ID, the operator detects the base backup for
the recovery as follows:

- When you use `targetTime` or `targetLSN`, the operator selects the closest
  backup that was completed before that target.
- Otherwise, the operator selects the last available backup, in chronological
  order.

### Point-in-Time Recovery (PITR) from `VolumeSnapshot` Objects

The following example demonstrates how to perform a **Point-in-Time Recovery (PITR)** using:

- A Kubernetes `VolumeSnapshot` of the `PGDATA` directory, which provides the
  base backup. This snapshot is specified in the `recovery.volumeSnapshots`
  section and is named `test-snapshot-1`.
- A recovery object store (in this case, MinIO) containing the archived WAL
  files. The object store is defined via a Barman Cloud Plugin `ObjectStore`
  resource (not shown here), and referenced using the `recovery.source` field,
  which points to an external cluster configuration.

The cluster will be restored to a specific point in time using the
`recoveryTarget.targetTime` option.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example-snapshot
spec:
  # ...
  bootstrap:
    recovery:
      source: origin
      volumeSnapshots:
        storage:
          name: test-snapshot-1
          kind: VolumeSnapshot
          apiGroup: snapshot.storage.k8s.io
      recoveryTarget:
        targetTime: "2023-07-06T08:00:39"
  externalClusters:
    - name: origin
      plugin:
        name: barman-cloud.cloudnative-pg.io
        parameters:
          barmanObjectName: minio-backup
          serverName: cluster-example
```

This setup enables CloudNativePG to restore the base data from a volume
snapshot and apply WAL segments from the object store to reach the desired
recovery target.

:::note
    If the backed-up cluster had `walStorage` enabled, you also must specify
    the volume snapshot containing the `PGWAL` directory, as mentioned in
    [Recovery from VolumeSnapshot objects](#recovery-from-volumesnapshot-objects).
:::

:::warning
    It's your responsibility to ensure that the end time of the base backup in
    the volume snapshot is before the recovery target timestamp.
:::

:::warning
    If you added or removed a [tablespace](tablespaces.md) in your cluster
    since the last base backup, replaying the WAL will fail. You need a base
    backup between the time of the tablespace change and the recovery target
    timestamp.
:::

### Recovery targets

Here are the recovery target criteria you can use:

targetTime
:  Time stamp up to which recovery proceeds, expressed in
   [RFC 3339](https://datatracker.ietf.org/doc/html/rfc3339) format.
   (The precise stopping point is also influenced by the `exclusive` option.)

:::warning
    PostgreSQL recovery will stop when it encounters the first transaction that
    occurs after the specified time. If no such transaction exists after the
    target time, the recovery process will fail.
:::

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

:::info[Important]
    The operator can retrieve the closest backup when you specify either
    `targetTime` or `targetLSN`. However, this isn't possible for the remaining
    targets: `targetName`, `targetXID`, and `targetImmediate`. In such cases, it's
    mandatory to specify `backupID`.
:::

This example uses a `targetName`-based recovery target:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
  bootstrap:
    recovery:
      source: origin
      recoveryTarget:
        backupID: 20220616T142236
        targetName: 'restore_point_1'
[...]
```

You can choose only a single one among the targets in each `recoveryTarget`
configuration.

Additionally, you can specify `targetTLI` to force recovery to a specific
timeline.

By default, the previous parameters are considered to be inclusive, stopping
just after the recovery target, matching
[the behavior in PostgreSQL](https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-RECOVERY-TARGET-INCLUSIVE).

You can request exclusive behavior, stopping right before the recovery target,
by setting the `exclusive` parameter to `true`. The following example shows
this behavior, relying on a blob container in Azure for both base backups and
the WAL archive:

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
      source: origin
      recoveryTarget:
        backupID: 20220616T142236
        targetName: "maintenance-activity"
        exclusive: true

  externalClusters:
    - name: origin
      plugin:
        name: barman-cloud.cloudnative-pg.io
        parameters:
          barmanObjectName: cluster-example-backup
          serverName: cluster-example
```

## Configure the application database

For the recovered cluster, you can configure the application database name and
credentials with additional configuration. To update application database
credentials, you can generate your own passwords, store them as secrets, and
update the database to use the secrets. Or you can also let the operator
generate a secret with a randomly secure password for use.
See [Bootstrap an empty cluster](bootstrap.md#bootstrap-an-empty-cluster-initdb)
for more information about secrets.

:::info[Important]
    While the `Cluster` is in recovery mode, no changes to the database,
    including the catalog, are permitted. This restriction includes any role
    overrides, which are deferred until the `Cluster` transitions to primary.
    During this phase, users remain as defined in the source cluster.
:::

The following example configures the `app` database with the owner `app` and
the password stored in the provided secret `app-secret`, following the
bootstrap from a live cluster.

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

With the above configuration, the following will happen only **after recovery is
completed**:

1. If the `app` database does not exist, it will be created.
2. If the `app` user does not exist, it will be created.
3. If the `app` user is not the owner of the `app` database, ownership will be
   granted to the `app` user.
4. If the `owner` value matches the `username` value in the secret, the
   password for the application user (the `app` user in this case) will be
   updated to the `password` value in the secret.

## How recovery works under the hood

<!-- TODO: do we need this section? -->
<!-- Also, "under the hood" is an idiom and should be replaced with something more precise -->

You can use the data uploaded to the object storage to *bootstrap* a new
cluster from an existing backup. The operator orchestrates the recovery process
using the `barman-cloud-restore` tool (for the base backup) and the
`barman-cloud-wal-restore` tool (for WAL files, including parallel support, if
requested).

For details and instructions on the `recovery` bootstrap method, see
[Bootstrap from a backup](bootstrap.md#bootstrap-from-a-backup-recovery).

:::info[Important]
    If you're not familiar with how
    [PostgreSQL PITR](https://www.postgresql.org/docs/current/continuous-archiving.html#BACKUP-PITR-RECOVERY)
    works, we suggest that you configure the recovery cluster as the original
    one when it comes to `.spec.postgresql.parameters`. Once the new cluster is
    restored, you can then change the settings as desired.
:::

The way it works is that the operator injects an init container in the first
instance of the new cluster, and the init container starts recovering the
backup from the object storage.

:::info[Important]
    The duration of the base backup copy in the new PVC depends on
    the size of the backup, as well as the speed of both the network and the
    storage.
:::

When the base backup recovery process is complete, the operator starts the
Postgres instance in recovery mode. In this phase, PostgreSQL is up, though not
able to accept connections, and the pod is healthy according to the
liveness probe. By way of the `restore_command`, PostgreSQL starts fetching WAL
files from the archive. You can speed up this phase by setting the
`maxParallel` option and enabling the parallel WAL restore capability.

This phase terminates when PostgreSQL reaches the target, either the end of the
WAL or the required target in case of PITR. You can optionally specify a
`recoveryTarget` to perform a PITR. If left unspecified, the recovery continues
up to the latest available WAL on the default target timeline (`latest`).

Once the recovery is complete, the operator sets the required superuser
password into the instance. The new primary instance starts as usual, and the
remaining instances join the cluster as replicas.

The process is transparent for the user and is managed by the instance manager
running in the pods.

## Restoring into a Cluster with a Backup Section

When restoring a cluster, the manifest may include a `plugins` section with
Barman Cloud plugin pointing to a *backup* object store resource. This enables
the newly created cluster to begin archiving WAL files and taking backups
immediately after recovery—provided backup policies are configured.

Avoid reusing the same `ObjectStore` configuration for both *backup* and
*recovery* in the same cluster. If you must, ensure that each cluster uses a
unique `serverName` to prevent accidental overwrites of backup or WAL archive
data.

:::warning
    CloudNativePG includes a safety check to prevent a cluster from overwriting
    existing data in a shared storage bucket. If a conflict is detected, the
    cluster remains in the `Setting up primary` state, and the associated pods will
    fail with an error. The pod logs will display:
    `ERROR: WAL archive check failed for server recoveredCluster: Expected empty archive`.
:::

:::info[Important]
    You can bypass this safety check by setting the
    `cnpg.io/skipEmptyWalArchiveCheck` annotation to `enabled` on the recovered
    cluster. However, this is strongly discouraged unless you are highly
    familiar with PostgreSQL's recovery process. Skipping the check incorrectly can
    lead to severe data loss. Use with caution and only in expert scenarios.
:::