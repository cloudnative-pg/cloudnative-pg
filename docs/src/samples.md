# Examples

In this section, you can find some examples of configuration files to set up
your PostgreSQL Cluster.

!!! Important
    These are here for demonstration and experimentation
    purposes, and can be executed on a personal Kubernetes cluster with Minikube
    or Kind as described in the ["Quickstart"](quickstart.md).

Basic cluster
:  [`cluster-example.yaml`](samples/cluster-example.yaml)
   a basic example of  a cluster.

Custom cluster
:  [`cluster-example-custom.yaml`](samples/cluster-example-custom.yaml)
   a basic cluster that uses the default storage class and custom parameters for
   the `postgresql.conf` and `pg_hba.conf` files.

Customized storage class
: [`cluster-storage-class.yaml`](samples/cluster-storage-class.yaml):
   a basic cluster that uses a specified storage class of `standard`.

Customized storage class and backups
:   **Prerequisites**: bucket storage should be available. The sample config is for AWS,
    please change to suit your setup
: [`cluster-storage-class-with-backup.yaml`](samples/cluster-storage-class-with-backup.yaml) a cluster
   with backups configured

Backup
:   **Prerequisites**: [`cluster-storage-class-with-backup.yaml`](samples/cluster-storage-class-with-backup.yaml)
    applied and Healthy
: [`backup-example.yaml`](samples/backup-example.yaml):
  an example of a backup that runs against the previous sample

Cluster with PVC (Persistent Volume Claim) configured
: [`cluster-pvc-template.yaml`](samples/cluster-pvc-template.yaml):
   a basic cluster that with an explicit persistent volume claim template.

Full example
: [`cluster-example-full.yaml`](samples/cluster-example-full.yaml):
   a cluster that sets most of the available options.

PostGIS example
: [`postgis-example.yaml`](samples/postgis-example.yaml):
   an example of "PostGIS cluster" (see the [PostGIS section](postgis.md) for details.)

Replica cluster via streaming
:   **Prerequisites**: [`cluster-example.yaml`](samples/cluster-example.yaml)
    applied and Healthy
:   [`cluster-example-replica-streaming.yaml`](samples/cluster-example-replica-streaming.yaml): a replica cluster following `cluster-example` with streaming replication.

Simple cluster with backup configured
:   **Prerequisites**: The configuration assumes `minio` is running and working.
    Please update `backup.barmanObjectStore` with your `minio` parameters or your cloud solution
:  [`cluster-example-with-backup.yaml`](samples/cluster-example-with-backup.yaml)
   a basic cluster with backups configured.

Replica cluster via backup
:   **Prerequisites**:
    [`cluster-storage-class-with-backup.yaml`](samples/cluster-storage-class-with-backup.yaml) applied and Healthy.
    And a backup
    [`cluster-example-trigger-backup.yaml`](samples/cluster-example-trigger-backup.yaml)
    applied and Completed.
: [`cluster-example-replica-from-backup-simple.yaml`](samples/cluster-example-replica-from-backup-simple.yaml):
   a replica cluster following a cluster with backup configured.

Bootstrap cluster with SQL files
: [`cluster-example-initdb-sql-refs.yaml`](samples/cluster-example-initdb-sql-refs.yaml):
   a cluster example that will execute a set of queries defined in a Secret and a ConfigMap right after the database is created.


For a list of available options, please refer to the ["API Reference" page](api_reference.md).
