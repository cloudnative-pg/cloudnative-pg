# Cloud Native PostgreSQL

**Cloud Native PostgreSQL** is a stack designed by
[EnterpriseDB](https://www.enterprisedb.com) to manage PostgreSQL
workloads on Kubernetes, particularly optimized for private cloud environments
with local Persistent Volumes (PV).

## Table of content

- [Code of conduct](CODE-OF-CONDUCT.md)
- [Contributing](CONTRIBUTING.md)
- [License](LICENSE)
- [Setting up your dev workstation](DEV.md)
- [Setting up a local k8s cluster to test your code](hack/e2e/README.md#setting-up-a-local-k8s-cluster)
- [E2E tests (how-to)](hack/e2e/README.md#e2e-testing)
- [Release process (how-to)](RELEASE.md)

## How to upgrade the list of licenses

To generate or update the `licenses` folder run the following command:

```bash
make licenses
```
