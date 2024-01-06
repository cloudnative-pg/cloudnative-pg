# Security

Security for CloudNativePG
is analyzed at three different layers: code, container, and cluster.

!!! Warning
    In addition to the security tasks described here, you must
    perform regular InfoSec duties on your Kubernetes cluster. 
    Familiarize yourself with [Overview of Cloud Native Security](https://kubernetes.io/docs/concepts/security/overview/)
    in the Kubernetes documentation.

!!! Seealso "About the 4C's Security Model"
    See [The 4C’s Security Model in Kubernetes](https://www.enterprisedb.com/blog/4cs-security-model-kubernetes)
    blog article to get a better understanding and context of the approach EDB
    took with security in CloudNativePG.

## Code

Source code of CloudNativePG is systematically scanned for static analysis purposes,
including security, using a popular open-source *linter* for Go called
[GolangCI-Lint](https://github.com/golangci/golangci-lint). You can run this linter directly in the CI/CD pipeline.
GolangCI-Lint can run several linters on the same source code.

One of these is [Golang Security Checker](https://github.com/securego/gosec), or gosec,
a linter that scans the abstract syntactic tree of the source against a set of rules aimed at
discovering well-known vulnerabilities, threats, and weaknesses. These vulnerabilities include those hidden in
the code, such as hard-coded credentials, integer overflows, and SQL injections, to name a few.

!!! Important
    A failure in the static-code-analysis phase of the CI/CD pipeline is a blocker
    for the entire delivery of CloudNativePG. This means that each commit is validated
    against all the linters defined by GolangCI-Lint.

## Container

Every container image that's part of CloudNativePG is built by way of CI/CD pipelines following every commit.
Such images include not only the operator's but also the operands', specifically every supported PostgreSQL version.
In the pipelines, images are scanned with [Dockle](https://github.com/goodwithtech/dockle), for best practices in terms
of the container build process.

!!! Important
    All operand images are rebuilt once a day by our pipelines in case
    of security updates at the base image and package level. This approach provides *patch-level updates*
    for the container images that EDB distributes.

The following guidelines and frameworks were taken into account for container-level security:

- The [Container Image Creation and Deployment Guide](https://dl.dod.cyber.mil/wp-content/uploads/devsecops/pdf/DevSecOps_Enterprise_Container_Image_Creation_and_Deployment_Guide_2.6-Public-Release.pdf),
  developed by the Defense Information Systems Agency (DISA) of the United States Department of Defense (DoD)
- The [CIS Benchmark for Docker](https://www.cisecurity.org/benchmark/docker/),
  developed by the Center for Internet Security (CIS)

!!! Seealso "About the container-level security"
    See the [Security and Containers in CloudNativePG](https://www.enterprisedb.com/blog/security-and-containers-cloud-native-postgresql)
    blog article for more information about the approach that EDB takes on
    security at the container level in CloudNativePG.

## Cluster

Security at the cluster level takes into account all Kubernetes components that
form both the control plane and the nodes, as well as the applications that run in
the cluster, including PostgreSQL.

### Role based access control (RBAC)

The operator interacts with the Kubernetes API server with a dedicated service
account called `cnpg-manager`. In Kubernetes, this account is installed
by default in the `cnpg-system` namespace. A cluster role
binds between this service account and the `cnpg-manager`
cluster role and defines the set of rules/resources/verbs granted to the operator.

!!! Important
    These permissions are exclusively reserved for the operator's service
    account to interact with the Kubernetes API server. They're not directly
    accessible by the users of the operator that interact only with `Cluster`,
    `Pooler`, `Backup`, and `ScheduledBackup` resources.

The following are some examples and, most importantly, the reasons why
CloudNativePG requires full or partial management of standard Kubernetes
namespaced resources.

`configmaps`
: The operator needs to create and manage default config maps for
  the Prometheus exporter monitoring metrics.

`deployments`
: The operator needs to manage a PgBouncer connection pooler
  using a standard Kubernetes `Deployment` resource.

`jobs`
: The operator needs to handle jobs to manage different `Cluster` phases.

`persistentvolumeclaims`
: The volume where the `PGDATA` resides is the
  central element of a PostgreSQL `Cluster` resource. The operator needs
  to interact with the selected storage class to dynamically provision
  the requested volumes, based on the defined scheduling policies.

`pods`
: The operator needs to manage `Cluster` instances.

`secrets`
: Unless you provide certificates and passwords to your `Cluster`
  objects, the operator adopts the convention-over-configuration paradigm by
  self-provisioning random-generated passwords and TLS certificates, and by
  storing them in secrets.

`serviceaccounts`
: The operator needs to create a service account that
  enables the instance manager, which is the *PID 1* process of the container
  that controls the PostgreSQL server. This is needed to safely communicate with the
  Kubernetes API server to coordinate actions and continuously provide
  a reliable status of the `Cluster`.

`services`
: The operator needs to control network access to the PostgreSQL cluster
  or the connection pooler from applications. It also needs to properly manage
  failover/switchover operations in an automated way by assigning, for example,
  the correct endpoint of a service to the proper primary PostgreSQL instance.

`validatingwebhookconfigurations` and `mutatingwebhookconfigurations`
: The operator injects its self-signed webhook CA into both webhook
  configurations, which are needed to validate and mutate all the resources it
  manages. For more details, see the
  [Kubernetes documentation](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/).

`volumesnapshots`
: The operator needs to generate `VolumeSnapshots` objects to take
  backups of a PostgreSQL server. `VolumeSnapshots` are also read, to
  validate them before starting the restore process.

`nodes`
: The operator needs to get the labels for `Affinity` and `AntiAffinity` so it can
  decide in which nodes a pod can be scheduled. This behavior prevents the replicas from being
  in the same node, especially if nodes are in different availability zones. This
  permission is also used to determine if a node is scheduled, avoiding
  the creation of pods that can't be created at all.

To see all the permissions required by the operator, you can run `kubectl
describe clusterrole cnpg-manager`.

### Calls to the API server made by the instance manager

The instance manager, which is the entry point of the operand container, needs
to make some calls to the Kubernetes API server. These calls ensure that the status of
some resources is correctly updated and access the config maps and secrets
that are associated with that Postgres cluster. Such calls are performed through
a dedicated `ServiceAccount` created by the operator that shares the same
PostgreSQL `Cluster` resource name.

!!! Important
    The operand can access only a specific and limited subset of resources
    through the API server. A service account is the
    [recommended way to access the API server from a pod](https://kubernetes.io/docs/tasks/run-application/access-api-from-pod/).

For transparency, the permissions associated with the service account are defined in the
[roles.go](https://github.com/cloudnative-pg/cloudnative-pg/blob/main/pkg/specs/roles.go)
file. For example, to retrieve the permissions of a generic `mypg` cluster in the
`myns` namespace, you can use the following command:

```bash
kubectl get role -n myns mypg -o yaml
```

Then verify that the role is bound to the service account:

```bash
kubectl get rolebinding -n myns mypg -o yaml
```

!!! Important
    Remember that roles are limited to a given namespace.

The following is a quick summary of the permissions associated with the service
account for generic Kubernetes resources.

`configmaps`
: The instance manager can only read config maps that are related to the same
  cluster, such as custom monitoring queries.

`secrets`
: The instance manager can only read secrets that are related to the same
  cluster, namely: streaming replication user, application user, super user,
  LDAP authentication user, client CA, server CA, server certificate, backup
  credentials, and custom monitoring queries.

`events`
: The instance manager can create an event for the cluster, informing the
  API server about a particular aspect of the PostgreSQL instance lifecycle.

Here, instead, is the same summary for resources specific to
CloudNativePG.

`clusters`
: The instance manager requires read-only permissions, namely `get`, `list`, and
  `watch`, for its own `Cluster` resource.

`clusters/status`
: The instance manager requires to `update` and `patch` the status of its
  own `Cluster` resource.

`backups`
: The instance manager requires `get` and `list` permissions to read any
  `Backup` resource in the namespace. Additionally, it requires the `delete`
  permission to clean up the Kubernetes cluster by removing the `Backup` objects
  that don't have a counterpart in the object store, typically because of
  retention policies.

`backups/status`
: The instance manager requires to `update` and `patch` the status of any
  `Backup` resource in the namespace.

### Pod security policies

A [pod security policy](https://kubernetes.io/docs/concepts/policy/pod-security-policy/)
is the Kubernetes way to define security rules and specifications that a pod needs to meet
to run in a cluster.
For InfoSec reasons, every Kubernetes platform must implement them.

CloudNativePG doesn't require privileged mode for containers execution.
The PostgreSQL containers run as the postgres system user. No component requires running as root.

Likewise, volumes access doesn't require privileged mode or root privileges.
Proper permissions must be assigned by the Kubernetes platform or administrators.
The PostgreSQL containers run with a read-only root filesystem, that is, no writable layer.

The operator explicitly sets the required security contexts.

### Restricting pod access using AppArmor

You can assign an
[AppArmor](https://kubernetes.io/docs/tutorials/security/apparmor/) profile to
the `postgres`, `initdb`, `join`, `full-recovery`, and `bootstrap-controller` containers inside every `Cluster` pod using the
`container.apparmor.security.beta.kubernetes.io` annotation.

!!! Seealso "Example of cluster annotations"
```
	kind: Cluster
	metadata:
		name: cluster-apparmor
		annotations:
			container.apparmor.security.beta.kubernetes.io/postgres: runtime/default
			container.apparmor.security.beta.kubernetes.io/initdb: runtime/default
			container.apparmor.security.beta.kubernetes.io/join: runtime/default
```

!!! Warning
    Using this kind of annotations can cause your cluster to stop working.
    If this happens, you can safely remove the annotation from the cluster.

The AppArmor configuration must be at Kubernetes node level, meaning that the
underlying operating system must have this option enabled and properly
configured. If not, and the annotations were added at the
`Cluster` creation time, pods aren't created. On the other hand, if you
add the annotations after the `Cluster` was created, the pods in the cluster can't
start, and you see an error like this:

```
metadata.annotations[container.apparmor.security.beta.kubernetes.io/postgres]: Forbidden: may not add AppArmor annotations]
```

In such cases, contact your Kubernetes administrators and ask for the
correct AppArmor profile to use.

### Network policies

The pods created by the `Cluster` resource can be controlled by Kubernetes
[network policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
to enable or disable inbound and outbound network access at IP and TCP level.
For more information, see [Networking](networking.md).

!!! Important
    The operator needs to communicate to each instance on TCP port 8000
    to get information about the status of the PostgreSQL server. 
    Make sure you keep this in mind if you add any network policy,
    and refer to [Exposed ports](#exposed-ports) for a list of ports used by
    CloudNativePG for finer control.

Network policies are beyond the scope of this documentation.
See [Network policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
in the Kubernetes documentation for more information.

#### Exposed ports

CloudNativePG exposes ports at operator, instance manager, and operand
levels, as shown in the table.

System           | Port number  | Exposing            |  Name               |  Certificates  |  Authentication
:--------------- | :----------- | :------------------ | :------------------ | :------------  | :--------------
operator         | 9443         | webhook server      | `webhook-server`    |  TLS           | Yes
operator         | 8080         | metrics             | `metrics`           |  no TLS        | No
instance manager | 9187         | metrics             | `metrics`           |  no TLS        | No
instance manager | 8000         | status              | `status`            |  no TLS        | No
operand          | 5432         | PostgreSQL instance | `postgresql`        |  optional TLS  | Yes

### PostgreSQL

The current implementation of CloudNativePG creates
passwords and `.pgpass` files for the the database owner and,
if requested by setting `enableSuperuserAccess` to `true`, for the
postgres superuser.

!!! Warning
    Prior to CloudNativePG 1.21, `enableSuperuserAccess` was set to `true` by
    default. This change was implemented to improve the security-by-default
    posture of the operator. This approach fosters a microservice approach where changes to
    PostgreSQL are performed in a declarative way through the `spec` of the
    `Cluster` resource. At the same time, it provides developers with full powers inside the
    database through the database owner user.

As far as password encryption is concerned, CloudNativePG follows
the default behavior of PostgreSQL: starting with PostgreSQL 14,
`password_encryption` is by default set to `scram-sha-256`. In earlier
versions, it's set to `md5`.

!!! Important
    See [Password authentication](https://www.postgresql.org/docs/current/auth-password.html)
    in the PostgreSQL documentation for details.

!!! Note
    The operator supports toggling the `enableSuperuserAccess` option. When you
    disable it on a running cluster, the operator ignores the content of the secret,
    removes it (if previously generated by the operator), and sets the password of the
    `postgres` user to `NULL`. This in effect disables remote access through password authentication.

See [Secrets](applications.md#secrets) for more information.

You can use those files to configure application access to the database.

By default, every replica is configured to connect in *physical
async streaming replication* with the current primary instance, with a special
user called streaming_replica. The connection between nodes is encrypted,
and authentication is by way of TLS client certificates. (See
[Client TLS/SSL connections](ssl_connections.md#"Client TLS/SSL connections") 
for details.) By default, the operator requires TLS v1.3 connections.

Currently, the operator allows administrators to add `pg_hba.conf` lines directly in the manifest
as part of the `pg_hba` section of the `postgresql` configuration. The lines defined in the
manifest are added to a default `pg_hba.conf`.

For details on how `pg_hba.conf` is managed by the operator, see
[PostgreSQL configuration](postgresql_conf.md#the-pg_hba-section).

!!! Important
    Examples assume that the Kubernetes cluster runs in a private and secure network.

### Storage

CloudNativePG delegates encryption at rest to the underlying storage class. For
data protection in production environments, we strongly recommend that you choose
a storage class that supports encryption at rest.
