# Security

This section contains information about security for Cloud Native PostgreSQL
analyzed at 3 different layers: Code, Container and Cluster.

!!! Warning
    The information contained in this page must not exonerate you from
    performing regular InfoSec duties on your Kubernetes cluster. Please
    familiarize with the ["Overview of Cloud Native Security"](https://kubernetes.io/docs/concepts/security/overview/)
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

Source code is also regularly inspected through [Coverity Scan by Synopsys](https://scan.coverity.com/)
via EnterpriseDB's internal CI/CD pipeline.

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
    security at container level in Cloud Native PostgreSQL.

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
PostgreSQL servers run as `postgres` system user. No component whatsoever requires to run as `root`.

Likewise, Volumes access does not require *privileges* mode or `root` privileges either.
Proper permissions must be properly assigned by the Kubernetes platform and/or administrators.

### Network Policies

The pods created by the `Cluster` resource can be controlled by Kubernetes
[network policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
to enable/disable inbound and outbound network access at IP and TCP level.

!!! Important
    The operator needs to communicate to each instance on TCP port 8000
    to get information about the status of the PostgreSQL server. Make sure
    you keep this in mind in case you add any network policy.

Network policies are beyond the scope of this document.
Please refer to the ["Network policies"](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
section of the Kubernetes documentation for further information.

### PostgreSQL

The current implementation of Cloud Native PostgreSQL automatically creates
passwords and `.pgpass` files for the `postgres` superuser and the database owner.
See the ["Secrets" section in the "Architecture" page](architecture.md#secrets).

You can use those files to configure application access to the database.

By default, every replica is automatically configured to connect in **physical
async streaming replication** with the current primary instance, with a special
user called `streaming_replica`. The connection between nodes is **encrypted**
and authentication is via **TLS client certificates** (please refer to the
["Client TLS/SSL Connections"](ssl_connections.md#Client TLS/SSL Connections) page
for details).

Currently, the operator allows administrators to add `pg_hba.conf` lines directly in the manifest
as part of the `pg_hba` section of the `postgresql` configuration. The lines defined in the
manifest are added to a default `pg_hba.conf`.

For further detail on how `pg_hba.conf` is managed by the operator, see the
["PostgreSQL Configuration" page](postgresql_conf.md#the-pg_hba-section) of the documentation.

!!! Important
    Examples assume that the Kubernetes cluster runs in a private and secure network.
