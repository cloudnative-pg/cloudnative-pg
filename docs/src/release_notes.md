# Release notes

History of user-visible changes for Cloud Native PostgreSQL.

## Version 1.8.0

**Release date:** 13 September 2021

Features:

- Bootstrap a new cluster via full or Point-In-Time Recovery directly from an
  object store defined in the external cluster section, eliminating the
  previous requirement to have a Backup CR defined
- Introduce the `immediate` option in scheduled backups to request a backup
  immediately after the first Postgres instance running, adding the capability
  to rewind to the very beginning of a cluster when Point-In-Time Recovery is
  configured
- Add the `firstRecoverabilityPoint` in the cluster status to report the oldest
  consistent point in time to request a recovery based on the backup object
  store’s content
- Enhance the default Prometheus exporter for a PostgreSQL instance by exposing
  the following new metrics:

    1. number of WAL files and computed total size on disk
    2. number of `.ready` and `.done` files in the archive status folder
    3. flag for replica mode
    4. number of requested minimum/maximum synchronous replicas, as well as
       the expected and actually observed ones

- Add support for the `runonserver` option when defining custom metrics in the
  Prometheus exporter to limit the collection of a metric to a range of
  PostgreSQL versions
- Natively support Azure Blob Storage for backup and recovery, by taking
  advantage of the feature introduced in Barman 2.13 for Barman Cloud
- Rely on `pg_isready` for the liveness probe
- Support RFC3339 format for timestamp specification in recovery target times
- Introduce `.spec.imagePullPolicy` to control the pull policy of image
  containers for all pods and jobs created for a cluster
- Add support for OpenShift 4.8, which replaces OpenShift 4.5
- Support PostgreSQL 14 (beta)
- Enhance the replica cluster feature with cross-cluster replication from an
  object store defined in an external cluster section, without requiring a
  streaming connection (experimental)
- Introduce `logLevel` option to the cluster's spec to specify one of the
  following levels: error, info, debug or trace

Security Enhancements:

- Introduce `.spec.enableSuperuserAccess` to enable/disable network access with the
  `postgres` user through password authentication

Fixes:

- Properly inform users when a cluster enters an unrecoverable state and
  requires human intervention

## Version 1.7.1

**Release date:** 11 August 2021

Features:

- Prefer self-healing over configuration with regards to synchronous
  replication, empowering the operator to temporarily override
  `minSyncReplicas` and `maxSyncReplicas` settings in case the cluster is not
  able to meet the requirements during self-healing operations
- Introduce the `postInitSQL` option as part of the `initdb` bootstrap method
  to specify a list of SQL queries to be executed as a superuser immediately
  after the cluster has been created

Fixes:

- Allow the operator to failover when the primary is not ready (bug introduced in 1.7.0)
- Execute administrative queries using the `LOCAL` synchronous commit level
- Correctly parse multi-line log entries in PGAudit

## Version 1.7.0

**Release date:** 28 July 2021

Features:

- Add native support to PGAudit with a new type of `logger` called `pgaudit`
  directly available in the JSON output
- Enhance monitoring and observability capabilities through:

    - Native support for the `pg_stat_statements` and `auto_explain` extensions
    - The `target_databases` option in the Prometheus exporter to run a
      user-defined metric query on one or more databases (including
      auto-discovery of databases through shell-like pattern matching)
    - Exposure of the `manual_switchover_required` metric to promptly report
      whether a cluster with `primaryUpdateStrategy` set to `supervised`
      requires a manual switchover

- Transparently handle `shared_preload_libraries` for `pg_audit`,
  `auto_explain` and `pg_stat_statements`

    - Automatic configuration of `shared_preload_libraries` for PostgreSQL when
      `pg_stat_statements`, ` pgaudit` or `auto_explain` options are added to
      the `postgresql` parameters section

- Support the `k8s.enterprisedb.io/reload` label to finely control the
  automated reload of config maps and secrets, including those used for custom
  monitoring/alerting metrics in the Prometheus exporter or to store certificates
- Add the `reload` command to the `cnp` plugin for `kubectl` to trigger a
  reconciliation loop on the instances
- Improve control of pod affinity and anti-affinity configurations through
  `additionalPodAffinity` and `additionalPodAntiAffinity`
- Introduce a separate `PodDisruptionBudget` for primary instances, by
  requiring at least a primary instance to run at any time

Security Enhancements:

- Add the `.spec.certificates.clientCASecret` and
  `spec.certificates.replicationTLSSecret` options to define custom client
  Certification Authority and certificate for the PostgreSQL server, to be used
  to authenticate client certificates and secure communication between PostgreSQL
  nodes
- Add the `.spec.backup.barmanObjectStore.endpointCA` option to define the
  custom Certification Authority bundle of the endpoint of Barman’s backup
  object store

Fixes:

- Correctly parse histograms in the Prometheus exporter
- Reconcile services created by the operator for a cluster

## Version 1.6.0

**Release date:** 12 July 2021

Features:

- Replica mode (**EXPERIMENTAL**): allow a cluster to be created as a replica
  of a source cluster. A replica cluster has a *designated primary* and any
  number of standbys.
- Add the `.spec.postgresql.promotionTimeout` parameter to specify the maximum amount of
  seconds to wait when promoting an instance to primary, defaulting to 40000000 seconds.
- Add the `.spec.affinity.podAntiAffinityType` parameter. It can be set to
  `preferred` (default), resulting in
  `preferredDuringSchedulingIgnoredDuringExecution` being used, or to
  `required`,   resulting in `requiredDuringSchedulingIgnoredDuringExecution`.

Changes:

- Fixed a race condition when deleting a PVC and a pod which prevented the
  operator from creating a new pod.
- Fixed a race condition preventing the manager from detecting the need for
  a PostgreSQL restart on a configuration change.
- Fixed a panic in `kubectl-cnp` on clusters without annotations.
- Lowered the level of some log messages to `debug`.
- E2E tests for server CA and TLS injection.

## Version 1.5.1

**Release date:** 17 June 2021

Change: 

- Fix a bug with CRD validation preventing auto-update with Operator Deployments on Red Hat OpenShift
- Allow passing operator's configuration using a Secret.

## Version 1.5.0

**Release date:** 11 June 2021

Features:

- Introduce the `pg_basebackup` bootstrap method to create a new PostgreSQL
  cluster as a copy of an existing PostgreSQL instance of the same major
  version, even outside Kubernetes
- Add support for Kubernetes’ tolerations in the `Affinity` section of the
  `Cluster` resource, allowing users to distribute PostgreSQL instances on
  Kubernetes nodes with the required taint
- Enable specification of a digest to an image name, through the
  `<image>:<tag>@sha256:<digestValue>` format, for more deterministic and
  repeatable deployments

Security Enhancements:

- Customize TLS certificates to authenticate the PostgreSQL server by defining
  secrets for the server certificate and the related Certification Authority
  that signed it
- Raise the `sslmode` for the WAL receiver process of internal and
  automatically managed streaming replicas from `require` to `verify-ca`

Changes:

- Enhance the `promote` subcommand of the `cnp` plugin for `kubectl` to accept
  just the node number rather than the whole name of the pod
- Adopt DNS-1035 validation scheme for cluster names (from which service names
  are inherited)
- Enforce streaming replication connection when cloning a standby instance or
  when bootstrapping using the `pg_basebackup` method
- Integrate the `Backup` resource with `beginWal`, `endWal`, `beginLSN`,
  `endLSN`, `startedAt` and `stoppedAt` regarding the physical base backup
- Documentation improvements:
    - Provide a list of ports exposed by the operator and the operand container
    - Introduce the `cnp-bench` helm charts and guidelines for benchmarking the
      storage and PostgreSQL for database workloads
- E2E tests enhancements:
    - Test Kubernetes 1.21
    - Add test for High Availability of the operator
    - Add test for node draining
- Minor bug fixes, including:
    - Timeout to pg_ctl start during recovery operations too short
    - Operator not watching over direct events on PVCs
    - Fix handling of `immediateCheckpoint` and `jobs` parameter in
      `barmanObjectStore` backups
    - Empty logs when recovering from a backup

## Version 1.4.0

**Release date:** 18 May 2021

Features:

- Standard output logging of PostgreSQL error messages in JSON format
- Provide a basic set of PostgreSQL metrics for the Prometheus exporter
- Add the `restart` command to the `cnp` plugin for `kubectl` to restart
  the pods of a given PostgreSQL cluster in a rollout fashion

Security Enhancements:

- Set `readOnlyRootFilesystem` security context for pods

Changes:

- **IMPORTANT:** If you have previously deployed the Cloud Native PostgreSQL
  operator using the YAML manifest, you must delete the existing operator
  deployment before installing the new version. This is required to avoid
  conflicts with other Kubernetes API's due to a change in labels
  and label selectors being directly managed by the operator. Please refer to
  the Cloud Native PostgreSQL documentation for additional detail on upgrading
  to 1.4.0
- Fix the labels that are automatically defined by the operator, renaming them
  from `control-plane: controller-manager` to
  `app.kubernetes.io/name: cloud-native-postgresql`
- Assign the `metrics` name to the TCP port for the Prometheus exporter
- Set `cnp_metrics_exporter` as the `application_name` to the metrics exporter
  connection in PostgreSQL
- When available, use the application database for monitoring queries of the
  Prometheus exporter instead of the `postgres` database
- Documentation improvements:
    - Customization of monitoring queries
    - Operator upgrade instructions
- E2E tests enhancements
- Minor bug fixes, including:
    - Avoid using `-R` when calling `pg_basebackup`
    - Remove stack trace from error log when getting the status

## Version 1.3.0

**Release date:** 23 Apr 2021

Features:

- Inheritance of labels and annotations
- Set resource limits for every container

Security Enhancements:

- Support for restricted security context constraint on RedHat OpenShift to
  limit pod execution to a namespace allocated UID and SELinux context
- Pod security contexts explicitly defined by the operator to run as
  non-root, non-privileged and without privilege escalation

Changes:

- Prometheus exporter endpoint listening on port 9187 (port 8000 is now
  reserved to instance coordination with API server)
- Documentation improvements
- E2E tests enhancements, including GKE environment
- Minor bug fixes

## Version 1.2.1

**Release date:** 6 Apr 2021

- ScheduledBackup are no longer owners of the Backups, meaning that backups
  are not removed when ScheduledBackup objects are deleted
- Update on ubi8-minimal image to solve RHSA-2021:1024 (Security Advisory: Important)

## Version 1.2.0

**Release date:** 31 Mar 2021

- Introduce experimental support for custom monitoring queries as ConfigMap and
  Secret objects using a compatible syntax with `postgres_exporter` for Prometheus
- Support Operator Lifecycle Manager (OLM) deployments, with the subsequent
  presence on OperatorHub.io
- Enhance container security by applying guidelines from the US Department of
  Defense (DoD)'s Defense Information Systems Agency (DISA) and the Center for
  Internet Security (CIS) and verifying them directly in the pipeline with
  Dockle
- Improve E2E tests on AKS
- Minor bug fixes

## Version 1.1.0

**Release date:** 3 Mar 2021

- Add `kubectl cnp status` to pretty-print the status of a cluster, including
  JSON and YAML output
- Add `kubectl cnp certificate` to enable TLS authentication for client applications
- Add the `-ro` service to route connections to the available hot
  standby replicas only, enabling offload of read-only queries from
  the cluster's primary instance
- Rollback scaling down a cluster to a value lower than `maxSyncReplicas`
- Request a checkpoint before demoting a former primary
- Send `SIGINT` signal (fast shutdown) to PostgreSQL process on `SIGTERM`
- Minor bug fixes

## Version 1.0.0

**Release date:** 4 Feb 2021

The first major stable release of Cloud Native PostgreSQL implements `Cluster`,
`Backup` and `ScheduledBackup` in the API group `postgresql.k8s.enterprisedb.io/v1`.
It uses these resources to create and manage PostgreSQL clusters inside
Kubernetes with the following main capabilities:

- Direct integration with Kubernetes API server for High Availability, without
  requiring an external tool
- Self-Healing capability, through:
    - failover of the primary instance by promoting the most aligned replica
    - automated recreation of a replica
- Planned switchover of the primary instance by promoting a selected replica
- Scale up/down capabilities
- Definition of an arbitrary number of instances (minimum 1 - one primary server)
- Definition of the *read-write* service to connect your applications to the
  only primary server of the cluster
- Definition of the *read* service to connect your applications to any of the
  instances for reading workloads
- Support for Local Persistent Volumes with PVC templates
- Reuse of Persistent Volumes storage in Pods
- Rolling updates for PostgreSQL minor versions and operator upgrades
- TLS connections and client certificate authentication
- Continuous backup to an S3 compatible object store
- Full recovery and point-in-time recovery from an S3 compatible object store backup
- Support for synchronous replicas
- Support for node affinity via `nodeSelector` property
- Standard output logging of PostgreSQL error messages
