# Examples

In this section, you can find some examples of configuration files to set up
your PostgreSQL Cluster.

!!! Important
    These examples are here for demonstration and experimentation
    purposes, and can be executed on a personal Kubernetes cluster with Minikube
    or Kind as described in the ["Quickstart"](quickstart.md).

!!! Seealso "Reference"
    For a list of available options, please refer to the ["API Reference" page](cloudnative-pg.v1.md).

## Basics

**Basic cluster**
:  [`cluster-example.yaml`](samples/cluster-example.yaml)
   a basic example of  a cluster.

**Custom cluster**
:  [`cluster-example-custom.yaml`](samples/cluster-example-custom.yaml)
   a basic cluster that uses the default storage class and custom parameters for
   the `postgresql.conf` and `pg_hba.conf` files.

**Cluster with customized storage class**
: [`cluster-storage-class.yaml`](samples/cluster-storage-class.yaml):
   a basic cluster that uses a specified storage class of `standard`.

**Cluster with PVC Template (Persistent Volume Claim) configured**
: [`cluster-pvc-template.yaml`](samples/cluster-pvc-template.yaml):
   a basic cluster that with an explicit persistent volume claim template.

**Extended configuration example**
: [`cluster-example-full.yaml`](samples/cluster-example-full.yaml):
   a cluster that sets most of the available options.

**Bootstrap cluster with SQL files**
: [`cluster-example-initdb-sql-refs.yaml`](samples/cluster-example-initdb-sql-refs.yaml):
   a cluster example that will execute a set of queries defined in a Secret and a ConfigMap right after the database is created.

**Sample cluster with customized `pg_hba` configuration**
: [`cluster-example-pg-hba.yaml`](samples/cluster-example-pg-hba.yaml):
  a basic cluster that enables user `app` to authenticate using certificates.

**Sample cluster with Secret and Configmap mounted using projected volume template**
: [`cluster-example-projected-volume.yaml`](samples/cluster-example-projected-volume.yaml)
  a basic cluster with existing Secret and Configmap mounted into Postgres pod using projected volume mount.

## Backups

**Customized storage class and backups**
:   *Prerequisites*: bucket storage should be available. The sample config is for AWS,
    please change to suit your setup
: [`cluster-storage-class-with-backup.yaml`](samples/cluster-storage-class-with-backup.yaml) a cluster
   with backups configured

**Backup**
:   *Prerequisites*: [`cluster-storage-class-with-backup.yaml`](samples/cluster-storage-class-with-backup.yaml)
    applied and Healthy
: [`backup-example.yaml`](samples/backup-example.yaml):
  an example of a backup that runs against the previous sample

**Simple cluster with backup configured**
:   *Prerequisites*: The configuration assumes `minio` is running and working.
    Please update `backup.barmanObjectStore` with your `minio` parameters or your cloud solution
:  [`cluster-example-with-backup.yaml`](samples/cluster-example-with-backup.yaml)
   a basic cluster with backups configured.

## Replica clusters

**Replica cluster via Backup from an object store**
:   *Prerequisites*:
    [`cluster-storage-class-with-backup.yaml`](samples/cluster-storage-class-with-backup.yaml) applied and Healthy.
    And a backup
    [`cluster-example-trigger-backup.yaml`](samples/cluster-example-trigger-backup.yaml)
    applied and Completed.
: [`cluster-example-replica-from-backup-simple.yaml`](samples/cluster-example-replica-from-backup-simple.yaml):
   a replica cluster following a cluster with backup configured.

**Replica cluster via Volume Snapshot**
:   *Prerequisites*:
    [`cluster-example-with-volume-snapshot.yaml`](samples/cluster-example-with-volume-snapshot.yaml) applied and Healthy.
    And a volume snapshot
    [`backup-with-volume-snapshot.yaml`](samples/backup-with-volume-snapshot.yaml)
    applied and Completed.
: [`cluster-example-replica-from-volume-snapshot.yaml`](samples/cluster-example-replica-from-volume-snapshot.yaml):
   a replica cluster following a cluster with volume snapshot configured.

**Replica cluster via streaming (pg_basebackup)**
:   *Prerequisites*: [`cluster-example.yaml`](samples/cluster-example.yaml)
    applied and Healthy
:   [`cluster-example-replica-streaming.yaml`](samples/cluster-example-replica-streaming.yaml): a replica cluster following `cluster-example` with streaming replication.

## PostGIS

**PostGIS example**
: [`postgis-example.yaml`](samples/postgis-example.yaml):
   an example of "PostGIS cluster" (see the [PostGIS section](postgis.md) for details.)

## Managed roles

**Cluster with declarative role management**
: [`cluster-example-with-roles.yaml`](samples/cluster-example-with-roles.yaml):
  declares a role with the `managed` stanza, includes password management with
  kubernetes secrets

## Declarative tablespaces

**Cluster with declarative tablespaces**
: [`cluster-example-with-tablespaces.yaml`](samples/cluster-example-with-tablespaces.yaml)

**Cluster with declarative tablespaces and backup**
: *Prerequisites*: The configuration assumes `minio` is running and working.
    Please update `backup.barmanObjectStore` with your `minio` parameters or your cloud solution
: [`cluster-example-with-tablespaces-backup.yaml`](samples/cluster-example-with-tablespaces-backup.yaml)

*Restored cluster with tablespaces from object store*
: *Prerequisites*: the previous cluster applied and a base backup completed.
    Remember to update `bootstrap.recovery.backup.name` with the backup name.
: [`cluster-restore-with-tablespaces.yaml`](samples/cluster-restore-with-tablespaces.yaml)

For a list of available options, see the ["API Reference" page](cloudnative-pg.v1.md).