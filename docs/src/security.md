This section contains information about security for Cloud Native PostgreSQL,
from a few standpoints: source code, Kubernetes and PostgreSQL.

!!! Warning
    The information contained in this page must not exonerate you from
    performing regular InfoSec duties on your Kubernetes cluster.

## Source code static analysis

Source code of Cloud Native PostgreSQL is *systematically scanned* for static analysis purposes,
including **security problems**, using a popular open source for Go called
[GolangCI-Lint](https://github.com/golangci/golangci-lint) directly in the CI/CD pipeline.
GolangCI-Lint has the ability to run several *linters* on the same source code.

One of these is [Golang Security Checker](https://github.com/securego/gosec), or simply `gosec`,
a linter that scans the abstract syntactic tree of the source against a set of rules aimed at
the discovery of well known vulnerabilities, threats and weaknesses hidden in
the code such as hard-coded credentials, integer overflows and SQL injections - to name a few.

!!! Important
    A failure in the static code analysis phase of the CI/CD pipeline is a blocker
    for the entire delivery of Cloud Native PostgreSQL, meaning that each commit is validated
    against all the linters defined by GolangCI-Lint.

Source code is also regularly inspected through [Coverity Scan by Synopsys](https://scan.coverity.com/)
via 2ndQuadrant's internal CI/CD pipeline.

## Kubernetes

### Pod Security Policies

A [Pod Security Policy](https://kubernetes.io/docs/concepts/policy/pod-security-policy/)
is the Kubernetes way to define security rules and specifications that a pod needs to meet
in order to run in a cluster.
For InfoSec reasons, every Kubernetes platform should implement them.

Cloud Native PostgreSQL does not require *privileged* mode for containers execution.
PostgreSQL servers run as `postgres` system user. No component whatsoever requires to run as `root`.

Likewise, Volumes access does not require *privileges* mode or `root` privileges either.
Proper permissions must be properly assigned by the Kubernetes platform and/or administrators.

### Resources

In a typical Kubernetes cluster, containers run with unlimited resources. By default,
they might be allowed to use as much CPU and RAM as needed.

Cloud Native PostgreSQL allows administrators to control and manage resource usage by the pods of the cluster,
through the `resources` section of the manifest. For details, please refer to the
["Resources" section in the "Custom Resource Definitions" page](crd.md#resources).

## PostgreSQL

The current implementation of Cloud Native PostgreSQL automatically creates
passwords and `.pgpass` files for the `postgres` superuser and for the owner of
the database. See the
["Secrets" section in the "Architecture" page](architecture.md#secrets).

Those files can be used to configure application access to the database.

Currently, the operator allows administrators to add `pg_hba.conf` lines directly in the manifest, as part of the
`pg_hba` section of the `postgresql` configuration. The lines defined in the
manifest are added to a default `pg_hba.conf`

```
# Grant local access
local all all peer

# HBA lines in the pg_hba section are added here

# Require md5 authentication elsewhere
host all all all md5
host replication all all md5
```

!!! Important
    Examples assume that the Kubernetes cluster runs in a private and secure network.
