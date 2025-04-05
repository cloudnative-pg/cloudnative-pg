# External Secrets

[External Secrets](https://external-secrets.io/latest/) is a CNCF Sandbox
project, accepted in 2022 under the sponsorship of TAG Security.

The **External Secrets Operator (ESO)** is a Kubernetes operator that enhances
secret management by decoupling the storage of secrets from Kubernetes itself.
It enables seamless synchronization between external secret management systems
and native Kubernetes `Secret` resources.

ESO supports a wide range of backends, including:

- [AWS Secrets Manager](https://aws.amazon.com/secrets-manager/)
- [HashiCorp Vault](https://www.vaultproject.io/)
- [Google Secret Manager](https://cloud.google.com/secret-manager)
- [Azure Key Vault](https://azure.microsoft.com/en-us/services/key-vault/)
- [IBM Cloud Secrets Manager](https://www.ibm.com/cloud/secrets-manager)
- [Akeyless](https://akeyless.io)
- [CyberArk Conjur](https://www.conjur.org)
- [Pulumi ESC](https://www.pulumi.com/product/esc/)
- And many others

## Integration with PostgreSQL and CloudNativePG

When it comes to PostgreSQL databases, External Secrets integrates seamlessly
with [CloudNativePG](https://cloudnative-pg.io/) in two major use cases:

- **Automated password management:** ESO can handle the automatic generation
  and refresh of database user passwords stored in Kubernetes `Secret`
  resources, ensuring that applications running inside the cluster always have
access to up-to-date credentials.

- **Cross-platform secret access:** It enables transparent synchronization of
  those passwords with an external Key Management Service (KMS). This allows
  applications and developers outside the Kubernetes cluster—who may not have
  access to Kubernetes secrets—to retrieve the database credentials directly from
  the external KMS.

## Example of Automated Password Management

TODO


