# Security

This section contains information about security for Cloud Native PostgreSQL,
from a few standpoints: source code, Kubernetes, and PostgreSQL.

!!! Warning
    The information contained in this page must not exonerate you from
    performing regular InfoSec duties on your Kubernetes cluster.

## Source code static analysis

Source code of Cloud Native PostgreSQL is *systematically scanned* for static analysis purposes,
including **security problems**, using a popular open-source for Go called
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

## Kubernetes

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

Network policies are beyond the scope of this document.
Please refer to the ["Network policies"](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
section of the Kubernetes documentation for further information.

### Resources

In a typical Kubernetes cluster, containers run with unlimited resources. By default,
they might be allowed to use as much CPU and RAM as needed.

Cloud Native PostgreSQL allows administrators to control and manage resource usage by the pods of the cluster,
through the `resources` section of the manifest, with two knobs:

- `requests`: initial requirement
- `limits`: maximum usage, in case of dynamic increase of resource needs

For example, you can request an initial amount of RAM of 32MiB (scalable to 128MiB) and 50m of CPU (scalable to 100m) as follows:

```yaml
  resources:
    requests:
      memory: "32Mi"
      cpu: "50m"
    limits:
      memory: "128Mi"
      cpu: "100m"
```

[//]: # ( TODO: we may want to explain what happens to a pod that exceedes the resource limits: CPU -> trottle; MEMORY -> kill )

!!! Seealso "Managing Compute Resources for Containers"
    For more details on resource management, please refer to the
    ["Managing Compute Resources for Containers"](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/)
    page from the Kubernetes documentation.

## PostgreSQL

The current implementation of Cloud Native PostgreSQL automatically creates
passwords and `.pgpass` files for the `postgres` superuser and the database owner.
See the ["Secrets" section in the "Architecture" page](architecture.md#secrets).

You can use those files to configure application access to the database.

By default, every replica is automatically configured to connect in **physical
async streaming replication** with the current primary instance, with a special
user called `streaming_replica`.  The connection between nodes is **encrypted**
and authentication is via **TLS client certificates**.

Currently, the operator allows administrators to add `pg_hba.conf` lines directly in the manifest
as part of the `pg_hba` section of the `postgresql` configuration. The lines defined in the
manifest are added to a default `pg_hba.conf`.

For further detail on how `pg_hba.conf` is managed by the operator, see the
["PostgreSQL Configuration" page](postgresql_conf.md#the-pg_hba-section) of the documentation.

!!! Important
    Examples assume that the Kubernetes cluster runs in a private and secure network.
