# Examples

The examples show configuration files for setting up
your PostgreSQL cluster.

!!! Important
    These examples are for demonstration and experimentation
    purposes. You can execute them on a personal Kubernetes cluster with Minikube
    or Kind, as described in [Quick start](quickstart.md).

!!! Seealso "Reference"
    For a list of available options, see [API reference](cloudnative-pg.v1.md).

## Basics

**Basic cluster**
:  [`cluster-example.yaml`](samples/cluster-example.yaml)
   A basic example of a cluster.

**Custom cluster**
:  [`cluster-example-custom.yaml`](samples/cluster-example-custom.yaml)
   A basic cluster that uses the default storage class and custom parameters for
   the `postgresql.conf` and `pg_hba.conf` files.

**Cluster with customized storage class**
: [`cluster-storage-class.yaml`](samples/cluster-storage-class.yaml):
   A basic cluster that uses a specified storage class of `standard`.

**Cluster with persistent volume claim (PVC) template configured**
: [`cluster-pvc-template.yaml`](samples/cluster-pvc-template.yaml):
   A basic cluster with an explicit persistent volume claim template.

**Extended configuration example**
: [`cluster-example-full.yaml`](samples/cluster-example-full.yaml):
   A cluster that sets most of the available options.

**Bootstrap cluster with SQL files**
: [`cluster-example-initdb-sql-refs.yaml`](samples/cluster-example-initdb-sql-refs.yaml):
   A cluster example that executes a set of queries defined in a secret and a
   `ConfigMap` right after the database is created.

**Sample cluster with customized `pg_hba` configuration**
: [`cluster-example-pg-hba.yaml`](samples/cluster-example-pg-hba.yaml):
  A basic cluster that enables the user app to authenticate using certificates.

**Sample cluster with Secret and ConfigMap mounted using projected volume template**
: [`cluster-example-projected-volume.yaml`](samples/cluster-example-projected-volume.yaml)
  A basic cluster with the existing `Secret` and `ConfigMap` mounted into Postgres
  pod using projected volume mount.

## Backups

**Customized storage class and backups**
:   *Prerequisites*: Bucket storage must be available. The sample config is for AWS.
    Change it to suit your setup.
: [`cluster-storage-class-with-backup.yaml`](samples/cluster-storage-class-with-backup.yaml) 
   A cluster with backups configured.

**Backup**
:   *Prerequisites*: [`cluster-storage-class-with-backup.yaml`](samples/cluster-storage-class-with-backup.yaml)
    applied and healthy.
: [`backup-example.yaml`](samples/backup-example.yaml):
  An example of a backup that runs against the previous sample.

**Simple cluster with backup configured**
:   *Prerequisites*: The configuration assumes minio is running and working.
    Update `backup.barmanObjectStore` with your minio parameters or your cloud solution.
:  [`cluster-example-with-backup.yaml`](samples/cluster-example-with-backup.yaml)
   A basic cluster with backups configured.

## Replica clusters

**Replica cluster by way of backup from an object store**
:   *Prerequisites*:
    [`cluster-storage-class-with-backup.yaml`](samples/cluster-storage-class-with-backup.yaml)
    applied and healthy, and a backup
    [`cluster-example-trigger-backup.yaml`](samples/cluster-example-trigger-backup.yaml)
    applied and completed.
: [`cluster-example-replica-from-backup-simple.yaml`](samples/cluster-example-replica-from-backup-simple.yaml):
   A replica cluster following a cluster with backup configured.

**Replica cluster by way of volume snapshot**
:   *Prerequisites*:
    [`cluster-example-with-volume-snapshot.yaml`](samples/cluster-example-with-volume-snapshot.yaml)
    applied and healthy, and a volume snapshot
    [`backup-with-volume-snapshot.yaml`](samples/backup-with-volume-snapshot.yaml)
    applied and completed.
: [`cluster-example-replica-from-volume-snapshot.yaml`](samples/cluster-example-replica-from-volume-snapshot.yaml):
   A replica cluster following a cluster with volume snapshot configured.

**Replica cluster by way of streaming (pg_basebackup)**
:   *Prerequisites*: [`cluster-example.yaml`](samples/cluster-example.yaml)
    applied and healthy.
:   [`cluster-example-replica-streaming.yaml`](samples/cluster-example-replica-streaming.yaml): 
   A replica cluster following `cluster-example` with streaming replication.

## PostGIS

**PostGIS example**
: [`postgis-example.yaml`](samples/postgis-example.yaml):
   An example of a PostGIS cluster. See [PostGIS](postgis.md) for details.

## Managed roles

**Cluster with declarative role management**
: [`cluster-example-with-roles.yaml`](samples/cluster-example-with-roles.yaml):
  Declares a role with the `managed` stanza. Includes password management with
  Kubernetes secrets.

## Declarative tablespaces

**Cluster with declarative tablespaces**
: [`cluster-example-with-tablespaces.yaml`](samples/cluster-example-with-tablespaces.yaml)

**Cluster with declarative tablespaces and backup**
: *Prerequisites*: The configuration assumes minio is running and working.
    Update `backup.barmanObjectStore` with your minio parameters or your cloud solution.
: [`cluster-example-with-tablespaces-backup.yaml`](samples/cluster-example-with-tablespaces-backup.yaml)

**Restored cluster with tablespaces from object store**
: *Prerequisites*: The previous cluster applied and a base backup completed.
    Remember to update `bootstrap.recovery.backup.name` with the backup name.
: [`cluster-restore-with-tablespaces.yaml`](samples/cluster-restore-with-tablespaces.yaml)

For a list of available options, see [API reference](cloudnative-pg.v1.md).

## Pooler configuration

**Pooler with custom service config**
: [`pooler-external.yaml`](samples/pooler-external.yaml)
