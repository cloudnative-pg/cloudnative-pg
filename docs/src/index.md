# CloudNativePG
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

CloudNativePG (CNPG) is an open-source
[operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
designed to manage [PostgreSQL](https://www.postgresql.org/) workloads on any
supported [Kubernetes](https://kubernetes.io) cluster.
It fosters cloud-neutrality through seamless deployment in private, public,
hybrid, and multi-cloud environments via its
[distributed topology](replica_cluster.md#distributed-topology) feature.

Built around DevOps principles, CloudNativePG embraces declarative
configuration and immutable infrastructure, ensuring reliability and automation
in database management.

At its core, CloudNativePG introduces a custom Kubernetes resource called
`Cluster`, representing a PostgreSQL cluster with:

- A single primary instance for write operations.
- Optional replicas for High Availability and read scaling.

These instances reside within a Kubernetes namespace, allowing applications to
connect seamlessly using operator-managed services. Failovers and switchovers
occur transparently, eliminating the need for manual intervention.

For applications inside the Kubernetes cluster, CNPG provides a microservice
database approach, enabling co-location of PostgreSQL clusters and applications
in the same namespace for optimized access.
For applications outside the cluster, CNPG offers flexible connectivity through
service templates and `LoadBalancer` services for direct TCP exposure.
Additionally, web applications can take advantage of the native connection
pooler based on PgBouncer.

CloudNativePG was originally built by [EDB](https://www.enterprisedb.com), then
released open source under Apache License 2.0.
The [source code repository is in GitHub](https://github.com/cloudnative-pg/cloudnative-pg).

!!! Note
    Based on the [Operator Capability Levels model](operator_capability_levels.md),
    users can expect a "Level V - Auto Pilot" subset of capabilities from the
    CloudNativePG Operator.

## Supported Kubernetes distributions

Each minor release of CloudNativePG is designed to work with a range of
Kubernetes versions, usually the ones supported by the CNCF at the time the
minor version was first released.

Please refer to the ["Supported releases"](supported_releases.md) page for details.

## Container images

The [CloudNativePG community](https://github.com/cloudnative-pg) maintains
container images for both the operator and PostgreSQL (the operand).

### Operator

The CloudNativePG operator container images are available on the
[`cloudnative-pg` project's GitHub Container Registry](https://github.com/cloudnative-pg/cloudnative-pg/pkgs/container/cloudnative-pg)
in three different flavors:

- Debian 12 distroless
- Red Hat UBI 9 micro (suffix `-ubi9`)

Red Hat UBI images are primarily intended for OLM consumption.

All container images are signed and include SBOM and provenance attestations,
provided separately for each architecture.

### Operands

The PostgreSQL operand container images are available for all
[PGDG supported versions of PostgreSQL](https://www.postgresql.org/),
across multiple architectures, directly from the
[`postgres-containers` project's GitHub Container Registry](https://github.com/cloudnative-pg/postgres-containers/pkgs/container/postgresql).

All container images are signed and include SBOM and provenance attestations,
provided separately for each architecture.

Weekly jobs ensure that critical vulnerabilities (CVEs) in the entire stack are
promptly addressed.

Additionally, the community provides images for the [PostGIS extension](postgis.md).

## Main features

- Direct integration with the Kubernetes API server for High Availability,
  eliminating the need for external tools.
- Self-healing capabilities, including:
    - Automated failover by promoting the most aligned replica.
    - Automatic recreation of failed replicas.
- Planned switchover of the primary instance by promoting a selected replica.
- Declarative management of key PostgreSQL configurations, including:
    - PostgreSQL settings.
    - Roles, users, and groups.
    - Databases, extensions, and schemas.
    - Tablespaces (including temporary tablespaces).
- Flexible instance definition, supporting any number of instances (minimum 1
  primary server).
- Scale-up/down capabilities to dynamically adjust cluster size.
- Read-Write and Read-Only Services, ensuring applications connect correctly:
    - *Read-Write Service*: Routes connections to the primary server.
    - *Read-Only Service*: Distributes connections among replicas for read workloads.
- Support for quorum-based and priority-based PostgreSQL Synchronous
  Replication.
- Replica clusters enabling PostgreSQL distributed topologies across multiple
  Kubernetes clusters (private, public, hybrid, and multi-cloud).
- Delayed Replica clusters for point-in-time access to historical data.
- Persistent volume management, including:
    - Support for Local Persistent Volumes with PVC templates.
    - Reuse of Persistent Volumes storage in Pods.
    - Separate volumes for WAL files and tablespaces.
- Backup and recovery options, including:
    - Integration with the [Barman Cloud plugin](https://github.com/cloudnative-pg/plugin-barman-cloud)
      for continuous online backup via WAL archiving to AWS S3, S3-compatible
      services, Azure Blob Storage, and Google Cloud Storage, with support for
      retention policies based on a configurable recovery window.
    - Backups using volume snapshots (where supported by storage classes).
    - Full and Point-In-Time recovery from volume snapshots or object stores (via Barman Cloud plugin).
    - Backup from standby replicas to reduce primary workload impact.
- Offline and online import of PostgreSQL databases, including major upgrades:
    - *Offline Import*: Direct restore from existing databases.
    - *Online Import*: PostgreSQL native logical replication via the `Subscription` resource.
- High Availability physical replication slots, including synchronization of
  user-defined replication slots.
- Parallel WAL archiving and restore, ensuring high-performance data
  synchronization in high-write environments.
- TLS support, including:
    - Secure connections and client certificate authentication.
    - Custom TLS certificates (integrated with `cert-manager`).
- Startup and readiness probes, including replica probes based on desired lag
  from the primary.
- Declarative rolling updates for:
    - PostgreSQL minor versions.
    - Operator upgrades (in-place or rolling updates).
- Standard output logging of PostgreSQL error messages in JSON format for
  easier integration with log aggregation tools.
- Prometheus-compatible metrics exporter (`metrics` port 9187) for custom
  monitoring.
- `cnpg` plugin for `kubectl` to simplify cluster operations.
- Cluster hibernation for resource efficiency in inactive states.
- Fencing of PostgreSQL clusters (full cluster or subset) to isolate instances
  when needed.
- Connection pooling with PgBouncer for improved database efficiency.
- OLM (Operator Lifecycle Manager) installation support for streamlined
  deployments.
- Multi-arch container images, including Software Bill of Materials (SBOM) and
  provenance attestations for security compliance.

!!! Info
    CloudNativePG does not use `StatefulSet`s for managing data persistence.
    Instead, it directly manages Persistent Volume Claims (PVCs).
    See ["Custom Pod Controller"](controller.md) for more details.

## About this guide

Follow the instructions in the ["Quickstart"](quickstart.md) to test
CloudNativePG on a local Kubernetes cluster using Kind, or Minikube.

In case you are not familiar with some basic terminology on Kubernetes and PostgreSQL,
please consult the ["Before you start" section](before_you_start.md).

The CloudNativePG documentation is licensed under a Creative Commons
Attribution 4.0 International License.

---

*[Postgres, PostgreSQL, and the Slonik Logo](https://www.postgresql.org/about/policies/trademarks/)
are trademarks or registered trademarks of the PostgreSQL Community Association
of Canada, and used with their permission.*

---

CloudNativePG is a
[Cloud Native Computing Foundation Sandbox project](https://www.cncf.io/sandbox-projects/).

![](https://github.com/cncf/artwork/blob/main/other/cncf/horizontal/color/cncf-color.png?raw=true)
