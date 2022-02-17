# Release notes

History of user-visible changes for Cloud Native PostgreSQL.

## Version 1.13.0

**Release date:** 17 February 2022

Features:

- Support for Snappy compression. Snappy is a fast compression option for backups that increase
  the speed of uploads to the object store using a lower compression ratio
- Support for tagging files uploaded to the Barman object store. This feature requires
  Barman 2.18 in the operand image.
  of backups after Cluster deletion
- Extension of the status of a Cluster with `status.conditions`. The condition `ContinuousArchiving`
  indicates that the Cluster has started to archive WAL files
- Improve the status command of the `cnp` plugin for `kubectl` with additional information:
  add a `Cluster Summary` section showing the status of the Cluster and a `Certificates Status`
  section including the status of the certificates used in the Cluster along
  with the time left to expire
- Support the new `barman-cloud-check-wal-archive` command to detect a non-empty backup destination
  when creating a new cluster
- Add support for using a `Secret` to add default monitoring queries through
  `MONITORING_QUERIES_SECRET` configuration variable.
- Allow the user to restrict container’s permissions using AppArmor (on Kubernetes clusters deployed
  with AppArmor support)
- Add Windows platform support to `cnp` plugin for `kubectl`, now the plugin is available
  on Windows x86 and ARM
- Drop support for Kubernetes 1.18 and deprecated API versions

Container Images:

- PostgreSQL containers include Barman 2.18

Security Fix:

- Add coherence check of username field inside owner and superuser secrets;
  previously, a malicious user could have used the secrets to change the password
  of any PostgreSQL user

Fixes:

- Fix a memory leak in code fetching status from Postgres pods
- Disable PostgreSQL self-restart after a crash. The instance controller handles
  the lifecycle of the PostgreSQL instance
- Prevent modification of `spec.postgresUID` and `spec.postgresGID` fields
  in validation webhook. Changing these fields after Cluster creation makes PostgreSQL unable to start
- Reduce the log verbosity from the backup and WAL archiving handling code
- Correct a bug resulting in a Cluster being marked as `Healthy` when not initialized yet
- Allows standby servers in clusters with a very high WAL production rate to switch to streaming
  once they are aligned
- Fix a race condition during the startup of a PostgreSQL pod that could seldom lead to a crash
- Fix a race condition that could lead to a failure initializing the first PVC in a Cluster
- Remove an extra restart of a just demoted primary Pod before joining the Cluster as a replica
- Correctly handle replication-sensitive PostgreSQL configuration parameters when recovering
  from a backup
- Fix missing validation of PostgreSQL configurations during Cluster creation

## Version 1.12.0

**Release date:** 11 January 2022

Features:

- Add Kubernetes 1.23 to the list of supported Kubernetes distributions and remove end-to-end tests for 1.17,  
  which ended support by the Kubernetes project in Dec 2020
- Improve the responsiveness of pod status checks in case of network issues
  by adding a connection timeout of 2 seconds and a communication timeout
  of 30 seconds. This change sets a limit on the time the operator waits for
  a pod to report its status before declaring it as failed, enhancing
  the robustness and predictability of a failover operation
- Introduce the `.spec.inheritedMetadata` field to the Cluster allowing the user
  to specify labels and annotations that will apply to all objects generated
  by the Cluster
- Reduce the number of queries executed when calculating the status
  of an instance
- Add a readiness probe for PgBouncer
- Add support for custom Certification Authority of the endpoint of Barman’s
  backup object store when using Azure protocol

Fixes:

- During a failover, wait to select a new primary until all the WAL streaming
  connections are closed. The operator now sets by default `wal_sender_timeout`
  and `wal_receiver_timeout` to 5 seconds to make sure standby nodes will
  quickly notice if the primary has network issues
- Change WAL archiving strategy in replica clusters to fix rolling updates
  by setting "archive_mode" to "always" for any PostgreSQL instance in
  a replica cluster. We then restrict the upload of the WAL only from
  the current and target designated primary. A WAL may be uploaded twice
  during switchovers, which is not an issue
- Fix support for custom Certification Authority of the endpoint of Barman’s
  backup object store in replica clusters source
- Use a fixed name for default monitoring config map in the cluster namespace
- If the defaulting webhook is not working for any reason, the operator now
  updates the Cluster with the defaults also during the reconciliation cycle
- Fix the comparison of resource requests and limits to fix a rare issue
  leading to an update of all the pods on every reconciliation cycle
- Improve log messages from webhooks to also include the object namespace
- Stop logging a “default” message at the start of every reconciliation loop
- Stop logging a PodMonitor deletion on every reconciliation cycle
  if `enablePodMonitor` is false
- Do not complain about possible architecture mismatch if a pod is not
  reachable

## Version 1.11.0

**Release date:** 15 December 2021

Features:

- **Parallel WAL archiving and restore:** allow the database to keep up with WAL
  generation on high write systems by introducing the
  `backupObjectStore.maxParallel` option to set the maximum number of parallel
  jobs to be executed during both WAL archiving (by PostgreSQL’s
  `archive_command`) and WAL restore (by `restore_command`). Using parallel
  restore option can allow newly promoted Standbys to get to a ready state faster
  by fetching needed WAL files to replay in parallel rather than sequentially
- **Default set of metrics for monitoring:** a new `ConfigMap` called
  `default-monitoring` is automatically deployed in the same namespace of the
  operator and, by default, added to any existing Postgres cluster. Such behavior
  can be changed globally by setting the `MONITORING_QUERIES_CONFIGMAP` parameter
  in the operator’s configuration, or at cluster level through the
  `.spec.monitoring.disableDefaultQueries` option (by default set to `false`)
- Introduce the `enablePodMonitor` option in the monitoring section of a
  cluster to automatically manage a `PodMonitor` resource and seamlessly
  integrate with Prometheus
- Improve the PostgreSQL shutdown procedure by trying to execute a smart
  shutdown for the first half of the desired `stopDelay` time, and a fast
  shutdown for the remaining half, before the pod is killed by Kubernetes
- Add the `switchoverDelay` option to control the time given to the former
  primary to shut down gracefully and archive all the WAL files before
  promoting the new primary (by default, Cloud Native PostgreSQL waits
  indefinitely to privilege data durability)
- Handle changes to resource requests and limits for a PostgreSQL `Cluster` by
  issuing a rolling update
- Improve the `status` command of the `cnp` plugin for `kubectl` with
  additional information: streaming replication status, total size of the
  database, role of an instance in the cluster
- Enhance support of workloads with many parallel workers by enabling
  configuration of the `dynamic_shared_memory_type` and `shared_memory_type`
  parameters for PostgreSQL’s management of shared memory
- Propagate labels and annotations defined at cluster level to the
  associated resources, including pods (deletions are not supported)
- Automatically remove pods that have been evicted by the Kubelet
- Manage automated resizing of persistent volumes in Azure through the
  `ENABLE_AZURE_PVC_UPDATES` operator configuration option, by issuing a
  rolling update of the cluster if needed (disabled by default)
- Introduce the`k8s.enterprisedb.io/reconciliationLoop` annotation that, when
  set to `disabled` on a given Postgres cluster, prevents the reconciliation
  loop from running
- Introduce the `postInitApplicationSQL` option as part of the `initdb`
  bootstrap method to specify a list of SQL queries to be executed on the main
  application database as a superuser immediately after the cluster has been
  created

Fixes:

- Liveness probe now correctly handles the startup process of a PostgreSQL
  server. This fixes an issue reported by a few customers and affects a
  restarted standby server that needs to recover WAL files to reach a consistent
  state, but it was not able to do it before the timeout of liveness probe would
  kick in, leaving the pods in `CrashLoopBackOff` status.
- Liveness probe now correctly handles the case of a former primary that needs
  to use `pg_rewind` to re-align with the current primary after a timeline
  diversion. This fixes the pod of the new standby from repeatedly being killed
  by Kubernetes.
- Reduce client-side throttling from Postgres pods (e.g. `Waited for
  1.182388649s due to client-side throttling, not priority and fairness,
  request: GET`)
- Disable Public Key Infrastructure (PKI) initialization on OpenShift and OLM
  installations, by using the provided one
- When changing configuration parameters that require a restart, always leave
  the primary as last
- Mark a PVC to be ready only after a job has been completed successfully,
  preventing a race condition in PVC initialization
- Use the correct public key when renewing the expired webhook TLS secret.
- Fix an overflow when parsing an LSN
- Remove stale PID files at startup
- Let the `Pooler` resource inherit the `imagePullSecret` defined in the
  operator, if exists

## Version 1.10.0

**Release date:** 11 November 2021

Features:

- **Connection Pooling with PgBouncer**: introduce the `Pooler` resource and
  controller to automatically manage a PgBouncer deployment to be used as a
  connection pooler for a local PostgreSQL `Cluster`. The feature includes TLS
  client/server connections, password authentication, High Availability, pod
  templates support, configuration of key PgBouncer parameters, `PAUSE`/`RESUME`,
  logging in JSON format, Prometheus exporter for stats, pools, and lists
- **Backup Retention Policies**: support definition of recovery window retention
  policies for backups (e.g. ‘30d’ to ensure a recovery window of 30 days)
- **In-Place updates of the operator**: introduce an in-place online update of the
  instance manager, which removes the need to perform a rolling update of the
  entire cluster following an update of the operator. By default this option is
  disabled (please refer to the
  [documentation for more detailed information](installation_upgrade.md#in-place-updates-of-the-instance-manager))
- Limit the list of options that can be customized in the `initdb` bootstrap
  method to `dataChecksums`, `encoding`,  `localeCollate`, `localeCType`,
  `walSegmentSize`. This makes the `options` array obsolete and planned to be
  removed in the v2 API
- Introduce the `postInitTemplateSQL` option as part of the `initdb` bootstrap
  method to specify a list of SQL queries to be executed on the `template1`
  database as a superuser immediately after the cluster has been created. This
  feature allows you to include default objects in all application databases
  created in the cluster
- New default metrics added to the instance Prometheus exporter: Postgres
  version, cluster name, and first point of recoverability according to the
  backup catalog
- Retry taking a backup after a failure
- Build awareness about Barman Cloud capabilities in order to prevent the
  operator from invoking recently introduced features (such as retention
  policies, or Azure Blob Container storage) that are not present in operand
  images that are not frequently updated
- Integrate the output of the `status` command of the `cnp` plugin with information
  about the backup
- Introduce a new annotation that reports the status of a PVC (being
  initialized or ready)
- Set the cluster name in the `k8s.enterprisedb.io/cluster` label for every
  object generated in a `Cluster`, including `Backup` objects
- Drop support for deprecated API version
  `postgresql.k8s.enterprisedb.io/v1alpha1` on the `Cluster`, `Backup`, and
  `ScheduledBackup` kinds
- Set default operand image to PostgreSQL 14.1

Security:

- Set allowPrivilegeEscalation to `false` for the operator containers
  securityContext

Fixes:

- Disable primary PodDisruptionBudget during maintenance in single-instance
  clusters
- Use the correct certificate certification authority (CA) during recovery
  operations
- Prevent Postgres connection leaking when checking WAL archiving status before
  taking a backup
- Let WAL archive/restore sleep for 100ms following transient errors that would
  flood logs otherwise

## Version 1.9.2

**Release date:** 15 October 2021

Features:

- Enhance JSON log with two new loggers: `wal-archive` for PostgreSQL's
  `archive_command`, and `wal-restore` for `restore_command` in a standby

Fixes:

- Enable WAL archiving during the standby promotion (prevented `.history` files
  from being archived)
- Pass the `--cloud-provider` option to Barman Cloud tools only when using
  Barman 2.13 or higher to avoid errors with older operands
- Wait for the pod of the primary to be ready before triggering a backup

## Version 1.9.1

**Release date:** 30 September 2021

*This release is to celebrate the launch of
[PostgreSQL 14](https://www.postgresql.org/about/news/postgresql-14-released-2318/)
by making it the default major version when a new `Cluster` is created without
defining a specific image name.*

Fixes:

- Fix issue causing `Error while getting barman endpoint CA secret` message to
  appear in the logs of the primary pod, which prevented the backup to work
  correctly
- Properly retry requesting a new backup in case of temporary communication
  issues with the instance manager

## Version 1.9.0

**Release date:** 28 September 2021

*Version 1.9.0 is not available on OpenShift due to delays with the
release process and the subsequent release of version 1.9.1.*

Features:

- Add Kubernetes 1.22 to the list of supported Kubernetes distributions, and
  remove 1.16
- Introduce support for the `--restore-target-wal` option in `pg_rewind`, in
  order to fetch WAL files from the backup archive, if necessary (available
  only with PostgreSQL 13+)
- Expose a default metric for the Prometheus exporter that estimates the number
  of pages in the `pg_catalog.pg_largeobject` table in each database
- Enhance the performance of WAL archiving and fetching, through local in-memory
  cache

Fixes:

- Explicitly set the `postgres` user when invoking `pg_isready` - required by
  restricted SCC in OpenShift
- Properly update the `FirstRecoverabilityPoint` in the status
- Set `archive_mode = always` on the designated primary if backup is requested
- Minor bug fixes

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

- Support for restricted security context constraint on Red Hat OpenShift to
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
