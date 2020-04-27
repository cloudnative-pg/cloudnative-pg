The operator can orchestrate a continuous backup infrastructure
that is based on the [Barman](https://pgbarman.org) tool. Instead
of using the classical architecture with a Barman server which
backup many PostgreSQL instances, the operator will use the
`barman-cloud-wal-archive` and `barman-cloud-backup` tools.

For this it is required an image with `barman-cli-cloud` installed. The
image named `2ndquadrant/postgresql` can be used for this scope,
as it is composed by a community PostgreSQL image and the latest
`barman-cli-cloud` package.

## Cloud credentials

The backup files can be archived in any service whose API is compatible
with AWS S3. You will need the following information about your
environment:

- `ACCESS_KEY_ID`: the ID of the access key that will be used
  to upload files in S3
  
- `ACCESS_SECRET_KEY`: the secret part of the previous access key

The access key used must have the permission to upload files in
the bucket. Given that, you must create a k8s secret with the
credentials, and you can do that with the following command:

```sh
kubectl create secret generic aws-creds \
  --from-literal=ACCESS_KEY_ID=<access key here> \
  --from-literal=ACCESS_SECRET_KEY=<secret key here>
```

The credentials will be stored inside Kubernetes and will be encrypted
if encryption at rest is configured in your installation.

## Configuring the Cluster

### S3

Given that secret, your can configure your cluster like in
the following example:

```yaml
apiVersion: postgresql.k8s.2ndq.io/v1alpha1
kind: Cluster
[...]
spec:
  backup:
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

In case you're using an S3-compatible object storage, like minio or
Linode Object Storage, you can specify an endpoint instead of using the
default S3 one.

In this example it will use the `bucket` bucket of Linode in the region
`us-east1`.

```yaml
apiVersion: postgresql.k8s.2ndq.io/v1alpha1
kind: Cluster
[...]
spec:
  backup:
    destinationPath: "<destination path here>"
    endpointURL: bucket.us-east1.linodeobjects.com
    s3Credentials:
      [...]
```

## On demand backups

To request a new backup you need to create a new Backup resource
like the following one:

```yaml
apiVersion: postgresql.k8s.2ndq.io/v1alpha1
kind: Backup
metadata:
  name: backup-example
spec:
  cluster:
    name: postgresql-bkp
```

The operator will start to orchestrate the cluster to take the
required backup using `barman-cloud-backup`. You can check
the backup status using the plain `kubectl describe backup <name>`
command:

```text
Name:         backup-example
Namespace:    default
Labels:       <none>
Annotations:  API Version:  postgresql.k8s.2ndq.io/v1alpha1
Kind:         Backup
Metadata:
  Creation Timestamp:  2020-04-08T13:45:04Z
  Generation:          1
  Resource Version:    148183
  Self Link:           /apis/postgresql.k8s.2ndq.io/v1alpha1/namespaces/default/backups/backup-example
  UID:                 e32f9190-e0c8-4424-9456-9e53d26a9c83
Spec:
  Cluster:
    Name:  postgresql-bkp
Status:
  Phase:       running
  Started At:  2020-04-08T13:45:04Z
Events:        <none>
```

When the backup has been completed, the phase will be `completed`
like in the following example:

```text
Name:         backup-example
Namespace:    default
Labels:       <none>
Annotations:  API Version:  postgresql.k8s.2ndq.io/v1alpha1
Kind:         Backup
Metadata:
  Creation Timestamp:  2020-04-08T13:45:04Z
  Generation:          1
  Resource Version:    148260
  Self Link:           /apis/postgresql.k8s.2ndq.io/v1alpha1/namespaces/default/backups/backup-example
  UID:                 e32f9190-e0c8-4424-9456-9e53d26a9c83
Spec:
  Cluster:
    Name:  postgresql-bkp
Status:
  Phase:       completed
  Started At:  2020-04-08T13:45:04Z
  Stopped At:  2020-04-08T13:45:33Z
Events:        <none>
```

## Scheduled backups

You can also schedule your backups periodically by creating a
resource named `ScheduledBackup`. The latter is similar to a
`Backup` but with an added field, named `schedule`.

This field is a [Cron](https://en.wikipedia.org/wiki/Cron) schedule
specification with a prepended field for seconds. This schedule format
is the same used in Kubernetes CronJobs.

This is an example of a scheduled backup:

```yaml
apiVersion: postgresql.k8s.2ndq.io/v1alpha1
kind: ScheduledBackup
metadata:
  name: backup-example
spec:
  schedule: "0 0 0 * * *"
  cluster:
    name: postgresql-bkp
```

The proposed specification will schedule a backup every day at midnight.

## WAL archiving

WAL archiving is enabled as soon as you choose a destination path
and you configure your cloud credentials.

If required, you can choose to compress WAL files as soon as they
are uploaded and/or encrypt them:

```yaml
apiVersion: postgresql.k8s.2ndq.io/v1alpha1
kind: Cluster
[...]
spec:
  backup:
    [...]
    wal:
      compression: gzip
      encryption: SHA256
```

The encryption can be configured directly in your bucket, and if
you don't specify otherwise in the cluster, the operator will use
that one.
