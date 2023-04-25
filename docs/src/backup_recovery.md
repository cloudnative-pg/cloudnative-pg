# Backup and Recovery

CloudNativePG natively supports **online/hot backup** of PostgreSQL
clusters through continuous physical backup and WAL archiving.
This means that the database is always up (no downtime required)
and that you can recover at any point in time from the first
available base backup in your system. The latter is normally
referred to as "Point In Time Recovery" (PITR).

The operator can orchestrate a continuous backup infrastructure
that is based on the [Barman](https://pgbarman.org) tool. Instead
of using the classical architecture with a Barman server, which
backs up many PostgreSQL instances, the operator relies on the
`barman-cloud-wal-archive`, `barman-cloud-check-wal-archive`,
`barman-cloud-backup`, `barman-cloud-backup-list`, and
`barman-cloud-backup-delete` tools. As a result, base backups will
be *tarballs*. Both base backups and WAL files can be compressed
and encrypted.

For this, it is required to use an image with `barman-cli-cloud` included.
You can use the image `ghcr.io/cloudnative-pg/postgresql` for this scope,
as it is composed of a community PostgreSQL image and the latest
`barman-cli-cloud` package.

!!! Important
    Always ensure that you are running the latest version of the operands
    in your system to take advantage of the improvements introduced in
    Barman cloud (as well as improve the security aspects of your cluster).

A backup is performed from a primary or a designated primary instance in a
`Cluster` (please refer to
[replica clusters](replica_cluster.md)
for more information about designated primary instances), or alternatively
on a [standby](#backup-from-a-standby).

## Cloud provider support

You can archive the backup files in any service that is supported
by the Barman Cloud infrastructure. That is:

- [AWS S3](https://aws.amazon.com/s3/)
- [Microsoft Azure Blob Storage](https://azure.microsoft.com/en-us/services/storage/blobs/)
- [Google Cloud Storage](https://cloud.google.com/storage/)

You can also use any compatible implementation of the
supported services.

The required setup depends on the chosen storage provider and is
discussed in the following sections.

### S3

You can define the permissions to store backups in S3 buckets in two ways:

- If CloudNativePG is running in EKS. you may want to use the
  [IRSA authentication method](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)
- Alternatively, you can use the `ACCESS_KEY_ID` and `ACCESS_SECRET_KEY` credentials

#### AWS Access key

You will need the following information about your environment:

- `ACCESS_KEY_ID`: the ID of the access key that will be used
  to upload files into S3

- `ACCESS_SECRET_KEY`: the secret part of the access key mentioned above

- `ACCESS_SESSION_TOKEN`: the optional session token, in case it is required

The access key used must have permission to upload files into
the bucket. Given that, you must create a Kubernetes secret with the
credentials, and you can do that with the following command:

```sh
kubectl create secret generic aws-creds \
  --from-literal=ACCESS_KEY_ID=<access key here> \
  --from-literal=ACCESS_SECRET_KEY=<secret key here>
# --from-literal=ACCESS_SESSION_TOKEN=<session token here> # if required
```

The credentials will be stored inside Kubernetes and will be encrypted
if encryption at rest is configured in your installation.

Once that secret has been created, you can configure your cluster like in
the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
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

The destination path can be any URL pointing to a folder where
the instance can upload the WAL files, e.g.
`s3://BUCKET_NAME/path/to/folder`.

#### IAM Role for Service Account (IRSA)

In order to use IRSA you need to set an `annotation` in the `ServiceAccount` of
the Postgres cluster.

We can configure CloudNativePG to inject them using the `serviceAccountTemplate`
stanza:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
[...]
spec:
  serviceAccountTemplate:
    metadata:
      annotations:
        eks.amazonaws.com/role-arn: arn:[...]
        [...]
```

### Other S3-compatible Object Storages providers

In case you're using S3-compatible object storage, like **MinIO** or
**Linode Object Storage**, you can specify an endpoint instead of using the
default S3 one.

In this example, it will use the `bucket` of **Linode** in the region
`us-east1`.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      destinationPath: "<destination path here>"
      endpointURL: "https://bucket.us-east1.linodeobjects.com"
      s3Credentials:
        [...]
```

In case you're using **Digital Ocean Spaces**, you will have to use the Path-style syntax.
In this example, it will use the `bucket` from **Digital Ocean Spaces** in the region `SFO3`.
```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      destinationPath: "s3://[your-bucket-name]/[your-backup-folder]/"
      endpointURL: "https://sfo3.digitaloceanspaces.com"
      s3Credentials:
        [...]
```

!!! Important
    Suppose you configure an Object Storage provider which uses a certificate signed with a private CA,
    like when using MinIO via HTTPS. In that case, you need to set the option `endpointCA`
    referring to a secret containing the CA bundle so that Barman can verify the certificate correctly.

!!! Note
    If you want ConfigMaps and Secrets to be **automatically** reloaded by instances, you can
    add a label with key `cnpg.io/reload` to the Secrets/ConfigMaps. Otherwise, you will have to reload
    the instances using the `kubectl cnpg reload` subcommand.

### MinIO Gateway

Optionally, you can use MinIO Gateway as a common interface which
relays backup objects to other cloud storage solutions, like S3 or GCS.
For more information, please refer to [MinIO official documentation](https://docs.min.io/).

Specifically, the CloudNativePG cluster can directly point to a local
MinIO Gateway as an endpoint, using previously created credentials and service.

MinIO secrets will be used by both the PostgreSQL cluster and the MinIO instance.
Therefore, you must create them in the same namespace:

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
    At the time of writing this documentation, the official
    [MinIO Operator](https://github.com/minio/minio-operator/issues/71)
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
#   - name: AWS_SESSION_TOKEN
#     valueFrom:
#       secretKeyRef:
#         name: aws-creds
#         key: ACCESS_SESSION_TOKEN
        ports:
        - containerPort: 9000
```

Proceed by configuring MinIO Gateway service as the `endpointURL` in the `Cluster`
definition, then choose a bucket name to replace `BUCKET_NAME`:

```yaml
apiVersion: postgresql.cnpg.io/v1
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
- **Storage account name** and [**Storage account SAS Token**](https://docs.microsoft.com/en-us/azure/storage/blobs/sas-service-create)
- **Storage account name** and [**Azure AD Workload Identity**](https://azure.github.io/azure-workload-identity/docs/introduction.html)
properly configured.

Using **Azure AD Workload Identity**, you can avoid saving the credentials into a Kubernetes Secret,
and have a Cluster configuration adding the `inheritFromAzureAD` as follows:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      destinationPath: "<destination path here>"
      azureCredentials:
        inheritFromAzureAD: true
```

On the other side, using both **Storage account access key** or **Storage account SAS Token**,
the credentials need to be stored inside a Kubernetes Secret, adding data entries only when
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
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      destinationPath: "<destination path here>"
      azureCredentials:
        connectionString:
          name: azure-creds
          key: AZURE_CONNECTION_STRING
        storageAccount:
          name: azure-creds
          key: AZURE_STORAGE_ACCOUNT
        storageKey:
          name: azure-creds
          key: AZURE_STORAGE_KEY
        storageSasToken:
          name: azure-creds
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

### Google Cloud Storage

Currently, the operator supports two authentication methods for Google Cloud Storage:

- the first one assumes that the pod is running inside a Google Kubernetes Engine cluster
- the second one leverages the environment variable `GOOGLE_APPLICATION_CREDENTIALS`

#### Running inside Google Kubernetes Engine

When running inside Google Kubernetes Engine you can configure your backups to
simply rely on [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity),
without having to set any credentials. In particular, you need to:

- set `.spec.backup.barmanObjectStore.googleCredentials.gkeEnvironment` to `true`
- set the `iam.gke.io/gcp-service-account` annotation in the `serviceAccountTemplate` stanza

Please use the following example as a reference:


```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  [...]
  backup:
    barmanObjectStore:
      destinationPath: "gs://<destination path here>"
      googleCredentials:
        gkeEnvironment: true

  serviceAccountTemplate:
    metadata:
      annotations:
        iam.gke.io/gcp-service-account:  [...].iam.gserviceaccount.com
        [...]
```

#### Using authentication

Following the [instruction from Google](https://cloud.google.com/docs/authentication/getting-started)
you will get a JSON file that contains all the required information to authenticate.

The content of the JSON file must be provided using a `Secret` that can be created
with the following command:

```shell
kubectl create secret generic backup-creds --from-file=gcsCredentials=gcs_credentials_file.json
```

This will create the `Secret` with the name `backup-creds` to be used in the yaml file like this:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      destinationPath: "gs://<destination path here>"
      googleCredentials:
        applicationCredentials:
          name: backup-creds
          key: gcsCredentials
```

Now the operator will use the credentials to authenticate against Google Cloud Storage.

!!! Important
    This way of authentication will create a JSON file inside the container with all the needed
    information to access your Google Cloud Storage bucket, meaning that if someone gets access to the pod
    will also have write permissions to the bucket.

## On-demand backups

To request a new backup, you need to create a new Backup resource
like the following one:

```yaml
apiVersion: postgresql.cnpg.io/v1
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
Annotations:  API Version:  postgresql.cnpg.io/v1
Kind:         Backup
Metadata:
  Creation Timestamp:  2020-10-26T13:57:40Z
  Self Link:         /apis/postgresql.cnpg.io/v1/namespaces/default/backups/backup-example
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
Annotations:  API Version:  postgresql.cnpg.io/v1
Kind:         Backup
Metadata:
  Creation Timestamp:  2020-10-26T13:57:40Z
  Self Link:         /apis/postgresql.cnpg.io/v1/namespaces/default/backups/backup-example
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

This field is a *cron schedule* specification, which follows the same
[format used in Kubernetes CronJobs](https://pkg.go.dev/github.com/robfig/cron#hdr-CRON_Expression_Format).

This is an example of a scheduled backup:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ScheduledBackup
metadata:
  name: backup-example
spec:
  schedule: "0 0 0 * * *"
  backupOwnerReference: self
  cluster:
    name: pg-backup
```

The above example will schedule a backup every day at midnight.

!!! Hint
    Backup frequency might impact your recovery time object (RTO) after a
    disaster which requires a full or Point-In-Time recovery operation. Our
    advice is that you regularly test your backups by recovering them, and then
    measuring the time it takes to recover from scratch so that you can refine
    your RTO predictability. Recovery time is influenced by the size of the
    base backup and the amount of WAL files that need to be fetched from the archive
    and replayed during recovery (remember that WAL archiving is what enables
    continuous backup in PostgreSQL!).
    Based on our experience, a weekly base backup is more than enough for most
    cases - while it is extremely rare to schedule backups more frequently than once
    a day.

ScheduledBackups can be suspended if needed by setting `.spec.suspend: true`,
this will stop any new backup to be scheduled as long as the option is set to false.

In case you want to issue a backup as soon as the ScheduledBackup resource is created
you can set `.spec.immediate: true`.

!!! Note
    `.spec.backupOwnerReference` indicates which ownerReference should be put inside
    the created backup resources.

    - *none:* no owner reference for created backup objects (same behavior as before the field was introduced)
    - *self:* sets the Scheduled backup object as owner of the backup
    - *cluster:* set the cluster as owner of the backup

## WAL archiving

WAL archiving is enabled as soon as you choose a destination path
and you configure your cloud credentials.

If required, you can choose to compress WAL files as soon as they
are uploaded and/or encrypt them:

```yaml
apiVersion: postgresql.cnpg.io/v1
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

PostgreSQL implements a sequential archiving scheme, where the
`archive_command` will be executed sequentially for every WAL
segment to be archived.

!!! Important
    By default, CloudNativePG sets `archive_timeout` to `5min`, ensuring
    that WAL files, even in case of low workloads, are closed and archived
    at least every 5 minutes, providing a deterministic time-based value for
    your Recovery Point Objective (RPO). Even though you change the value
    of the [`archive_timeout` setting in the PostgreSQL configuration](https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-ARCHIVE-TIMEOUT),
    our experience suggests that the default value set by the operator is
    suitable for most use cases.

When the bandwidth between the PostgreSQL instance and the object
store allows archiving more than one WAL file in parallel, you
can use the parallel WAL archiving feature of the instance manager
like in the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      [...]
      wal:
        compression: gzip
        maxParallel: 8
        encryption: AES256
```

In the previous example, the instance manager optimizes the WAL
archiving process by archiving in parallel at most eight ready
WALs, including the one requested by PostgreSQL.

When PostgreSQL will request the archiving of a WAL that has
already been archived by the instance manager as an optimization,
that archival request will be just dismissed with a positive status.

## Backup from a standby

Taking a base backup requires to scrape the whole data content of the
PostgreSQL instance on disk, possibly resulting in I/O contention with the
actual workload of the database.

For this reason, CloudNativePG allows you to take advantage of a
feature which is directly available in PostgreSQL: **backup from a standby**.

By default, backups will run on the most aligned replica of a `Cluster`. If
no replicas are available, backups will run on the primary instance.

!!! Info
    Although the standby might not always be up to date with the primary,
    in the time continuum from the first available backup to the last
    archived WAL this is normally irrelevant. The base backup indeed
    represents the starting point from which to begin a recovery operation,
    including PITR. Similarly to what happens with
    [`pg_basebackup`](https://www.postgresql.org/docs/current/app-pgbasebackup.html),
    when backing up from a standby we do not force a switch of the WAL on the
    primary. This might produce unexpected results in the short term (before
    `archive_timeout` kicks in) in deployments with low write activity.

If you prefer to always run backups on the primary, you can set the backup
target to `primary` as outlined in the example below:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  [...]
spec:
  backup:
    target: "primary"
```

When the backup target is set to `prefer-standby`, such policy will ensure
backups are run on the most up-to-date available secondary instance, or if no
other instance is available, on the primary instance.

By default, when not otherwise specified, target is automatically set to take
backups from a standby.

The backup target specified in the `Cluster` can be overridden in the `Backup`
and `ScheduledBackup` types, like in the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  [...]
spec:
  cluster:
    name: [...]
  target: "primary"
```

In the previous example, CloudNativePG will invariably choose the primary
instance even if the `Cluster` is set to prefer replicas.

## Recovery

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

### Restoring into a cluster with a backup section

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

## Retention policies

CloudNativePG can manage the automated deletion of backup files from
the backup object store, using **retention policies** based on the recovery
window.

Internally, the retention policy feature uses `barman-cloud-backup-delete`
with `--retention-policy “RECOVERY WINDOW OF {{ retention policy value }} {{ retention policy unit }}”`.

For example, you can define your backups with a retention policy of 30 days as
follows:

```yaml
apiVersion: postgresql.cnpg.io/v1
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
    retentionPolicy: "30d"
```

!!! Note "There's more ..."
    The **recovery window retention policy** is focused on the concept of
    *Point of Recoverability* (`PoR`), a moving point in time determined by
    `current time - recovery window`. The *first valid backup* is the first
    available backup before `PoR` (in reverse chronological order).
    CloudNativePG must ensure that we can recover the cluster at
    any point in time between `PoR` and the latest successfully archived WAL
    file, starting from the first valid backup. Base backups that are older
    than the first valid backup will be marked as *obsolete* and permanently
    removed after the next backup is completed.

## Compression algorithms

CloudNativePG by default archives backups and WAL files in an
uncompressed fashion. However, it also supports the following compression
algorithms via `barman-cloud-backup` (for backups) and
`barman-cloud-wal-archive` (for WAL files):

* bzip2
* gzip
* snappy

The compression settings for backups and WALs are independent. See the
[DataBackupConfiguration](api_reference.md#DataBackupConfiguration) and
[WALBackupConfiguration](api_reference.md#WalBackupConfiguration) sections in
the API reference.

It is important to note that archival time, restore time, and size change
between the algorithms, so the compression algorithm should be chosen according
to your use case.

The Barman team has performed an evaluation of the performance of the supported
algorithms for Barman Cloud. The following table summarizes a scenario where a
backup is taken on a local MinIO deployment. The Barman GitHub project includes
a [deeper analysis](https://github.com/EnterpriseDB/barman/issues/344#issuecomment-992547396).

| Compression | Backup Time (ms) | Restore Time (ms) | Uncompressed size (MB) | Compressed size (MB)  | Approx ratio |
|-------------|------------------|-------------------|------------------------|-----------------------|--------------|
| None        | 10927            | 7553              | 395                    | 395                   | 1:1          |
| bzip2       | 25404            | 13886             | 395                    | 67                    | 5.9:1        |
| gzip        | 116281           | 3077              | 395                    | 91                    | 4.3:1        |
| snappy      | 8134             | 8341              | 395                    | 166                   | 2.4:1        |

## Tagging of backup objects

Barman 2.18 introduces support for tagging backup resources when saving them in
object stores via `barman-cloud-backup` and `barman-cloud-wal-archive`. As a
result, if your PostgreSQL container image includes Barman with version 2.18 or
higher, CloudNativePG enables you to specify tags as key-value pairs
for backup objects, namely base backups, WAL files and history files.

You can use two properties in the `.spec.backup.barmanObjectStore` definition:

- `tags`: key-value pair tags to be added to backup objects and archived WAL
  file in the backup object store
- `historyTags`: key-value pair tags to be added to archived history files in
  the backup object store

The excerpt of a YAML manifest below provides an example of usage of this
feature:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      [...]
      tags:
        backupRetentionPolicy: "expire"
      historyTags:
        backupRetentionPolicy: "keep"
```
