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

Basic cluster
:  [`cluster-example.yaml`](samples/cluster-example.yaml)
   A basic example of a cluster.

Custom cluster
:  [`cluster-example-custom.yaml`](samples/cluster-example-custom.yaml)
   A basic cluster that uses the default storage class and custom parameters for
   the `postgresql.conf` and `pg_hba.conf` files.

Customized storage class
: [`cluster-storage-class.yaml`](samples/cluster-storage-class.yaml):
   A basic cluster that uses a specified storage class of `standard`.

Customized storage class and backups
:   **Prerequisites**: bucket storage should be available. The sample config is for AWS,
    please change to suit your setup
: [`cluster-storage-class-with-backup.yaml`](samples/cluster-storage-class-with-backup.yaml) a cluster
   with backups configured

Backup
:   **Prerequisites**: [`cluster-storage-class-with-backup.yaml`](samples/cluster-storage-class-with-backup.yaml)
    applied and Healthy
: [`backup-example.yaml`](samples/backup-example.yaml):
  An example of a backup that runs against the previous sample.

Cluster with PVC (Persistent Volume Claim) configured
: [`cluster-pvc-template.yaml`](samples/cluster-pvc-template.yaml):
   a basic cluster that with an explicit persistent volume claim template.

Full example
: [`cluster-example-full.yaml`](samples/cluster-example-full.yaml):
   a cluster that sets most of the available options.

PostGIS example
: [`postgis-example.yaml`](samples/postgis-example.yaml):
   an example of "PostGIS cluster" (see the [PostGIS section](postgis.md) for details.)

Replica cluster via streaming (pg_basebackup)
:   **Prerequisites**: [`cluster-example.yaml`](samples/cluster-example.yaml)
    applied and Healthy
:   [`cluster-example-replica-streaming.yaml`](samples/cluster-example-replica-streaming.yaml): a replica cluster following `cluster-example` with streaming replication.

Simple cluster with backup configured
:   **Prerequisites**: The configuration assumes `minio` is running and working.
    Please update `backup.barmanObjectStore` with your `minio` parameters or your cloud solution
:  [`cluster-example-with-backup.yaml`](samples/cluster-example-with-backup.yaml)
   A basic cluster with backups configured.

Replica cluster via Backup from an object store
:   **Prerequisites**:
    [`cluster-storage-class-with-backup.yaml`](samples/cluster-storage-class-with-backup.yaml) applied and Healthy.
    And a backup
    [`cluster-example-trigger-backup.yaml`](samples/cluster-example-trigger-backup.yaml)
    applied and completed.
: [`cluster-example-replica-from-backup-simple.yaml`](samples/cluster-example-replica-from-backup-simple.yaml):
   A replica cluster following a cluster with backup configured.

Replica cluster via Volume Snapshot
:   **Prerequisites**:
    [`cluster-example-with-volume-snapshot.yaml`](samples/cluster-example-with-volume-snapshot.yaml) applied and Healthy.
    And a volume snapshot
    [`backup-with-volume-snapshot.yaml`](samples/backup-with-volume-snapshot.yaml)
    applied and completed.
: [`cluster-example-replica-from-volume-snapshot.yaml`](samples/cluster-example-replica-from-volume-snapshot.yaml):
   A replica cluster following a cluster with volume snapshot configured.

Bootstrap cluster with SQL files
: [`cluster-example-initdb-sql-refs.yaml`](samples/cluster-example-initdb-sql-refs.yaml):
   a cluster example that will execute a set of queries defined in a Secret and a ConfigMap right after the database is created.

Sample cluster with customized `pg_hba` configuration
: [`cluster-example-pg-hba.yaml`](samples/cluster-example-pg-hba.yaml):
  a basic cluster that enables user `app` to authenticate using certificates.

Sample cluster with Secret and Configmap mounted using projected volume template
: [`cluster-example-projected-volume.yaml`](samples/cluster-example-projected-volume.yaml)
  a basic cluster with existing Secret and Configmap mounted into Postgres pod using projected volume mount.

Cluster with declarative role management
: [`cluster-example-with-roles.yaml`](samples/cluster-example-with-roles.yaml):
  Declares a role with the `managed` stanza. Includes password management with
  Kubernetes secrets.

For a list of available options, see [API reference](cloudnative-pg.v1.md).
