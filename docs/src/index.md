# CloudNativePG

**CloudNativePG** is an open source
[operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
designed to manage [PostgreSQL](https://www.postgresql.org/) workloads on any
supported [Kubernetes](https://kubernetes.io) cluster running in private,
public, hybrid, or multi-cloud environments.
CloudNativePG adheres to DevOps principles and concepts such as declarative
configuration and immutable infrastructure.

It defines a new Kubernetes resource called `Cluster` representing a PostgreSQL
cluster made up of a single primary and an optional number of replicas that co-exist
in a chosen Kubernetes namespace for High Availability and offloading of
read-only queries.

Applications that reside in the same Kubernetes cluster can access the
PostgreSQL database using a service which is solely managed by the operator,
without having to worry about changes of the primary role following a failover
or a switchover. Applications that reside outside the Kubernetes cluster, need
to configure a Service or Ingress object to expose the Postgres via TCP.
Web applications can take advantage of the native connection pooler based on PgBouncer.

CloudNativePG was originally built by [EDB](https://www.enterprisedb.com), then
released open source under Apache License 2.0 and submitted for CNCF Sandbox in April 2022.
The [source code repository is in Github](https://github.com/cloudnative-pg/cloudnative-pg).

!!! Note
    Based on the [Operator Capability Levels model](operator_capability_levels.md),
    users can expect a **"Level V - Auto Pilot"** set of capabilities from the
    CloudNativePG Operator.

## Supported Kubernetes distributions

Each minor release of CloudNativePG is designed to work with a range of
Kubernetes versions, usually the ones supported by the CNCF at the time the
minor version was first released.

Please refer to the ["Supported releases"](supported_releases.md) page for details.

## Container images

The [CloudNativePG community](https://github.com/cloudnative-pg)  maintains
container images for both the operator and the operand, that is PostgreSQL.

The CloudNativePG operator container images are [distroless](https://github.com/GoogleContainerTools/distroless)
and available on the [`cloudnative-pg` project's GitHub Container Registry](https://github.com/cloudnative-pg/cloudnative-pg/pkgs/container/cloudnative-pg).

The PostgreSQL operand container images are available for all the
[PGDG supported versions of PostgreSQL](https://www.postgresql.org/),
on multiple architectures, directly from the
[`postgres-containers` project's GitHub Container Registry](https://github.com/cloudnative-pg/postgres-containers/pkgs/container/postgresql).

Additionally, the Community provides images for the [PostGIS extension](postgis.md).

!!! Warning
    CloudNativePG requires that all nodes in a Kubernetes cluster have the
    same CPU architecture, thus a hybrid CPU architecture Kubernetes cluster is not
    supported.

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
* Declarative management of PostgreSQL configuration, including certain popular
  Postgres extensions through the cluster `spec`: `pgaudit`, `auto_explain`,
  `pg_stat_statements`, and `pg_failover_slots`
* Declarative management of Postgres roles, users and groups
* Support for Local Persistent Volumes with PVC templates
* Reuse of Persistent Volumes storage in Pods
* Separate volume for WAL files
* Rolling updates for PostgreSQL minor versions
* In-place or rolling updates for operator upgrades
* TLS connections and client certificate authentication
* Support for custom TLS certificates (including integration with cert-manager)
* Continuous backup to an object store (AWS S3 and S3-compatible, Azure Blob Storage, and Google Cloud Storage)
* Full recovery and Point-In-Time recovery from an existing backup in an object store
* Offline import of existing PostgreSQL databases, including major upgrades of PostgreSQL
* Fencing of an entire PostgreSQL cluster, or a subset of the instances in a declarative way
* Hibernation of a PostgreSQL cluster in a declarative way
* Support for Synchronous Replicas
* Support for HA physical replication slots at cluster level
* Backup from a standby
* Backup retention policies (based on recovery window)
* Parallel WAL archiving and restore to allow the database to keep up with WAL
  generation on high write systems
* Support tagging backup files uploaded to an object store to enable optional
  retention management at the object store layer Replica clusters for
* PostgreSQL deployments across multiple Kubernetes
  clusters, enabling private, public, hybrid, and multi-cloud architectures
* Connection pooling with PgBouncer
* Support for node affinity via `nodeSelector`
* Native customizable exporter of user defined metrics for Prometheus through the `metrics` port (9187)
* Standard output logging of PostgreSQL error messages in JSON format
* Automatically set `readOnlyRootFilesystem` security context for pods
* `cnpg` plugin for `kubectl`
* Simple bind and search+bind LDAP client authentication
* Multi-arch format container images

!!! Info
    CloudNativePG does not use `StatefulSet`s for managing data persistence.
    Rather, it manages persistent volume claims (PVCs) directly. If you are
    curious, read ["Custom Pod Controller"](controller.md) to know more.

## About this guide

Follow the instructions in the ["Quickstart"](quickstart.md) to test CloudNativePG
on a local Kubernetes cluster using Kind, or Minikube.

In case you are not familiar with some basic terminology on Kubernetes and PostgreSQL,
please consult the ["Before you start" section](before_you_start.md).

*[Postgres, PostgreSQL and the Slonik Logo](https://www.postgresql.org/about/policies/trademarks/)
are trademarks or registered trademarks of the PostgreSQL Community Association
of Canada, and used with their permission.*
