# Backup and Recovery

The operator can orchestrate a continuous backup infrastructure
that is based on the [Barman](https://pgbarman.org) tool. Instead
of using the classical architecture with a Barman server which
backup many PostgreSQL instances, the operator will use the
`barman-cloud-wal-archive` and `barman-cloud-backup` tools.
As a result, base backups will be *tarballs*. Both base backups and WAL files
can be compressed and encrypted.

For this it is required an image with `barman-cli-cloud` installed. The
image `quay.io/enterprisedb/postgresql` can be used for this scope,
as it is composed by a community PostgreSQL image and the latest
`barman-cli-cloud` package.

## Cloud credentials

The backup files can be archived in any service whose API is compatible
with AWS S3. You will need the following information about your
environment:

- `ACCESS_KEY_ID`: the ID of the access key that will be used
  to upload files in S3

- `ACCESS_SECRET_KEY`: the secret part of the previous access key

- `ACCESS_SESSION_TOKEN`: the optional session token in case it is required

The access key used must have the permission to upload files in
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

## Configuring the Cluster

### S3

Given that secret, your can configure your cluster like in
the following example:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1alpha1
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

In case you're using an S3-compatible object storage, like MinIO or
Linode Object Storage, you can specify an endpoint instead of using the
default S3 one.

In this example it will use the `bucket` bucket of Linode in the region
`us-east1`.

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1alpha1
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

### MinIO Gateway

Optionally, MinIO Gateway can be used as a common interface which
relays backup objects to other cloud storage solutions, like S3, GCS or
Azure. For more information, please refer to [MinIO official documentation](https://docs.min.io/).

Specifically, the Cloud Native PostgreSQL cluster can directly point to a local
MinIO Gateway as an endpoint, using previously created credentials and service.

MinIO secrets will be used by both the PostgreSQL cluster and the MinIO instance.
Therefore they must be created in the same namespace:

```sh
kubectl create secret generic minio-creds \
  --from-literal=MINIO_ACCESS_KEY=<minio access key here> \
  --from-literal=MINIO_SECRET_KEY=<minio secret key here>
```

!!! NOTE "Note"
    Cloud Object Storage credentials will be used only by MinIO Gateway in this case.

!!! Important
    In order to allow PostgreSQL reach MinIO Gateway, it is necessary to create a
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

The MinIO deployment will the use cloud storage credentials to upload objects to the
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
apiVersion: postgresql.k8s.enterprisedb.io/v1alpha1
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

## On demand backups

To request a new backup you need to create a new Backup resource
like the following one:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1alpha1
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
Annotations:  API Version:  postgresql.k8s.enterprisedb.io/v1alpha1
Kind:         Backup
Metadata:
  Creation Timestamp:  2020-10-26T13:57:40Z
  Self Link:         /apis/postgresql.k8s.enterprisedb.io/v1alpha1/namespaces/default/backups/backup-example
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
Annotations:  API Version:  postgresql.k8s.enterprisedb.io/v1alpha1
Kind:         Backup
Metadata:
  Creation Timestamp:  2020-10-26T13:57:40Z
  Self Link:         /apis/postgresql.k8s.enterprisedb.io/v1alpha1/namespaces/default/backups/backup-example
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
    This feature will not backup the secrets for the superuser and for the
    application user. The secrets are supposed to be backed up as part of
    the standard backup procedures for the Kubernetes cluster.

## Scheduled backups

You can also schedule your backups periodically by creating a
resource named `ScheduledBackup`. The latter is similar to a
`Backup` but with an added field, named `schedule`.

This field is a [Cron](https://en.wikipedia.org/wiki/Cron) schedule
specification with a prepended field for seconds. This schedule format
is the same used in Kubernetes CronJobs.

This is an example of a scheduled backup:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1alpha1
kind: ScheduledBackup
metadata:
  name: backup-example
spec:
  schedule: "0 0 0 * * *"
  cluster:
    name: pg-backup
```

The proposed specification will schedule a backup every day at midnight.

## WAL archiving

WAL archiving is enabled as soon as you choose a destination path
and you configure your cloud credentials.

If required, you can choose to compress WAL files as soon as they
are uploaded and/or encrypt them:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1alpha1
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

The encryption can be configured directly in your bucket, and if
you don't specify otherwise in the cluster, the operator will use
that one.

## Recovery

The data uploaded to the object storage can be used to bootstrap a
new cluster from a backup. The operator will orchestrate the restore
process using the `barman-cloud-restore` tool.

When a backup is completed, the corresponding Kubernetes resource will
contain every information needed to restore it, just like in the
following example:

```text
Name:         backup-example
Namespace:    default
Labels:       <none>
Annotations:  API Version:  postgresql.k8s.enterprisedb.io/v1alpha1
Kind:         Backup
Metadata:
  Creation Timestamp:  2020-10-26T13:57:40Z
  Self Link:         /apis/postgresql.k8s.enterprisedb.io/v1alpha1/namespaces/default/backups/backup-example
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
apiVersion: postgresql.k8s.enterprisedb.io/v1alpha1
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

The operator will inject an init container in the first instance of the
cluster and the init container will start recovering the backup from the
object storage.

When the recovery process is completed, the operator will start the instance
to allow it to recover the transaction log files needed for the
consistency of the restored data directory.

Once the recovery is complete, the required superuser password will be set
into the instance. Having done that, the new primary instance will start
as usual and the remaining instances will join the cluster as replicas.

The process is transparent for the user, and managed by the instance manager
running in the Pods.
