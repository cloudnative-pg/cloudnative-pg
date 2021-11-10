# Cloud Native PostgreSQL

**Cloud Native PostgreSQL** is an [operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
designed by [EnterpriseDB](https://www.enterprisedb.com)
to manage [PostgreSQL](https://www.postgresql.org/) workloads on any supported [Kubernetes](https://kubernetes.io)
cluster running in private, public, hybrid, or multi-cloud environments.
Cloud Native PostgreSQL adheres to DevOps principles and concepts
such as declarative configuration and immutable infrastructure.

It defines a new Kubernetes resource called "Cluster" representing a PostgreSQL
cluster made up of a single primary and an optional number of replicas that co-exist
in a chosen Kubernetes namespace for High Availability and offloading of
read-only queries.

Applications that reside in the same Kubernetes cluster can access the
PostgreSQL database using a service which is solely managed by the operator,
without having to worry about changes of the primary role following a failover
or a switchover. Applications that reside outside the Kubernetes cluster, need
to configure an Ingress object to expose the service via TCP.

Cloud Native PostgreSQL works with PostgreSQL and is available under the
[EnterpriseDB Limited Use License](https://www.enterprisedb.com/limited-use-license).

!!! Important
    Based on the [Operator Capability Levels model](operator_capability_levels.md),
    users can expect a **"Level V - Auto Pilot"** set of capabilities from the
    Cloud Native PostgreSQL Operator.

## Supported Kubernetes distributions

Cloud Native PostgreSQL requires Kubernetes 1.17 or higher.

Cloud Native PostgreSQL has also been certified for
[RedHat OpenShift Container Platform (OCP)](https://www.openshift.com/products/container-platform)
4.6+ and is available directly from the [RedHat Catalog](https://catalog.redhat.com/).
OpenShift Container Platform is an open-source distribution of Kubernetes which is
[maintained and commercially supported](https://access.redhat.com/support/policy/updates/openshift#ocp4)
by Red Hat.

!!! Important
    Please take into account that some delay may occur when releasing Cloud
    Native PostgreSQL on RedHat's OpenShift Container Platform, as the process is
    not entirely under our control.

Please refer to the
["Platform Compatibility"](https://www.enterprisedb.com/product-compatibility#cnp)
page from the EDB website for a list of the currently supported Kubernetes distributions.

### Multiple architectures

The Cloud Native PostgreSQL Operator container images support the multi-arch
format for the following platforms: `linux/amd64`, `linux/arm64`,
`linux/ppc64le`, `linux/s390x`.

!!! Warning
    Cloud Native PostgreSQL requires that all nodes in a Kubernetes cluster have the
    same CPU architecture, thus a hybrid CPU architecture Kubernetes cluster is not
    supported.

## Supported Postgres versions

The following versions of Postgres are currently supported:

- PostgreSQL 14, 13, 12, 11 and 10 (`linux/amd64`)

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
* Continuous backup to an S3 compatible object store
* Backup retention policies (based on recovery window)
* Full recovery and Point-In-Time recovery from an S3 compatible object store backup
* Replica clusters for PostgreSQL deployments across multiple Kubernetes
  clusters, enabling private, public, hybrid, and multi-cloud architectures
* Support for Synchronous Replicas
* Connection pooling with PgBouncer
* Support for node affinity via `nodeSelector`
* Native customizable exporter of user defined metrics for Prometheus through the `metrics` port (9187)
* Standard output logging of PostgreSQL error messages in JSON format
* Support for the `restricted` security context constraint (SCC) in Red Hat OpenShift
* `cnp` plugin for `kubectl`
* Multi-arch format container images

## About this guide

Follow the instructions in the ["Quickstart"](quickstart.md) to test Cloud Native PostgreSQL
on a local Kubernetes cluster using Minikube or Kind.

In case you are not familiar with some basic terminology on Kubernetes and PostgreSQL,
please consult the ["Before you start" section](before_you_start.md).

!!! Note
    Although the guide primarily addresses Kubernetes, all concepts can
    be extended to OpenShift as well.
