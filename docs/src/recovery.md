# Recovery

<!-- TODO:

- Explain the two sources of recovery: object store and volume snapshots
- Provide an overview of both solutions (see example table below)
- Describe both solutions, with examples, and cover PITR as well as snapshot recovery

|                                      | WAL archiving | Cold backup | Hot backup | Backup from a standby | Snapshot recovery | Point In Time Recovery (PITR) |
|--------------------------------------|:-------------:|:-----------:|:----------:|:---------------------:|:-----------------:|:-----------------------------:|
| Object store                         |      Yes      |      No     |     Yes    |          Yes          |        No*        |              Yes              |
| Volume Snapshots without WAL archive |       No      |     Yes     |     No     |          Yes          |        Yes        |               No              |
| Volume Snapshots with WAL archive    |      Yes      |     Yes     |     Yes    |          Yes          |        Yes        |              Yes              |

-->

Cluster restores are not performed "in-place" on an existing cluster.
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
