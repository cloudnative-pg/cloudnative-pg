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

Memory requests and limits are associated with Containers, but it is useful to think of a Pod as having a memory request
and limit. The memory request for the Pod is the sum of the memory requests for all the Containers in the Pod.

Pod scheduling is based on requests and not limits. A Pod is scheduled to run on a Node only if the Node has enough
available memory to satisfy the Pod's memory request.

For each resource, we divide containers into 3 QoS classes: Guaranteed, Burstable, and
Best-Effort, in decreasing order of priority.
For more details, please refer to ["Resource Quality of Service in Kubernetes"](https://kubernetes.io/docs/tasks/configure-pod-container/quality-service-pod/#qos-classes)

To avoid resources related issues in k8s, we can refer the best practices for out of resource handling while creating
a cluster:

-  Specify our required values for memory and CPU in the resources section of the manifest file.
   This way we can avoid the `OOM Killed` (where OOM stands for Out Of Memory) and `CPU throttle` or any other resources
   related issues on running instances.
-  In order for the pods of your cluster to get assigned to the `Guaranteed` QoS class, you must set limits and requests
   for both memory and CPU to the same value.
-  Specify our required PostgreSQL memory parameters that can be helpful to manage memory in PostgreSQL.
-  Set up database server pods on a dedicated node using nodeSelector.
   See the ["nodeSelector field of the affinityconfiguration resource on the API reference page"](api_reference.md#affinityconfiguration).

You can refer the following example manifest:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
metadata:
  name: postgresql-resources
spec:

  instances: 3

  postgresql:
    parameters:
      shared_buffers: "256MB"

  resources:
    requests:
      memory: "1024Mi"
      cpu: 1
    limits:
      memory: "1024Mi"
      cpu: 1

  storage:
    size: 1Gi
```

In the above example, we have specified `shared_buffers` parameter whose value is `256 MB` i.e. how much memory is
dedicated to the server for caching data (The default value for this parameter is 128 MB in case it's not defined)

A reasonable starting value for shared_buffers is 25% of the memory in your system.
For example: if your `shared_buffers` is 256 MB then the recommended value for your container memory size is 1 GB,
which means that within a pod all the containers will have a total of 1 GB memory that Kubernetes will always preserve
so that our containers will work as expected.
For more details, please refer to the ["Performance tuning parameters of postgresql"](https://www.postgresql.org/docs/current/runtime-config-resource.html)

!!! See also "Managing Compute Resources for Containers"
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
