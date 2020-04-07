The operator can orchestrate a continuous backup infrastructure
that is based on the [Barman](https://pgbarman.org) tool. Instead
of using the classical architecture with a Barman server which
backup many PostgreSQL instances, the operator will use the
`barman-cloud-wal-archive` and `barman-cloud-backup` tools.

To that, an image containing Barman installed is needed. The
image named `2ndquadrant/postgresql` can be used for this scope,
as it is composed by a community PostgreSQL image and the latest
Barman package. 

# How to manage your cloud credentials

The backup files can be archived in any service whose API is compatible
with AWS S3. You will need the following information about your
environment:

- `ACCESS_KEY_ID`: the ID of the access key that will be used
  to upload files in S3
  
- `ACCESS_SECRET_KEY`: the secret part of the previous access key

The access key used must have the permission to upload files in
the bucket. Given that, you must create a k8s secret with the
credentials, and you can do that with the following command:

    kubectl create secret generic aws-creds \ 
      --from-literal=ACCESS_KEY_ID=<access key here> \
      --from-literal=ACCESS_SECRET_KEY=<secret key here>

The credentials will be stored inside Kubernetes and encrypted
if supported by your installation.

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
the instance can upload the WAL files, i.e.
`s3://BUCKET_NAME/path/to/folder`.

# On demand backups

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


# Scheduled backups

TODO

# WAL archiving

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

