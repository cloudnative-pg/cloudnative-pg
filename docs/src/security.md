# Security

This section contains information about security for Cloud Native PostgreSQL,
that are analyzed at 3 different layers: Code, Container and Cluster.

!!! Warning
    The information contained in this page must not exonerate you from
    performing regular InfoSec duties on your Kubernetes cluster. Please
    familiarize yourself with the ["Overview of Cloud Native Security"](https://kubernetes.io/docs/concepts/security/overview/)
    page from the Kubernetes documentation.

!!! Seealso "About the 4C's Security Model"
    Please refer to ["The 4C’s Security Model in Kubernetes"](https://www.enterprisedb.com/blog/4cs-security-model-kubernetes)
    blog article to get a better understanding and context of the approach EDB
    has taken with security in Cloud Native PostgreSQL.

## Code

Source code of Cloud Native PostgreSQL is *systematically scanned* for static analysis purposes,
including **security problems**, using a popular open-source linter for Go called
[GolangCI-Lint](https://github.com/golangci/golangci-lint) directly in the CI/CD pipeline.
GolangCI-Lint can run several *linters* on the same source code.

One of these is [Golang Security Checker](https://github.com/securego/gosec), or simply `gosec`,
a linter that scans the abstract syntactic tree of the source against a set of rules aimed at
the discovery of well-known vulnerabilities, threats, and weaknesses hidden in
the code such as hard-coded credentials, integer overflows and SQL injections - to name a few.

!!! Important
    A failure in the static code analysis phase of the CI/CD pipeline is a blocker
    for the entire delivery of Cloud Native PostgreSQL, meaning that each commit is validated
    against all the linters defined by GolangCI-Lint.

## Container

Every container image that is part of Cloud Native PostgreSQL is automatically built via CI/CD pipelines following every commit.
Such images include not only the operator's, but also the operands' - specifically every supported PostgreSQL version.
Within the pipelines, images are scanned with:

- [Dockle](https://github.com/goodwithtech/dockle): for best practices in terms
  of the container build process
- [Clair](https://github.com/quay/clair): for vulnerabilities found in both the
  underlying operating system as well as libraries and applications that they run

!!! Important
    All operand images are automatically rebuilt once a day by our pipelines in case
    of security updates at the base image and package level, providing **patch level updates**
    for the container images that EDB distributes.

The following guidelines and frameworks have been taken into account for container-level security:

- the ["Container Image Creation and Deployment Guide"](https://dl.dod.cyber.mil/wp-content/uploads/devsecops/pdf/DevSecOps_Enterprise_Container_Image_Creation_and_Deployment_Guide_2.6-Public-Release.pdf),
  developed by the Defense Information Systems Agency (DISA) of the United States Department of Defense (DoD)
- the ["CIS Benchmark for Docker"](https://www.cisecurity.org/benchmark/docker/),
  developed by the Center for Internet Security (CIS)

!!! Seealso "About the Container level security"
    Please refer to ["Security and Containers in Cloud Native PostgreSQL"](https://www.enterprisedb.com/blog/security-and-containers-cloud-native-postgresql)
    blog article for more information about the approach that EDB has taken on
    security at the container level in Cloud Native PostgreSQL.

## Cluster

Security at the cluster level takes into account all Kubernetes components that
form both the control plane and the nodes, as well as the applications that run in
the cluster (PostgreSQL included).

### Pod Security Policies

A [Pod Security Policy](https://kubernetes.io/docs/concepts/policy/pod-security-policy/)
is the Kubernetes way to define security rules and specifications that a pod needs to meet
to run in a cluster.
For InfoSec reasons, every Kubernetes platform should implement them.

Cloud Native PostgreSQL does not require *privileged* mode for containers execution.
The PostgreSQL containers run as `postgres` system user. No component whatsoever requires running as `root`.

Likewise, Volumes access does not require *privileges* mode or `root` privileges either.
Proper permissions must be properly assigned by the Kubernetes platform and/or administrators.
The PostgreSQL containers run with a read-only root filesystem (i.e. no writable layer).

The operator explicitly sets the required security contexts.

On RedHat OpenShift, Cloud Native PostgreSQL runs in `restricted` security context constraint,
the most restrictive one. The goal is to limit the execution of a pod to a namespace allocated UID
and SELinux context.

!!! Seealso "Security Context Constraints in OpenShift"
    For further information on Security Context Constraints (SCC) in
    OpenShift, please refer to the
    ["Managing SCC in OpenShift"](https://www.openshift.com/blog/managing-sccs-in-openshift)
    article.

!!! Warning "Security Context Constraints and namespaces"
    As stated by [Openshift documentation](https://docs.openshift.com/container-platform/latest/authentication/managing-security-context-constraints.html#role-based-access-to-ssc_configuring-internal-oauth)
    SCCs are not applied in the default namespaces (`default`, `kube-system`,
    `kube-public`, `openshift-node`, `openshift-infra`, `openshift`) and those
    should not be used to run pods. CNP clusters deployed in those namespaces
    will be unable to start due to missing SCCs.

### Restricting Pod access using AppArmor

You can assign an
[AppArmor](https://kubernetes.io/docs/tutorials/security/apparmor/) profile to
the `postgres`, `initdb`, `join`, `full-recovery` and `bootstrap-controller` containers inside every `Cluster` pod through the
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
    Using this kind of annotations can result in your cluster to stop working.
    If this is the case, the annotation can be safely removed from the `Cluster`.

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

In such cases, please refer to your Kubernetes administrators and ask for the proper AppArmor profile to use.

!!! Warning "AppArmor and OpenShift"
    AppArmor is currently available only on Debian distributions like Ubuntu,
    hence this is not (and will not be) available in OpenShift

### Network Policies

The pods created by the `Cluster` resource can be controlled by Kubernetes
[network policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
to enable/disable inbound and outbound network access at IP and TCP level.

!!! Important
    The operator needs to communicate to each instance on TCP port 8000
    to get information about the status of the PostgreSQL server. Please
    make sure you keep this in mind in case you add any network policy,
    and refer to the "Exposed Ports" section below for a list of ports used by
    Cloud Native PostgreSQL for finer control.

Network policies are beyond the scope of this document.
Please refer to the ["Network policies"](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
section of the Kubernetes documentation for further information.

#### Exposed Ports

Cloud Native PostgreSQL exposes ports at operator, instance manager and operand
levels, as listed in the table below:

System           | Port number  | Exposing            |  Name               |  Certificates  |  Authentication
:--------------- | :----------- | :------------------ | :------------------ | :------------  | :--------------
operator         | 9443         | webhook server      | `webhook-server`    |  TLS           | Yes
operator         | 8080         | metrics             | `metrics`           |  no TLS        | No
instance manager | 9187         | metrics             | `metrics`           |  no TLS        | No
instance manager | 8000         | status              | `status`            |  no TLS        | No
operand          | 5432         | PostgreSQL instance | `postgresql`        |  optional TLS  | Yes

### PostgreSQL

The current implementation of Cloud Native PostgreSQL automatically creates
passwords and `.pgpass` files for the `postgres` superuser and the database owner.

As far as encryption of password is concerned, Cloud Native PostgreSQL follows
the default behavior of PostgreSQL: starting from PostgreSQL 14,
`password_encryption` is by default set to `scram-sha-256`, while on earlier
versions it is set to `md5`.

!!! Important
    Please refer to the ["Password authentication"](https://www.postgresql.org/docs/current/auth-password.html)
    section in the PostgreSQL documentation for details.

You can disable management of the `postgres` user password via secrets by setting
`enableSuperuserAccess` to `false`.

!!! Note
    The operator supports toggling the `enableSuperuserAccess` option. When you
    disable it on a running cluster, the operator will ignore the content of the secret,
    remove it (if previously generated by the operator) and set the password of the
    `postgres` user to `NULL` (de facto disabling remote access through password authentication).

See the ["Secrets" section in the "Architecture" page](architecture.md#secrets) for more information.

You can use those files to configure application access to the database.

By default, every replica is automatically configured to connect in **physical
async streaming replication** with the current primary instance, with a special
user called `streaming_replica`. The connection between nodes is **encrypted**
and authentication is via **TLS client certificates** (please refer to the
["Client TLS/SSL Connections"](ssl_connections.md#"Client TLS/SSL Connections") page
for details).

Currently, the operator allows administrators to add `pg_hba.conf` lines directly in the manifest
as part of the `pg_hba` section of the `postgresql` configuration. The lines defined in the
manifest are added to a default `pg_hba.conf`.

For further detail on how `pg_hba.conf` is managed by the operator, see the
["PostgreSQL Configuration" page](postgresql_conf.md#the-pg_hba-section) of the documentation.

!!! Important
    Examples assume that the Kubernetes cluster runs in a private and secure network.
