---
id: security
sidebar_position: 100
title: Security
---

# Security
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

This section contains information about security for CloudNativePG,
that are analyzed at 3 different layers: Code, Container and Cluster.

:::warning
    The information contained in this page must not exonerate you from
    performing regular InfoSec duties on your Kubernetes cluster. Please
    familiarize yourself with the ["Overview of Cloud Native Security"](https://kubernetes.io/docs/concepts/security/overview/)
    page from the Kubernetes documentation.
:::

:::note[About the 4C's Security Model]
    Please refer to ["The 4C’s Security Model in Kubernetes"](https://www.enterprisedb.com/blog/4cs-security-model-kubernetes)
    blog article to get a better understanding and context of the approach EDB
    has taken with security in CloudNativePG.
:::

## Code

CloudNativePG's source code undergoes systematic static analysis, including
checks for security vulnerabilities, using the popular open-source linter for
Go, [GolangCI-Lint](https://github.com/golangci/golangci-lint), directly
integrated into the CI/CD pipeline. GolangCI-Lint can run multiple linters on
the same source code.

The following tools are used to identify security issues:

- **[Golang Security Checker](https://github.com/securego/gosec) (`gosec`):** A
  linter that scans the abstract syntax tree of the source code against a set
  of rules designed to detect known vulnerabilities, threats, and weaknesses,
  such as hard-coded credentials, integer overflows, and SQL injections.
  GolangCI-Lint runs `gosec` as part of its suite.

- **[govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck):** This
  tool runs in the CI/CD pipeline and reports known vulnerabilities affecting
  Go code or the compiler. If the operator is built with a version of the Go
  compiler containing a known vulnerability, `govulncheck` will detect it.

- **[CodeQL](https://codeql.github.com/):** Provided by GitHub, this tool scans
  for security issues and blocks any pull request with detected
  vulnerabilities. CodeQL is configured to review only Go code, excluding other
  languages in the repository such as Python or Bash.

- **[Snyk](https://snyk.io/):** Conducts nightly code scans in a scheduled job
  and generates weekly reports highlighting any new findings related to code
  security and licensing issues.

The CloudNativePG repository has the *"Private vulnerability reporting"* option
enabled in the [Security section](https://github.com/cloudnative-pg/cloudnative-pg/security).
This feature allows users to safely report security issues that require careful
handling before being publicly disclosed. If you discover any security bug,
please use this medium to report it.

:::info[Important]
    A failure in the static code analysis phase of the CI/CD pipeline will
    block the entire delivery process of CloudNativePG. Every commit must pass all
    the linters defined by GolangCI-Lint.
:::

## Container

Every container image in CloudNativePG is automatically built via CI/CD
pipelines after every commit. These images include not only the operator's
image but also the operands' images, specifically for every supported
PostgreSQL version.

:::info[Important]
    All operand images are automatically and regularly rebuilt by our pipelines
    to incorporate the latest security updates at both the base image and package
    levels. This ensures that container images distributed to the community receive
    **patch-level updates** regularly.
:::

During the CI/CD process, images are scanned using the following tools:

- **[Dockle](https://github.com/goodwithtech/dockle):** Ensures best practices
  in the container build process.
- **[Snyk](https://snyk.io/):** Detects security issues within the container
  and reports findings via the GitHub interface.

### Image Signatures

The operator and [operand
images](https://github.com/cloudnative-pg/postgres-containers) are
cryptographically signed using [cosign](https://github.com/sigstore/cosign), a
signature tool from [sigstore](https://www.sigstore.dev/).
This process is automated via GitHub Actions and leverages
[short-lived tokens issued through OpenID Connect](https://docs.github.com/en/actions/security-for-github-actions/security-hardening-your-deployments/about-security-hardening-with-openid-connect).

The token issuer is `https://token.actions.githubusercontent.com`, and the
signing identity corresponds to a GitHub workflow executed under the
[cloudnative-pg](https://github.com/cloudnative-pg/cloudnative-pg/) repository.
This workflow uses the [cosign-installer action](https://github.com/marketplace/actions/cosign-installer)
to streamline the signing process.

To verify the authenticity of an operator image, use the following `cosign`
command with the image digest:

```shell
cosign verify ghcr.io/cloudnative-pg/cloudnative-pg@sha256:<DIGEST> \
  --certificate-identity-regexp="^https://github.com/cloudnative-pg/cloudnative-pg/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com"
```

### Attestations

Container images include the following attestations for transparency and
traceability:

- **[Software Bill of Materials
  (SBOM)](https://docs.docker.com/build/metadata/attestations/sbom/):** A
  comprehensive list of software artifacts included in the image or used during
  its build process, formatted using the
  [in-toto SPDX predicate standard](https://github.com/in-toto/attestation/blob/main/spec/predicates/spdx.md).
- **[Provenance](https://docs.docker.com/build/metadata/attestations/slsa-provenance/):**
  Metadata detailing how the image was built, following the [SLSA Provenance](https://slsa.dev)
  framework.

You can retrieve the SBOM for a specific image and platform using the following
command:

```shell
docker buildx imagetools inspect <IMAGE> \
  --format '{{ json (index .SBOM "<PLATFORM>").SPDX }}'
```

This command outputs the SBOM in JSON format, providing a detailed view of the
software components and build dependencies.

For the provenance, use:

```shell
docker buildx imagetools inspect <IMAGE> \
  --format '{{ json (index .Provenance "<PLATFORM>").SLSA }}'
```

### Guidelines and Frameworks for Container Security

The following guidelines and frameworks have been considered for ensuring
container-level security:

- **["Container Image Creation and Deployment Guide"](https://dl.dod.cyber.mil/wp-content/uploads/devsecops/pdf/DevSecOps_Enterprise_Container_Image_Creation_and_Deployment_Guide_2.6-Public-Release.pdf):**
  Developed by the Defense Information Systems Agency (DISA) of the United States
  Department of Defense (DoD).
- **["CIS Benchmark for Docker"](https://www.cisecurity.org/benchmark/docker/):**
  Developed by the Center for Internet Security (CIS).

:::note[About Container-Level Security]
    For more information on the approach that EDB has taken regarding security
    at the container level in CloudNativePG, please refer to the blog article
    ["Security and Containers in CloudNativePG"](https://www.enterprisedb.com/blog/security-and-containers-cloud-native-postgresql).
:::

## Cluster

Security at the cluster level takes into account all Kubernetes components that
form both the control plane and the nodes, as well as the applications that run in
the cluster (PostgreSQL included).

### Role Based Access Control (RBAC)

The operator interacts with the Kubernetes API server using a dedicated service
account named `cnpg-manager`. This service account is typically installed in
the operator namespace, commonly `cnpg-system`. However, the namespace may vary
based on the deployment method (see the subsection below).

In the same namespace, there is a binding between the `cnpg-manager` service
account and a role. The specific name and type of this role (either `Role` or
`ClusterRole`) also depend on the deployment method. This role defines the
necessary permissions required by the operator to function correctly. To learn
more about these roles, you can use the `kubectl describe clusterrole` or
`kubectl describe role` commands, depending on the deployment method.

:::info[Important]
    The above permissions are exclusively reserved for the operator's service
    account to interact with the Kubernetes API server.  They are not directly
    accessible by the users of the operator that interact only with `Cluster`,
    `Pooler`, `Backup`, `ScheduledBackup`, `Database`, `Publication`,
    `Subscription`, `ImageCatalog` and `ClusterImageCatalog` resources.
:::

Below we provide some examples and, most importantly, the reasons why
CloudNativePG requires full or partial management of standard Kubernetes
namespaced or non-namespaced resources.

`configmaps`
: The operator needs to create and manage default config maps for
  the Prometheus exporter monitoring metrics.

`deployments`
: The operator needs to manage a PgBouncer connection pooler
  using a standard Kubernetes `Deployment` resource.

`jobs`
: The operator needs to handle jobs to manage different `Cluster`'s phases.

`persistentvolumeclaims`
: The volume where the `PGDATA` resides is the
  central element of a PostgreSQL `Cluster` resource; the operator needs
  to interact with the selected storage class to dynamically provision
  the requested volumes, based on the defined scheduling policies.

`pods`
: The operator needs to manage `Cluster`'s instances.

`secrets`
: Unless you provide certificates and passwords to your `Cluster`
  objects, the operator adopts the "convention over configuration" paradigm by
  self-provisioning random generated passwords and TLS certificates, and by
  storing them in secrets.

`serviceaccounts`
: The operator needs to create a service account that
  enables the instance manager (which is the *PID 1* process of the container
  that controls the PostgreSQL server) to safely communicate with the
  Kubernetes API server to coordinate actions and continuously provide
  a reliable status of the `Cluster`.

`services`
: The operator needs to control network access to the PostgreSQL cluster
  (or the connection pooler) from applications, and properly manage
  failover/switchover operations in an automated way (by assigning, for example,
  the correct end-point of a service to the proper primary PostgreSQL instance).

`validatingwebhookconfigurations` and `mutatingwebhookconfigurations`
: The operator injects its self-signed webhook CA into both webhook
  configurations, which are needed to validate and mutate all the resources it
  manages. For more details, please see the
  [Kubernetes documentation](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/).

`volumesnapshots`
: The operator needs to generate `VolumeSnapshots` objects in order to take
  backups of a PostgreSQL server. VolumeSnapshots are read too in order to
  validate them before starting the restore process.

`nodes`
: The operator needs to get the labels for Affinity and AntiAffinity so it can
  decide in which nodes a pod can be scheduled. This is useful, for example, to
  prevent the replicas from being scheduled in the same node - especially
  important if nodes are in different availability zones. This
  permission is also used to determine whether a node is scheduled, preventing
  the creation of pods on unscheduled nodes, or triggering a switchover if
  the primary lives in an unscheduled node.


#### Deployments and `ClusterRole` Resources

As mentioned above, each deployment method may have variations in the namespace
location of the service account, as well as the names and types of role
bindings and respective roles.

##### Via Kubernetes Manifest

When installing CloudNativePG using the Kubernetes manifest, permissions are
set to `ClusterRoleBinding` by default. You can inspect the permissions
required by the operator by running:

```sh
kubectl describe clusterrole cnpg-manager
```

##### Via OLM

From a security perspective, the Operator Lifecycle Manager (OLM) provides a
more flexible deployment method. It allows you to configure the operator to
watch either all namespaces or specific namespaces, enabling more granular
permission management.

:::info
   OLM allows you to deploy the operator in its own namespace and configure it
   to watch specific namespaces used for CloudNativePG clusters. This setup helps
   to contain permissions and restrict access more effectively.
:::

#### Why Are ClusterRole Permissions Needed?

The operator currently requires `ClusterRole` permissions to read `nodes` and
`ClusterImageCatalog` objects. All other permissions can be namespace-scoped (i.e., `Role`) or
cluster-wide (i.e., `ClusterRole`).

Even with these permissions, if someone gains access to the `ServiceAccount`,
they will only have `get`, `list`, and `watch` permissions, which are limited
to viewing resources. However, if an unauthorized user gains access to the
`ServiceAccount`, it indicates a more significant security issue.

Therefore, it's crucial to prevent users from accessing the operator's
`ServiceAccount` and any other `ServiceAccount` with elevated permissions.

### Calls to the API server made by the instance manager

The instance manager, which is the entry point of the operand container, needs
to make some calls to the Kubernetes API server to ensure that the status of
some resources is correctly updated and to access the config maps and secrets
that are associated with that Postgres cluster. Such calls are performed through
a dedicated `ServiceAccount` created by the operator that shares the same
PostgreSQL `Cluster` resource name.

:::info[Important]
    The operand can only access a specific and limited subset of resources
    through the API server. A service account is the
    [recommended way to access the API server from within a Pod](https://kubernetes.io/docs/tasks/run-application/access-api-from-pod/).
:::

#### Using a shared ServiceAccount

By default, CloudNativePG creates a dedicated `ServiceAccount` for each cluster,
named after the cluster itself. However, in cloud environments using IAM roles
(such as AWS IRSA, GCP Workload Identity, or Azure Workload Identity), each
cluster creating its own `ServiceAccount` requires individual IAM configuration
for every new cluster, making it difficult to scale when managing multiple
clusters.

CloudNativePG allows multiple clusters to share a single `ServiceAccount` by
specifying the `serviceAccountName` field in the cluster specification. This
enables one-time IAM configuration that works across all clusters using that
`ServiceAccount`.

!!! Important
    When using a shared `ServiceAccount`, you are responsible for creating and
    managing the `ServiceAccount` yourself. The operator will validate that the
    specified `ServiceAccount` exists but will not create or modify it.

Here's an example of using a shared `ServiceAccount`:

```yaml
# Create the shared ServiceAccount once
apiVersion: v1
kind: ServiceAccount
metadata:
  name: postgres-cloud-sa
  annotations:
    # AWS IRSA annotation
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/PostgresRole
---
# Reference it from multiple clusters
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-prod
spec:
  serviceAccountName: postgres-cloud-sa
  instances: 3
  storage:
    size: 100Gi
```

!!! Note
    The `serviceAccountName` field is mutually exclusive with
    `serviceAccountTemplate`. You can either let the operator manage the
    `ServiceAccount` (optionally customizing it with `serviceAccountTemplate`)
    or reference an existing one with `serviceAccountName`, but not both.

!!! Warning
    The `serviceAccountName` field is immutable once set. If you need to change
    the `ServiceAccount`, you must recreate the cluster.

For transparency, the permissions associated with the service account are defined in the
[roles.go](https://github.com/cloudnative-pg/cloudnative-pg/blob/main/pkg/specs/roles.go)
file. For example, to retrieve the permissions of a generic `mypg` cluster in the
`myns` namespace, you can type the following command:

```bash
kubectl get role -n myns mypg -o yaml
```

Then verify that the role is bound to the service account:

```bash
kubectl get rolebinding -n myns mypg -o yaml
```

:::info[Important]
    Remember that **roles are limited to a given namespace**.
:::

Below we provide a quick summary of the permissions associated with the service
account for generic Kubernetes resources.

`configmaps`
: The instance manager can only read config maps that are related to the same
  cluster, such as custom monitoring queries

`secrets`
: The instance manager can only read secrets that are related to the same
  cluster, namely: streaming replication user, application user, super user,
  LDAP authentication user, client CA, server CA, server certificate, backup
  credentials, custom monitoring queries

`events`
: The instance manager can create an event for the cluster, informing the
  API server about a particular aspect of the PostgreSQL instance lifecycle

Here instead, we provide the same summary for resources specific to
CloudNativePG.

`clusters`
: The instance manager requires read-only permissions, namely `get`, `list` and
  `watch`, just for its own `Cluster` resource

`clusters/status`
: The instance manager requires to `update` and `patch` the status of just its
  own `Cluster` resource

`backups`
: The instance manager requires `get` and `list` permissions to read any
  `Backup` resource in the namespace. Additionally, it requires the `delete`
  permission to clean up the Kubernetes cluster by removing the `Backup` objects
  that do not have a counterpart in the object store - typically because of
  retention policies

`backups/status`
: The instance manager requires to `update` and `patch` the status of any
  `Backup` resource in the namespace

### Pod and Container Security Contexts

A [Security Context](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/)
defines privilege and access control settings for a pod or container.

CloudNativePG does not require *privileged* mode for container execution.
The PostgreSQL containers run as the `postgres` system user. No component
whatsoever requires running as `root`.

Likewise, Volume access does not require *privileged* mode nor `root`
privileges. Proper permissions must be assigned by the Kubernetes platform
and/or administrators. The PostgreSQL containers run with a read-only root
filesystem (i.e. no writable layer).

The operator manages the setting of security contexts for all pods and 
containers of a PostgreSQL cluster. The [Seccomp Profile](https://kubernetes.io/docs/tutorials/security/seccomp/) 
to be used for the PostgreSQL containers can be configured with the 
`spec.seccompProfile` section of the `Cluster` resource. If this section is left
blank, the containers will use a seccompProfile `Type` of `RuntimeDefault`, that
is, the container runtime default.

The security context of PostgreSQL containers using the default `seccompProfile`
will look like this:

```
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
    - ALL
  privileged: false
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault
```

#### Customizing Security Contexts

CloudNativePG provides fine-grained control over security contexts for both
pods and containers through the `spec.podSecurityContext` and
`spec.securityContext` fields respectively.

:::info[Important]
    Changing security contexts can significantly affect the security posture
    of your PostgreSQL clusters and may prevent pods from starting or
    operating correctly. Before making changes, review which fields you will
    override and how they merge with the operator defaults, test changes
    in a non-production environment, and apply the minimal, well-documented
    modifications necessary.
:::

**Pod Security Context** (`spec.podSecurityContext`):
This allows you to override the default `PodSecurityContext` applied to all
PostgreSQL cluster pods. When specified, it will merge with the operator's
default settings, with your values taking precedence for explicitly set fields.

Example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3
  podSecurityContext:
    runAsUser: 26
    runAsGroup: 26
    fsGroup: 26
    supplementalGroups: [2000, 3000]
    fsGroupChangePolicy: "OnRootMismatch"
```

**Container Security Context** (`spec.securityContext`):
This allows you to override the default `SecurityContext` applied to all
containers within the PostgreSQL cluster pods. Like `podSecurityContext`, it
merges with the operator's defaults.

Example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3
  securityContext:
    allowPrivilegeEscalation: false
    # Note: capabilities are not merged with operator defaults.
    # If specified, they fully replace any defaults.
    capabilities:
      drop:
      - ALL
      add:
      - NET_BIND_SERVICE
    readOnlyRootFilesystem: true
    runAsNonRoot: true
```

:::info[Important]
    For any fields you don't explicitly set, the operator will apply its
    secure defaults. This ensures that even partial configurations maintain
    security best practices.
:::

:::note
    These fields are particularly useful when working with the
    [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/)
    `restricted` profile, which has strict requirements for pod and container
    security contexts.
:::

#### Security Context Constraints

When running in an environment that is utilizing
[Security Context Constraints (SCC)](https://docs.openshift.com/container-platform/4.17/authentication/managing-security-context-constraints.html)
the operator does not explicitly set the security context of the PostgreSQL
cluster pods, but rather allows the pods to inherit the restricted Security
Context Constraints that are already defined.

### Restricting Pod access using AppArmor

You can assign an
[AppArmor](https://kubernetes.io/docs/tutorials/security/apparmor/) profile to
the `postgres`, `initdb`, `join`, `full-recovery` and `bootstrap-controller` containers inside every `Cluster` pod through the
`container.apparmor.security.beta.kubernetes.io` annotation.
For example:

```yaml
kind: Cluster
metadata:
  name: cluster-apparmor
  annotations:
    container.apparmor.security.beta.kubernetes.io/postgres: runtime/default
    container.apparmor.security.beta.kubernetes.io/initdb: runtime/default
    container.apparmor.security.beta.kubernetes.io/join: runtime/default
```

:::warning
    Using this kind of annotations can result in your cluster to stop working.
    If this is the case, the annotation can be safely removed from the `Cluster`.
:::

The AppArmor configuration must be at Kubernetes node level, meaning that the
underlying operating system must have this option enable and properly
configured.

In case this is not the situation, and the annotations were added at the
`Cluster` creation time, pods will not be created. On the other hand, if you
add the annotations after the `Cluster` was created the pods in the cluster will
be unable to start and you will get an error like this:

```
metadata.annotations[container.apparmor.security.beta.kubernetes.io/postgres]: Forbidden: may not add AppArmor annotations]
```

In such cases, please refer to your Kubernetes administrators and ask for the
proper AppArmor profile to use.

### Network Policies

The pods created by the `Cluster` resource can be controlled by Kubernetes
[network policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
to enable/disable inbound and outbound network access at IP and TCP level.
You can find more information in the [networking document](networking.md).

:::info[Important]
    The operator needs to communicate to each instance on TCP port 8000
    to get information about the status of the PostgreSQL server. Please
    make sure you keep this in mind in case you add any network policy,
    and refer to the "Exposed Ports" section below for a list of ports used by
    CloudNativePG for finer control.
:::

Network policies are beyond the scope of this document.
Please refer to the ["Network policies"](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
section of the Kubernetes documentation for further information.

#### Exposed Ports

CloudNativePG exposes ports at operator, instance manager and operand
levels, as listed in the table below:

| System           | Port number | Exposing            | Name             | TLS      | Authentication |
|:-----------------|:------------|:--------------------|:-----------------|:---------|:---------------|
| operator         | 9443        | webhook server      | `webhook-server` | Yes      | Yes            |
| operator         | 8080        | metrics             | `metrics`        | No       | No             |
| instance manager | 9187        | metrics             | `metrics`        | Optional | No             |
| instance manager | 8000        | status              | `status`         | Yes      | No             |
| operand          | 5432        | PostgreSQL instance | `postgresql`     | Optional | Yes            |

### PostgreSQL

The current implementation of CloudNativePG automatically creates
passwords and `.pgpass` files for the database owner and, only
if requested by setting `enableSuperuserAccess` to `true`, for the
`postgres` superuser.

:::warning
    `enableSuperuserAccess` is set to `false` by default to improve the
    security-by-default posture of the operator, fostering a microservice approach
    where changes to PostgreSQL are performed in a declarative way through the
    `spec` of the `Cluster` resource, while providing developers with full powers
    inside the database through the database owner user.
:::

As far as password encryption is concerned, CloudNativePG follows
the default behavior of PostgreSQL: starting from PostgreSQL 14,
`password_encryption` is by default set to `scram-sha-256`, while on earlier
versions it is set to `md5`.

:::info[Important]
    Please refer to the ["Password authentication"](https://www.postgresql.org/docs/current/auth-password.html)
    section in the PostgreSQL documentation for details.
:::

:::note
    The operator supports toggling the `enableSuperuserAccess` option. When you
    disable it on a running cluster, the operator will ignore the content of the secret,
    remove it (if previously generated by the operator) and set the password of the
    `postgres` user to `NULL` (de facto disabling remote access through password authentication).
:::

See the ["Secrets" section in the "Connecting from an application" page](applications.md#secrets) for more information.

You can use those files to configure application access to the database.

By default, every replica is automatically configured to connect in **physical
async streaming replication** with the current primary instance, with a special
user called `streaming_replica`. The connection between nodes is **encrypted**
and authentication is via **TLS client certificates** (please refer to the
["Client TLS/SSL Connections"](ssl_connections.md#"Client TLS/SSL Connections") page
for details). By default, the operator requires TLS v1.3 connections.

Currently, the operator allows administrators to add `pg_hba.conf` lines directly in the manifest
as part of the `pg_hba` section of the `postgresql` configuration. The lines defined in the
manifest are added to a default `pg_hba.conf`.

For further detail on how `pg_hba.conf` is managed by the operator, see the
["PostgreSQL Configuration" page](postgresql_conf.md#the-pg_hba-section) of the documentation.

The administrator can also customize the content of the `pg_ident.conf` file that by default
only maps the local postgres user to the postgres user in the database.

For further detail on how `pg_ident.conf` is managed by the operator, see the
["PostgreSQL Configuration" page](postgresql_conf.md#the-pg_ident-section) of the documentation.

:::info[Important]
    Examples assume that the Kubernetes cluster runs in a private and secure network.
:::

### Storage

CloudNativePG delegates encryption at rest to the underlying storage class. For
data protection in production environments, we highly recommend that you choose
a storage class that supports encryption at rest.
