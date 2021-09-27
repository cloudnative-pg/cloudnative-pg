# Backup and Recovery

The operator can orchestrate a continuous backup infrastructure
that is based on the [Barman](https://pgbarman.org) tool. Instead
of using the classical architecture with a Barman server, which
backup many PostgreSQL instances, the operator will use the
`barman-cloud-wal-archive` and `barman-cloud-backup` tools.
As a result, base backups will be *tarballs*. Both base backups and WAL files
can be compressed and encrypted.

For this, it is required an image with `barman-cli-cloud` installed.
You can use the image `quay.io/enterprisedb/postgresql` for this scope,
as it is composed of a community PostgreSQL image and the latest
`barman-cli-cloud` package.

A backup is performed from a primary or a designated primary instance in a
`Cluster` (please refer to
[replica clusters](replication.md#replication-from-an-external-postgresql-cluster)
for more information about designated primary instances).

!!! Warning
    Cloud Native PostgreSQL does not currently manage the deletion of backup files 
    from the backup object store. The retention policy feature will be merged from 
    Barman to Barman Cloud in the future. For the time being, it is your responsibility 
    to configure retention policies directly on the object store. 

## Cloud provider support

You can archive the backup files in any service that is supported
by the Barman Cloud infrastructure. That is:

- [AWS S3](https://aws.amazon.com/s3/)
- [Microsoft Azure Blob Storage](https://azure.microsoft.com/en-us/services/storage/blobs/).

You can also use any compatible implementation of the
supported services.

The required setup depends on the chosen storage provider and is
discussed in the following sections.

### S3

You will need the following information about your environment:

- `ACCESS_KEY_ID`: the ID of the access key that will be used
  to upload files in S3

- `ACCESS_SECRET_KEY`: the secret part of the previous access key

- `ACCESS_SESSION_TOKEN`: the optional session token in case it is required

The access key used must have permission to upload files in
the bucket. Given that, you must create a k8s secret with the
credentials, and you can do that with the following command:

```sh
kubectl create secret generic aws-creds \
  --from-literal=ACCESS_KEY_ID=<access key here> \
  --from-literal=ACCESS_SECRET_KEY=<secret key here>
# --from-literal=ACCESS_SESSION_TOKEN=<session token here> # if required
```

The credentials will be stored inside Kubernetes and will be encrypted
if encryption at rest is configured in your installation.

Given that secret, you can configure your cluster like in
the following example:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      destinationPath: "<destination path here>"
      s3Credentials:
        accessKeyId:
          name: aws-creds
          key: ACCESS_KEY_ID
        secretAccessKey:
          name: aws-creds
          key: ACCESS_SECRET_KEY
```

The destination path can be every URL pointing to a folder where
the instance can upload the WAL files, e.g.
`s3://BUCKET_NAME/path/to/folder`.

### Other S3-compatible Object Storages providers

In case you're using S3-compatible object storage, like MinIO or
Linode Object Storage, you can specify an endpoint instead of using the
default S3 one.

In this example, it will use the `bucket` bucket of Linode in the region
`us-east1`.

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      destinationPath: "<destination path here>"
      endpointURL: bucket.us-east1.linodeobjects.com
      s3Credentials:
        [...]
```

!!! Important
    Suppose you configure an Object Storage provider which uses a certificated signed with a private CA,
    like when using OpenShift or MinIO via HTTPS. In that case, you need to set the option `endpointCA`
    referring to a secret containing the CA bundle so that Barman can verify the certificate correctly.

!!! Note
    If you want ConfigMaps and Secrets to be **automatically** reloaded by instances, you can
    add a label with key `k8s.enterprisedb.io/reload` to it, otherwise you will have to reload
    the instances using the `kubectl cnp reload` subcommand.

### MinIO Gateway

Optionally, you can use MinIO Gateway as a common interface which
relays backup objects to other cloud storage solutions, like S3 or GCS.
For more information, please refer to [MinIO official documentation](https://docs.min.io/).

Specifically, the Cloud Native PostgreSQL cluster can directly point to a local
MinIO Gateway as an endpoint, using previously created credentials and service.

MinIO secrets will be used by both the PostgreSQL cluster and the MinIO instance.
Therefore you must create them in the same namespace:

```sh
kubectl create secret generic minio-creds \
  --from-literal=MINIO_ACCESS_KEY=<minio access key here> \
  --from-literal=MINIO_SECRET_KEY=<minio secret key here>
```

!!! Note
    Cloud Object Storage credentials will be used only by MinIO Gateway in this case.

!!! Important
    In order to allow PostgreSQL to reach MinIO Gateway, it is necessary to create a
    `ClusterIP` service on port `9000` bound to the MinIO Gateway instance.

For example:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: minio-gateway-service
spec:
  type: ClusterIP
  ports:
    - port: 9000
      targetPort: 9000
      protocol: TCP
  selector:
    app: minio
```

!!! Warning
    At the time of writing this documentation, the official [MinIO Operator](https://github.com/minio/minio-operator/issues/71)
    for Kubernetes does not support the gateway feature. As such, we will use a
    `deployment` instead.

The MinIO deployment will use cloud storage credentials to upload objects to the
remote bucket and relay backup files to different locations.

Here is an example using AWS S3 as Cloud Object Storage:

```yaml
apiVersion: apps/v1
kind: Deployment
[...]
    spec:
      containers:
      - name: minio
        image: minio/minio:RELEASE.2020-06-03T22-13-49Z
        args:
        - gateway
        - s3
        env:
        # MinIO access key and secret key
        - name: MINIO_ACCESS_KEY
          valueFrom:
            secretKeyRef:
              name: minio-creds
              key: MINIO_ACCESS_KEY
        - name: MINIO_SECRET_KEY
          valueFrom:
            secretKeyRef:
              name: minio-creds
              key: MINIO_SECRET_KEY
        # AWS credentials
        - name: AWS_ACCESS_KEY_ID
          valueFrom:
            secretKeyRef:
              name: aws-creds
              key: ACCESS_KEY_ID
        - name: AWS_SECRET_ACCESS_KEY
          valueFrom:
            secretKeyRef:
              name: aws-creds
              key: ACCESS_SECRET_KEY
# Uncomment the below section if session token is required
#        - name: AWS_SESSION_TOKEN
#          valueFrom:
#            secretKeyRef:
#              name: aws-creds
#              key: ACCESS_SESSION_TOKEN
        ports:
        - containerPort: 9000
```

Proceed by configuring MinIO Gateway service as the `endpointURL` in the `Cluster`
definition, then choose a bucket name to replace `BUCKET_NAME`:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      destinationPath: s3://BUCKET_NAME/
      endpointURL: http://minio-gateway-service:9000
      s3Credentials:
        accessKeyId:
          name: minio-creds
          key: MINIO_ACCESS_KEY
        secretAccessKey:
          name: minio-creds
          key: MINIO_SECRET_KEY
    [...]
```

Verify on `s3://BUCKET_NAME/` the presence of archived WAL files before
proceeding with a backup.

### Azure Blob Storage

In order to access your storage account, you will need one of the following combinations
of credentials:

- [**Connection String**](https://docs.microsoft.com/en-us/azure/storage/common/storage-configure-connection-string#configure-a-connection-string-for-an-azure-storage-account)
- **Storage account name** and [**Storage account access key**](https://docs.microsoft.com/en-us/azure/storage/common/storage-account-keys-manage)
- **Storage account name** and [**Storage account SAS Token**](https://docs.microsoft.com/en-us/azure/storage/blobs/sas-service-create).

The credentials need to be stored inside a Kubernetes Secret, adding data entries only when
needed. The following command performs that:

```
kubectl create secret generic azure-creds \
  --from-literal=AZURE_STORAGE_ACCOUNT=<storage account name> \
  --from-literal=AZURE_STORAGE_KEY=<storage account key> \
  --from-literal=AZURE_STORAGE_SAS_TOKEN=<SAS token> \
  --from-literal=AZURE_STORAGE_CONNECTION_STRING=<connection string>
```

The credentials will be encrypted at rest, if this feature is enabled in the used
Kubernetes cluster.

Given the previous secret, the provided credentials can be injected inside the cluster
configuration:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      destinationPath: "<destination path here>"
      azureCredentials:
        connectionString:
          name: azurite
          key: AZURE_CONNECTION_STRING
        storageAccount:
          name: azurite
          key: AZURE_STORAGE_ACCOUNT
        storageKey:
          name: azurite
          key: AZURE_STORAGE_KEY
        storageSasToken:
          name: azurite
          key: AZURE_STORAGE_SAS_TOKEN
```

When using the Azure Blob Storage, the `destinationPath` fulfills the following
structure:

```
<http|https>://<account-name>.<service-name>.core.windows.net/<resource-path>
```

where `<resource-path>` is `<container>/<blob>`. The **account name**,
which is also called **storage account name**, is included in the used host name.

### Other Azure Blob Storage compatible providers

If you are using a different implementation of the Azure Blob Storage APIs,
the `destinationPath` will have the following structure:

```
<http|https>://<local-machine-address>:<port>/<account-name>/<resource-path>
```

In that case, `<account-name>` is the first component of the path.

This is required if you are testing the Azure support via the Azure Storage
Emulator or [Azurite](https://github.com/Azure/Azurite).

## On-demand backups

To request a new backup, you need to create a new Backup resource
like the following one:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Backup
metadata:
  name: backup-example
spec:
  cluster:
    name: pg-backup
```

The operator will start to orchestrate the cluster to take the
required backup using `barman-cloud-backup`. You can check
the backup status using the plain `kubectl describe backup <name>`
command:

```text
Name:         backup-example
Namespace:    default
Labels:       <none>
Annotations:  API Version:  postgresql.k8s.enterprisedb.io/v1
Kind:         Backup
Metadata:
  Creation Timestamp:  2020-10-26T13:57:40Z
  Self Link:         /apis/postgresql.k8s.enterprisedb.io/v1/namespaces/default/backups/backup-example
  UID:               ad5f855c-2ffd-454a-a157-900d5f1f6584
Spec:
  Cluster:
    Name:  pg-backup
Status:
  Phase:       running
  Started At:  2020-10-26T13:57:40Z
Events:        <none>
```

When the backup has been completed, the phase will be `completed`
like in the following example:

```text
Name:         backup-example
Namespace:    default
Labels:       <none>
Annotations:  API Version:  postgresql.k8s.enterprisedb.io/v1
Kind:         Backup
Metadata:
  Creation Timestamp:  2020-10-26T13:57:40Z
  Self Link:         /apis/postgresql.k8s.enterprisedb.io/v1/namespaces/default/backups/backup-example
  UID:               ad5f855c-2ffd-454a-a157-900d5f1f6584
Spec:
  Cluster:
    Name:  pg-backup
Status:
  Backup Id:         20201026T135740
  Destination Path:  s3://backups/
  Endpoint URL:      http://minio:9000
  Phase:             completed
  s3Credentials:
    Access Key Id:
      Key:   ACCESS_KEY_ID
      Name:  minio
    Secret Access Key:
      Key:      ACCESS_SECRET_KEY
      Name:     minio
  Server Name:  pg-backup
  Started At:   2020-10-26T13:57:40Z
  Stopped At:   2020-10-26T13:57:44Z
Events:         <none>
```

!!!Important
    This feature will not backup the secrets for the superuser and the
    application user. The secrets are supposed to be backed up as part of
    the standard backup procedures for the Kubernetes cluster.

## Scheduled backups

You can also schedule your backups periodically by creating a
resource named `ScheduledBackup`. The latter is similar to a
`Backup` but with an added field, called `schedule`.

This field is a [Cron](https://en.wikipedia.org/wiki/Cron) schedule
specification with a prepended field for seconds. This schedule format
is the same used in Kubernetes CronJobs.

This is an example of a scheduled backup:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: ScheduledBackup
metadata:
  name: backup-example
spec:
  schedule: "0 0 0 * * *"
  cluster:
    name: pg-backup
```

The proposed specification will schedule a backup every day at midnight.

ScheduledBackups can be suspended if needed by setting `.spec.suspend: true`,
this will stop any new backup to be scheduled as long as the option is set to false.

In case you want to issue a backup as soon as the ScheduledBackup resource is created
you can set `.spec.immediate: true`.

## WAL archiving

WAL archiving is enabled as soon as you choose a destination path
and you configure your cloud credentials.

If required, you can choose to compress WAL files as soon as they
are uploaded and/or encrypt them:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      [...]
      wal:
        compression: gzip
        encryption: AES256
```

You can configure the encryption directly in your bucket, and the operator
will use it unless you override it in the cluster configuration.

## Recovery

You can use the data uploaded to the object storage to bootstrap a
new cluster from a backup. The operator will orchestrate the recovery
process using the `barman-cloud-restore` tool.

When a backup is completed, the corresponding Kubernetes resource will
contain every information needed to restore it, just like in the
following example:

```text
Name:         backup-example
Namespace:    default
Labels:       <none>
Annotations:  API Version:  postgresql.k8s.enterprisedb.io/v1
Kind:         Backup
Metadata:
  Creation Timestamp:  2020-10-26T13:57:40Z
  Self Link:         /apis/postgresql.k8s.enterprisedb.io/v1/namespaces/default/backups/backup-example
  UID:               ad5f855c-2ffd-454a-a157-900d5f1f6584
Spec:
  Cluster:
    Name:  pg-backup
Status:
  Backup Id:         20201026T135740
  Destination Path:  s3://backups/
  Endpoint URL:      http://minio:9000
  Phase:             completed
  s3Credentials:
    Access Key Id:
      Key:   ACCESS_KEY_ID
      Name:  minio
    Secret Access Key:
      Key:      ACCESS_SECRET_KEY
      Name:     minio
  Server Name:  pg-backup
  Started At:   2020-10-26T13:57:40Z
  Stopped At:   2020-10-26T13:57:44Z
Events:         <none>
```

Given the following cluster definition:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
metadata:
  name: cluster-restore
spec:
  instances: 3

  storage:
    size: 5Gi

  bootstrap:
    recovery:
      backup:
        name: backup-example
```

The operator will inject an init container in the first instance of the
cluster and the init container will start recovering the backup from the
object storage.

When the recovery process is completed, the operator will start the instance
to allow it to recover the transaction log files needed for the
consistency of the restored data directory.

Once the recovery is complete, the operator will set the required
superuser password into the instance. The new primary instance will start
as usual, and the remaining instances will join the cluster as replicas.

The process is transparent for the user and it is managed by the instance
manager running in the Pods.

You can optionally specify a `recoveryTarget` to perform a point in time
recovery. If left unspecified, the recovery will continue up to the latest
available WAL on the default target timeline (`current` for PostgreSQL up to
11, `latest` for version 12 and above).
