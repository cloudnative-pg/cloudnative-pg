# Release notes

History of user-visible changes for CloudNativePG.
For a complete list of changes, please refer to the
[commits](https://github.com/cloudnative-pg/cloudnative-pg/commits/main)
in GitHub.
For information on the community support policy for CloudNativePG, please
refer to ["Supported releases"](supported_releases.md).

##  Version 1.15.1

**Release date:** May 27, 2022 (patch release)

Minor changes:

- Enable configuration of the `archive_timeout` setting for PostgreSQL, which
  was previously a fixed parameter (by default set to 5 minutes)
- Introduce a new field called `backupOwnerReference` in the `scheduledBackup`
  resource to set the ownership reference on the created backup resources, with
  possible values being `none` (default), `self` (objects owned by the scheduled
  backup object), and `cluster` (owned by the Postgres cluster object)
- Introduce automated collection of `pg_stat_wal` metrics for PostgreSQL 14 or
  higher in the native Prometheus exporter
- Set the default operand image to PostgreSQL 14.4

Fixes:

- Fix fencing by killing orphaned processes related to `postgres`
- Enable the CSV log pipe inside the `WithActiveInstance` function to collect
  logs from recovery bootstrap jobs and help in the troubleshooting phase
- Prevent bootstrapping a new cluster with a non-empty backup object store,
  removing the risk of overwriting existing backups
- With the `recovery` bootstrap method, make sure that the recovery object
  store and the backup object store are different to avoid overwriting existing
  backups
- Re-queue the reconciliation loop if the RBAC for backups is not yet created
- Fix an issue with backups and the wrong specification of the cluster name
  property
- Ensures that operator pods always have the latest certificates in the case of
  a deployment of the operator in high availability, with more than one replica
- Fix the `cnpg report operator` command to correctly handle the case of a
  deployment of the operator in high availability, with more than one replica
- Properly propagate changes in the cluster’s `inheritedMetadata` set of labels
  and annotations to the related resources of the cluster without requiring a
  restart
- Fix the `cnpg` plugin to correctly parse any custom configmap and secret name
  defined in the operator deployment, instead of relying just on the default
  values
- Fix the local building of the documentation by using the `minidocks/mkdocs` image
  for `mkdocs`

## Version 1.15.0

**Release date:** 21 April 2022

Features:

- **Fencing:** Introduction of the fencing capability for a cluster or a given
  set of PostgreSQL instances through the `cnpg.io/fencedInstances`
  annotation, which, if not empty, disables switchover/failovers in the cluster;
  fenced instances are shut down and the pod is kept running (while considered
  not ready) for inspection and emergencies
- **LDAP authentication:** Allow LDAP Simple Bind and Search+Bind configuration
  options in the `pg_hba.conf` to be defined in the Postgres cluster spec
  declaratively, enabling the optional use of Kubernetes secrets for sensitive
  options such as `ldapbindpasswd`
- Introduction of the `primaryUpdateMethod` option, accepting the values of
  `switchover` (default) and `restart`, to be used in case of unsupervised
  `primaryUpdateStrategy`; this method controls what happens to the primary
  instance during the rolling update procedure
- New `report` command in the `kubectl cnp` plugin for better diagnosis and
  more effective troubleshooting of both the operator and a specific Postgres
  cluster
- Prune those `Backup` objects that are no longer in the backup object store
- Specification of target timeline and `LSN` in Point-In-Time Recovery
  bootstrap method
- Support for the `AWS_SESSION_TOKEN` authentication token in AWS S3 through
  the `sessionToken` option
- Default image name for PgBouncer in `Pooler` pods set to
  `quay.io/enterprisedb/pgbouncer:1.17.0`

Fixes:

- Base backup detection for Point-In-Time Recovery via `targetTime` correctly
  works now, as previously a target prior to the latest available backup was
  not possible (the detection algorithm was always wrong by selecting the last
  backup as a starting point)
- Improved resilience of hot standby sensitive parameters by relying on the
  values the operator collects from `pg_controldata`
- Intermediate certificates handling has been improved by properly discarding invalid entries,
  instead of throwing an invalid certificate error
- Prometheus exporter metric collection queries in the databases are now
  committed instead of rolled back (this might result in a change in the number
  of rolled back transactions that are visible from downstream dashboards,
  where applicable)

Version 1.15.0 is the first release of CloudNativePG. Previously, this software
was called EDB Cloud Native PostgreSQL (now EDB Postgres for Kubernetes). If you
are looking for information about a previous release, please refer to the
[EDB documentation](https://www.enterprisedb.com/docs/postgres_for_kubernetes/latest/).
