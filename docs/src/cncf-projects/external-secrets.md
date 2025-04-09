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

<!--
## Example: Integration with an external KMS

TODO

-->
