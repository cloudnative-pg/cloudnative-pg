---
id: object_stores
title: Appendix C - Common object stores for backups
---

# Appendix C - Common object stores for backups
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

:::warning
    As of CloudNativePG 1.26, **native Barman Cloud support is deprecated** in
    favor of the **Barman Cloud Plugin**. While the native integration remains
    functional for now, we strongly recommend beginning a gradual migration to
    the plugin-based interface after appropriate testing. The Barman Cloud
    Plugin documentation describes
    [how to use common object stores](https://cloudnative-pg.io/plugin-barman-cloud/docs/object_stores/).
:::

You can store the [backup](../backup.md) files in any service that is supported
by the Barman Cloud infrastructure. That is:

- [Amazon S3](#aws-s3)
- [Microsoft Azure Blob Storage](#azure-blob-storage)
- [Google Cloud Storage](#google-cloud-storage)

You can also use any compatible implementation of the supported services.

The required setup depends on the chosen storage provider and is
discussed in the following sections.

## AWS S3

[AWS Simple Storage Service (S3)](https://aws.amazon.com/s3/) is
a very popular object storage service offered by Amazon.

As far as CloudNativePG backup is concerned, you can define the permissions to
store backups in S3 buckets in two ways:

- If CloudNativePG is running in EKS. you may want to use the
  [IRSA authentication method](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)
- Alternatively, you can use the `ACCESS_KEY_ID` and `ACCESS_SECRET_KEY` credentials

### AWS Access key

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

### IAM Role for Service Account (IRSA)

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

### S3 lifecycle policy

Barman Cloud writes objects to S3, then does not update them until they are
deleted by the Barman Cloud retention policy. A recommended approach for an S3
lifecycle policy is to expire the current version of objects a few days longer
than the Barman retention policy, enable object versioning, and expire
non-current versions after a number of days. Such a policy protects against
accidental deletion, and also allows for restricting permissions to the
CloudNativePG workload so that it may delete objects from S3 without granting
permissions to permanently delete objects.

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
      destinationPath: "s3://bucket/"
      endpointURL: "https://us-east1.linodeobjects.com"
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

### Using Object Storage with a private CA

Suppose you configure an Object Storage provider which uses a certificate
signed with a private CA, for example when using MinIO via HTTPS. In that case,
you need to set the option `endpointCA` inside `barmanObjectStore` referring
to a secret containing the CA bundle, so that Barman can verify the certificate
correctly.
You can find instructions on creating a secret using your cert files in the
[certificates](../certificates.md#example) document.
Once you have created the secret, you can populate the `endpointCA` as in the
following example:

``` yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  [...]
  backup:
    barmanObjectStore:
      endpointURL: <myEndpointURL>
      endpointCA:
        name: my-ca-secret
        key: ca.crt
```

:::note
    If you want ConfigMaps and Secrets to be **automatically** reloaded by instances, you can
    add a label with key `cnpg.io/reload` to the Secrets/ConfigMaps. Otherwise, you will have to reload
    the instances using the `kubectl cnpg reload` subcommand.
:::

## Azure Blob Storage

[Azure Blob Storage](https://azure.microsoft.com/en-us/services/storage/blobs/) is the
object storage service provided by Microsoft.

In order to access your storage account for backup and recovery of
CloudNativePG managed databases, you will need one of the following
combinations of credentials:

- [Connection String](https://docs.microsoft.com/en-us/azure/storage/common/storage-configure-connection-string#configure-a-connection-string-for-an-azure-storage-account)
- Storage account name and [Storage account access key](https://docs.microsoft.com/en-us/azure/storage/common/storage-account-keys-manage)
- Storage account name and [Storage account SAS Token](https://docs.microsoft.com/en-us/azure/storage/blobs/sas-service-create)
- Storage account name and [Azure AD Workload Identity](https://azure.github.io/azure-workload-identity/docs/introduction.html)
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

``` sh
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

``` sh
<http|https>://<account-name>.<service-name>.core.windows.net/<resource-path>
```

where `<resource-path>` is `<container>/<blob>`. The **account name**,
which is also called **storage account name**, is included in the used host name.

### Other Azure Blob Storage compatible providers

If you are using a different implementation of the Azure Blob Storage APIs,
the `destinationPath` will have the following structure:

``` sh
<http|https>://<local-machine-address>:<port>/<account-name>/<resource-path>
```

In that case, `<account-name>` is the first component of the path.

This is required if you are testing the Azure support via the Azure Storage
Emulator or [Azurite](https://github.com/Azure/Azurite).

## Google Cloud Storage

Currently, the CloudNativePG operator supports two authentication methods for
[Google Cloud Storage](https://cloud.google.com/storage/):

- the first one assumes that the pod is running inside a Google Kubernetes Engine cluster
- the second one leverages the environment variable `GOOGLE_APPLICATION_CREDENTIALS`

### Running inside Google Kubernetes Engine

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

### Using authentication

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

:::info[Important]
    This way of authentication will create a JSON file inside the container with all the needed
    information to access your Google Cloud Storage bucket, meaning that if someone gets access to the pod
    will also have write permissions to the bucket.
:::