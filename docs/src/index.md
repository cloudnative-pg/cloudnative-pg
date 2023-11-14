# CloudNativePG

CloudNativePG is an open source
[operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
designed to manage [PostgreSQL](https://www.postgresql.org/) workloads on any
supported [Kubernetes](https://kubernetes.io) cluster running in private,
public, hybrid, or multi-cloud environments.
CloudNativePG adheres to DevOps principles and concepts such as declarative
configuration and immutable infrastructure.

It defines a new Kubernetes resource called `Cluster` representing a PostgreSQL
cluster. It's made up of a single primary and an optional number of replicas that coexist
in a chosen Kubernetes namespace for high availability and offloading of
read-only queries.

Applications that reside in the same Kubernetes cluster can access the
PostgreSQL database using a service that's solely managed by the operator.
This capability means you don't have to worry about changes of the primary role following a failover
or a switchover. Applications that reside outside the Kubernetes cluster need
to configure a service or Ingress object to expose the Postgres by way of TCP.
Web applications can take advantage of the native connection pooler based on PgBouncer.

CloudNativePG was originally built by [EDB](https://www.enterprisedb.com) and then
released as open source under Apache License 2.0 and submitted for CNCF Sandbox in April 2022.
The [source code repository is in Github](https://github.com/cloudnative-pg/cloudnative-pg).

!!! Note
    Based on the [operator capability levels model](operator_capability_levels.md),
    you can expect a "Level V - Auto Pilot" set of capabilities from the
    CloudNativePG operator.

## Supported Kubernetes distributions

Each minor release of CloudNativePG is designed to work with a range of
Kubernetes versions, usually the ones supported by the CNCF at the time the
minor version was first released.

See [Supported releases](supported_releases.md) for details.

## Container images

The [CloudNativePG community](https://github.com/cloudnative-pg)  maintains
container images for both the operator and the operand, that is, PostgreSQL.

The CloudNativePG operator container images are [distroless](https://github.com/GoogleContainerTools/distroless)
and available on the [`cloudnative-pg` project's GitHub Container registry](https://github.com/cloudnative-pg/cloudnative-pg/pkgs/container/cloudnative-pg).

The PostgreSQL operand container images are available for all the
[PGDG-supported versions of PostgreSQL](https://www.postgresql.org/),
on multiple architectures, directly from the
[`postgres-containers` project's GitHub Container registry](https://github.com/cloudnative-pg/postgres-containers/pkgs/container/postgresql).

Additionally, the community provides images for the [PostGIS extension](postgis.md).

!!! Warning
    CloudNativePG requires that all nodes in a Kubernetes cluster have the
    same CPU architecture. Thus, a hybrid CPU architecture Kubernetes cluster isn't
    supported.

## Main features

* Direct integration with Kubernetes API server for high availability,
  without requiring an external tool
* Self-healing capability, through:
    * Failover of the primary instance by promoting the most aligned replica
    * Automated recreation of a replica
* Planned switchover of the primary instance by promoting a selected replica
* Scale up/down capabilities
* Definition of an arbitrary number of instances (minimum 1 - one primary server)
* Definition of the *read-write* service, to connect your applications to the only primary server of the cluster
* Definition of the *read-only* service, to connect your applications to any of the instances for reading workloads
* Declarative management of PostgreSQL configuration, including certain popular
  Postgres extensions through the cluster `spec`: `pgaudit`, `auto_explain`,
  `pg_stat_statements`, and `pg_failover_slots`
* Declarative management of Postgres roles, users, and groups
* Support for local persistent volumes with persistent volume claims (PVC) templates
* Reuse of persistent volumes storage in pods
* Separate volume for WAL files
* Rolling updates for PostgreSQL minor versions
* In-place or rolling updates for operator upgrades
* TLS connections and client certificate authentication
* Support for custom TLS certificates (including integration with cert-manager)
* Continuous WAL archiving to an object store (AWS S3 and S3-compatible, Azure Blob Storage, and Google Cloud Storage)
* Backups on volume snapshots (where supported by the underlying storage classes)
* Backups on object stores (AWS S3 and S3-compatible, Azure Blob Storage, and Google Cloud Storage)
* Full recovery and point-in-time recovery from an existing backup on volume snapshots or object stores
* Offline import of existing PostgreSQL databases, including major upgrades of PostgreSQL
* Fencing of an entire PostgreSQL cluster or a subset of the instances in a declarative way
* Hibernation of a PostgreSQL cluster in a declarative way
* Support for synchronous replicas
* Support for HA physical replication slots at cluster level
* Backup from a standby
* Backup retention policies (based on recovery window, only on object stores)
* Parallel WAL archiving and restore to allow the database to keep up with WAL
  generation on high write systems
* Support for tagging backup files uploaded to an object store to enable optional
  retention management at the object store layer replica clusters
* PostgreSQL deployments across multiple Kubernetes
  clusters, enabling private, public, hybrid, and multi-cloud architectures
* Connection pooling with PgBouncer
* Support for node affinity by way of `nodeSelector`
* Native customizable exporter of user-defined metrics for Prometheus through the `metrics` port (9187)
* Standard output logging of PostgreSQL error messages in JSON format
* Automatically setting of `readOnlyRootFilesystem` security context for pods
* `cnpg` plugin for `kubectl`
* Simple bind and search+bind LDAP client authentication
* Multi-arch format container images
* OLM installation

!!! Info
    CloudNativePG doesn't use `StatefulSet` for managing data persistence.
    Rather, it manages PVCs directly. 
    For more information, see [Custom pod controller"](controller.md).

## Getting started

Follow the instructions in [Quick start](quickstart.md) to test CloudNativePG
on a local Kubernetes cluster using Kind or Minikube.

If you aren't familiar with some basic terminology on Kubernetes and PostgreSQL,
see [Before you start](before_you_start.md).

*[Postgres, PostgreSQL, and the Slonik logo](https://www.postgresql.org/about/policies/trademarks/)
are trademarks or registered trademarks of the PostgreSQL Community Association
of Canada, and used with their permission.*
