This section describes the structure of a *Kubernetes manifest* to be used
to instantiate a PostgreSQL cluster using the Cloud Native PostgreSQL Operator.

A PostgreSQL cluster can be defined using a Kubernetes manifest in *YAML* according to the structure declared by the `Cluster` Custom Resource Definition.

On the top level both individual parameters and parameter groups can be defined. Parameter names are written in camelCase.

## PostgreSQL Cluster metadata

As any other object in Kubernetes, a PostgreSQL cluster has a `metadata` section which allows user to specify the following properties:

- `namespace`: a DNS compatible label used to group objects
- `name`: a string that uniquely identifies this object within the current namespace in the Kubernetes cluster

## PostgreSQL Cluster parameters

A PostgreSQL cluster object can be defined through the following parameters available in the `spec` key of the manifest:

- `affinity`: affinity/anti-affinity rules for Pods
- `backup`: configuration of the backup of the cluster. More details in the [Backup configuration](crd.md#backup-configuration) section.
- `bootstrap`: how to create this new PostgreSQL cluster. More details in the [Bootstrap](crd.md#bootstrap) section.
- `description`: description of the PostgreSQL cluster
- `imageName`: name of the container image for PostgreSQL
- `imagePullSecrets`: list of maps with secrets for pulling the PostgreSQL image
- `instances`: number of instances required in the cluster, with `instances - 1` replicas (**required**)
- `nodeMaintenanceWindow`: Define a maintenance window for the Kubernetes nodes
- `postgresql`: configuration of the PostgreSQL server.  More details in the [PostgreSQL server configuration](crd.md#postgresql-server-configuration) section.
- `primaryUpdateStrategy`: strategy to update the primary as part of a rolling update: automated (`unsupervised`)
   or manually triggered (`supervised`)
- `resources`: resources requirements of every generated Pod. More details in the [Resources](crd.md#resources) section.
- `startDelay`: allowed time in seconds for a PostgreSQL instance to successfully start up (default 30)
- `stopDelay`: allowed time in seconds for a PostgreSQL instance to gracefully shut down (default 30)
- `storage`: configuration of the storage of PostgreSQL instances. More details in the [Storage configuration](crd.md#storage-configuration) section.

## Bootstrap

The `bootstrap` section contain information about how to create this PostgreSQL cluster. The operator supports
two bootstrap types:

- `initdb`
- `fullRecovery`

The `initdb` bootstrap type is suitable to create a new cluster from scratch, while the `fullRecovery` allows the user
to restore an existing backup. When creating a cluster, only a single bootstrap method can be specified.

The operator will activate `initdb` as bootstrap method if no bootstrap type is specified.

### `initdb`

```yaml
apiVersion: postgresql.k8s.2ndq.io/v1alpha1
kind: Cluster
metadata:
  name: cluster-example-initdb
spec:
  instances: 3

  bootstrap:
    initdb:
      database: appdb
      owner: appuser

  storage:
    size: 1Gi
```

If the application database name is not specified, a database named `app` will be created. If the owner of the database
is not specified, the name of the database will be used for that.

### `fullRecovery`

```yaml
apiVersion: postgresql.k8s.2ndq.io/v1alpha1
kind: Cluster
metadata:
  name: cluster-restore
spec:
  instances: 3

  storage:
    size: 5Gi

  bootstrap:
    fullRecovery:
      backup:
        name: backup-example
```

The `fullRecovery` bootstrap method restores the backup with the specified name and, after having changed the
password with the one chosen for the superuser, will use it to bootstrap an existing cluster from the generated
primary instance.

More information about the restore feature can be found in the ["Backup and restore" page](backup_recovery.md).

## PostgreSQL server configuration

Each PostgreSQL instance can be configured in the `postgresql` section of the manifest, through the following options:

- `parameters`: PostgreSQL configuration options to be added to the `postgresql.conf` file (optional)
- `pg_hba`: PostgreSQL Host Based Authentication rules, as an array of lines to be appended to the `pg_hba.conf` file (optional)

## Resources

Cloud Native PostgreSQL allows administrators to control and manage resource usage by the pods of the cluster,
through the `resources` section of the manifest, with two knobs:

- `requests`: initial requirement
- `limits`: maximum usage, in case of dynamic increase of resource needs

For example, you can request an initial amount of RAM of 32MiB (scalable to 128MiB) and 50m of CPU (scalable to 100m) as follows:

```yaml
  resources:
    requests:
      memory: "32Mi"
      cpu: "50m"
    limits:
      memory: "128Mi"
      cpu: "100m"
```

[//]: # ( TODO: we may want to explain what happens to a pod that excedes the resource limits: CPU -> trottle; MEMORY -> kill )

!!! Seealso "Managing Compute Resources for Containers"
    For more details on resource management, please refer to the
    ["Managing Compute Resources for Containers"](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/)
    page from the Kubernetes documentation.

## Storage configuration

- `pvcTemplate`: template to be used to generate the Persistent Volume Claim
- `size`: size of the storage (*required* if not already specified in the PVC template)
- `storageClass`: `StorageClass` to use to contain PostgreSQL database (aka `PGDATA`);
   the storage class is applied after evaluating the PVC template, if available

!!! Seealso "See also"
    Please refer to the ["Configuration samples" page](samples.md) for examples on storage configuration.

## Backup configuration

You can configure backup settings of an entire cluster through the following parameters
available in the `backup` section of the `spec` key of the manifest:

- `s3Credentials`: credentials used to upload backup data to the object store
- `destinationPath`: the path where to store the backup (i.e. s3://bucket/path/to/folder),
                     automatically managed by Barman Cloud for both WAL files and base backups
- `endpointURL`: endpoint that identifies the object store (optional, overrides the automatic endpoint discovery)
- `serverName`: optional server name to be used for the backup (by default, the cluster name is used)
- `wal`: section for the configuration of WAL settings (optional)
- `data`: section for the configuration of base backup settings (optional)

For details and examples on backup configuration, please refer to the ["Backups"](backup_recovery.md) section.

### Configuration of WAL files

The `wal` section allows you to set the following options for WAL archive management:

- `compression`: compress a WAL file before sending it to the object store. Available options are empty string (no compression, default), `gzip` or `bzip2`.
- `encryption`:  enable server-side encryption (encryption at rest) for object store using the given method. Allowed options are empty string (use the bucket policy, default), `AES256` and `aws:kms`.

!!! Warning
    Without a `wal` section, WAL files will be stored uncompressed and may be unencrypted in the object store.

### Configuration of base backups

The `data` section allows you to set the following options for base backup management:

- `compression`: compress a backup file (a tar file per tablespace) while streaming it to the object store. Available options are empty string (no compression, default), `gzip` or `bzip2`.
- `encryption`:  enable server-side encryption (encryption at rest) for object store with the given method. Allowed options are empty string (use the bucket policy, default), `AES256` and `aws:kms`.
- `immediateCheckpoint`:  control whether the I/O workload for the backup initial checkpoint will be limited, according to the `checkpoint_completion_target` setting on the PostgreSQL server.
  If set to true, an immediate checkpoint will be used, meaning PostgreSQL will complete the checkpoint as soon as possible. (optional, `false` by default).
- `jobs`: number of parallel jobs to upload the backup (default 2).

!!! Warning
    Without a `data` section, base backup file will be stored uncompressed and may be unencrypted in the object store.

