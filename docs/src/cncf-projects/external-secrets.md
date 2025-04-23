# External Secrets

[External Secrets](https://external-secrets.io/latest/) is a CNCF Sandbox
project, accepted in 2022 under the sponsorship of TAG Security.

## About

The **External Secrets Operator (ESO)** is a Kubernetes operator that enhances
secret management by decoupling the storage of secrets from Kubernetes itself.
It enables seamless synchronization between external secret management systems
and native Kubernetes `Secret` resources.

ESO supports a wide range of backends, including:

- [HashiCorp Vault](https://www.vaultproject.io/)
- [AWS Secrets Manager](https://aws.amazon.com/secrets-manager/)
- [Google Secret Manager](https://cloud.google.com/secret-manager)
- [Azure Key Vault](https://azure.microsoft.com/en-us/services/key-vault/)
- [IBM Cloud Secrets Manager](https://www.ibm.com/cloud/secrets-manager)

…and many more. For a full and up-to-date list of supported providers, refer to
the [official External Secrets documentation](https://external-secrets.io/latest/).

## Integration with PostgreSQL and CloudNativePG

When it comes to PostgreSQL databases, External Secrets integrates seamlessly
with [CloudNativePG](https://cloudnative-pg.io/) in two major use cases:

- **Automated password management:** ESO can handle the automatic generation
  and rotation of database user passwords stored in Kubernetes `Secret`
  resources, ensuring that applications running inside the cluster always have
  access to up-to-date credentials.

- **Cross-platform secret access:** It enables transparent synchronization of
  those passwords with an external Key Management Service (KMS) via a
  `SecretStore` resources. This allows applications and developers outside the
  Kubernetes cluster—who may not have access to Kubernetes secrets—to retrieve
  the database credentials directly from the external KMS.

## Example: Automated Password Management with External Secrets

Let’s walk through how to automatically rotate the password of the `app` user
every 24 hours in the `cluster-example` Postgres cluster from the
[quickstart guide](../quickstart.md#part-3-deploy-a-postgresql-cluster).

!!! Important
    Before proceeding, ensure that the `cluster-example` Postgres cluster is up
    and running in your environment.

By default, CloudNativePG generates and manages a Kubernetes `Secret` named
`cluster-example-app`, which contains the credentials for the `app` user in the
`cluster-example` cluster. You can read more about this in the
[“Connecting from an application” section](../applications.md#secrets).

With External Secrets, the goal is to:

1. Define a `Password` generator that specifies how to generate the password.
2. Create an `ExternalSecret` resource that keeps the `cluster-example-app`
   secret in sync by updating only the `password` and `pgpass` fields.

### Creating the Password Generator

The following example creates a
[`Password` generator](https://external-secrets.io/main/api/generator/password/)
resource named `pg-password-generator` in the `default` namespace. You can
customize the name and properties to suit your needs:

```yaml
apiVersion: generators.external-secrets.io/v1alpha1
kind: Password
metadata:
  name: pg-password-generator
spec:
  length: 42
  digits: 5
  symbols: 5
  symbolCharacters: "-_$@"
  noUpper: false
  allowRepeat: true
```

This specification defines the characteristics of the generated password,
including its length and the inclusion of digits, symbols, and uppercase
letters.

### Creating the External Secret

The example below creates an `ExternalSecret` resource named
`cluster-example-app-secret`, which refreshes the password every 24 hours. It
uses a `Merge` policy to update only the specified fields (`password` and
`pgpass`) in the `cluster-example-app` secret.

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: cluster-example-app-secret
spec:
  refreshInterval: "24h"
  target:
    name: cluster-example-app
    creationPolicy: Merge
    template:
      metadata:
        labels:
          cnpg.io/reload: "true"
      data:
        password: "{{ .password }}"
        pgpass: "cluster-example-rw:5432:app:app:{{ .password }}"
  dataFrom:
    - sourceRef:
        generatorRef:
          apiVersion: generators.external-secrets.io/v1alpha1
          kind: Password
          name: pg-password-generator
```

The label `cnpg.io/reload: "true"` ensures that CloudNativePG triggers a reload
of the user password in the database when the secret changes.

### Verifying the Configuration

To check that the `ExternalSecret` is correctly synchronizing:

```sh
kubectl get es cluster-example-app-secret
```

To observe the password being refreshed in real time, temporarily reduce the
`refreshInterval` to `30s` and run the following command repeatedly:

```sh
kubectl get secret cluster-example-app \
  -o jsonpath="{.data.password}" | base64 -d
```

You should see the password change every 30 seconds, confirming that the
rotation is working correctly.

### There's More

While the example above focuses on the default `cluster-example-app` secret
created by CloudNativePG, the same approach can be extended to manage any
custom secrets or PostgreSQL users you create to regularly rotate their
password.


## Example: Integration with an external KMS

One of the popular KMS (Key Management Service) providers in the CNCF ecosystem is
[HashiCorp Vault](https://www.vaultproject.io/), in the following example we will
see how we can integrate CloudNativePG, External Secrets Operator and HashiCorp Vault to
automatically rotate the password and store it in Vault.

!!! Important
    We assume that you have already installed and configured HashiCorp Vault in your
    environment. For more information, refer to the [HashiCorp Vault documentation](https://www.vaultproject.io/docs).

Following the previous example, we will create the required `SecretStore` and `PushSecret`
objects required to integrate with HashiCorp Vault.

### Creating the SecretStore

The HashiCorp Vault will be access from the namespace in the following URL `http://vault.vault.svc:8200`
and we presume there's a `Secret` object with the name `vault-token` in the same namespace
that contains the token to access the Vault.

```yaml
apiVersion: external-secrets.io/v1beta1
kind: SecretStore
metadata:
  name: vault-backend
spec:
  provider:
    vault:
      server: "http://vault.vault.svc:8200"
      path: "secrets"
      # Version is the Vault KV secret engine version.
      # This can be either "v1" or "v2", defaults to "v2"
      version: "v2"
      auth:
        # points to a secret that contains a vault token
        # https://www.vaultproject.io/docs/auth/token
        tokenSecretRef:
          name: "vault-token"
          key: "token"
```

Now we have the required `SecretStore` object with the name `vault-backend`.

!!! Important
    On the HashiCorp Vault is required to have the `secrets` path on the `kv` engine with version 2,
    these names may not be the same in your current implementation of HashiCorp Vault, so
    please adjust the `path` and `version` accordingly.

### Creating the PushSecret

The `PushSecret` object is used to push the secret to the HashiCorp Vault, we have a simplified
configuration that will work with our sample cluster `cluster-example` and the `app` user. For
more configurations on the `PushSecret` object, please refer to the https://external-secrets.io/latest/api/pushsecret/

```yaml
apiVersion: external-secrets.io/v1alpha1
kind: PushSecret
metadata:
  name: pushsecret-example
spec:
  deletionPolicy: Delete
  refreshInterval: 24h
  secretStoreRefs:
    - name: vault-backend
      kind: SecretStore
  selector:
    secret:
      name: cluster-example-app
  data:
    - match:
        remoteRef:
          remoteKey: cluster-example-app
```

Here we are using the `PushSecret` object to push the `cluster-example-app` secret to the
HashiCorp Vault, the `remoteKey` is the name of the secret in the HashiCorp Vault referenced
by the `SecretStore` with the name `vault-backend`.

### Verifying the Configuration

To check that the `PushSecret` is correctly synchronizing, you can go to the HashiCorp Vault
UI and in the secret engine `kv` with the path `secrets` you should be able to see a secret
with the name `cluster-example-app` as it was referenced in the `remoteKey` before.
