# Release notes

History of user-visible changes for Cloud Native PostgreSQL.

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
  and label selectors being directly managed by the  operator. Please refer to
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

