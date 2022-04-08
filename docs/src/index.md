# CloudNativePG

**CloudNativePG** is an [operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
designed by [EDB](https://www.enterprisedb.com)
to manage [PostgreSQL](https://www.postgresql.org/) workloads on any supported [Kubernetes](https://kubernetes.io)
cluster running in private, public, hybrid, or multi-cloud environments.
CloudNativePG adheres to DevOps principles and concepts
such as declarative configuration and immutable infrastructure.

It defines a new Kubernetes resource called "Cluster" representing a PostgreSQL
cluster made up of a single primary and an optional number of replicas that co-exist
in a chosen Kubernetes namespace for High Availability and offloading of
read-only queries.

Applications that reside in the same Kubernetes cluster can access the
PostgreSQL database using a service which is solely managed by the operator,
without having to worry about changes of the primary role following a failover
or a switchover. Applications that reside outside the Kubernetes cluster, need
to configure a Service or Ingress object to expose the Postgres via TCP.
Web applications can take advantage of the native connection pooler based on PgBouncer.

CloudNativePG works with PostgreSQL and is available under the
[EDB Limited Use License](https://www.enterprisedb.com/limited-use-license).

!!! Note
    Based on the [Operator Capability Levels model](operator_capability_levels.md),
    users can expect a **"Level V - Auto Pilot"** set of capabilities from the
    CloudNativePG Operator.

## Supported Kubernetes distributions

CloudNativePG requires Kubernetes 1.19 or higher.

CloudNativePG has also been certified for
[Red Hat OpenShift Container Platform (OCP)](https://www.openshift.com/products/container-platform)
4.6+ and is available directly from the [Red Hat Catalog](https://catalog.redhat.com/).
OpenShift Container Platform is an open-source distribution of Kubernetes which is
[maintained and commercially supported](https://access.redhat.com/support/policy/updates/openshift#ocp4)
by Red Hat.

!!! Important
    Please take into account that some delay may occur when releasing Cloud
    Native PostgreSQL on Red Hat's OpenShift Container Platform, as the process is
    not entirely under our control.

Please refer to the
["Platform Compatibility"](https://www.enterprisedb.com/product-compatibility#cnp)
page from the EDB website for a list of the currently supported Kubernetes distributions.

### Multiple architectures

The CloudNativePG Operator container images support the multi-arch
format for the following platforms: `linux/amd64`, `linux/ppc64le`, `linux/s390x`.

!!! Warning
    CloudNativePG requires that all nodes in a Kubernetes cluster have the
    same CPU architecture, thus a hybrid CPU architecture Kubernetes cluster is not
    supported. Additionally, EDB supports `linux/ppc64le` and `linux/s390x` architectures
    on OpenShift only.

## Supported Postgres versions

The following versions of Postgres are currently supported:

- PostgreSQL 14 (default), 13, 12, 11, and 10

All of the above versions, except PostgreSQL 10, are available on the
following platforms: `linux/amd64`, `linux/ppc64le`, `linux/s390x`.
PostgreSQL 10 is available on `linux/amd64` only.
EDB supports operand images for `linux/ppc64le` and `linux/s390x`
architectures on OpenShift only.

## Main features

* Direct integration with Kubernetes API server for High Availability,
  without requiring an external tool
* Self-Healing capability, through:
    * failover of the primary instance by promoting the most aligned replica
    * automated recreation of a replica
* Planned switchover of the primary instance by promoting a selected replica
* Scale up/down capabilities
* Definition of an arbitrary number of instances (minimum 1 - one primary server)
* Definition of the *read-write* service, to connect your applications to the only primary server of the cluster
* Definition of the *read-only* service, to connect your applications to any of the instances for reading workloads
* Support for Local Persistent Volumes with PVC templates
* Reuse of Persistent Volumes storage in Pods
* Rolling updates for PostgreSQL minor versions
* In-place or rolling updates for operator upgrades
* TLS connections and client certificate authentication
* Support for custom TLS certificates (including integration with cert-manager)
* Continuous backup to an object store  (AWS S3 and S3-compatible, Azure Blob Storage, and Google Cloud Storage)
* Backup retention policies (based on recovery window)
* Full recovery and Point-In-Time recovery from an existing backup in an object store
* Replica clusters for PostgreSQL deployments across multiple Kubernetes
  clusters, enabling private, public, hybrid, and multi-cloud architectures
* Support for Synchronous Replicas
* Connection pooling with PgBouncer
* Support for node affinity via `nodeSelector`
* Native customizable exporter of user defined metrics for Prometheus through the `metrics` port (9187)
* Standard output logging of PostgreSQL error messages in JSON format
* Support for the `restricted` security context constraint (SCC) in Red Hat OpenShift
* `cnp` plugin for `kubectl`
* Fencing of an entire PostgreSQL cluster, or a subset of the instances
* Multi-arch format container images

## About this guide

Follow the instructions in the ["Quickstart"](quickstart.md) to test CloudNativePG
on a local Kubernetes cluster using Minikube or Kind.

In case you are not familiar with some basic terminology on Kubernetes and PostgreSQL,
please consult the ["Before you start" section](before_you_start.md).

!!! Note
    Although the guide primarily addresses Kubernetes, all concepts can
    be extended to OpenShift as well.

*[Postgres, PostgreSQL and the Slonik Logo](https://www.postgresql.org/about/policies/trademarks/)
are trademarks or registered trademarks of the PostgreSQL Community Association
of Canada, and used with their permission.*
