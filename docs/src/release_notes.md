# Release notes

History of user-visible changes for CloudNativePG.
For a complete list of changes, please refer to the
[commits](https://github.com/cloudnative-pg/cloudnative-pg/commits/main)
in GitHub.
For information on the community support policy for CloudNativePG, please
refer to ["Supported releases"](supported_releases.md).

##  Version 1.16.0

**Release date:** Jul 7, 2022 (minor release)

Features:

- **Offline data import and major upgrades for PostgreSQL:** introduce the
  `bootstrap.initdb.import` section to provide a way to import objects via the
  network from an existing PostgreSQL instance (even outside Kubernetes) inside a
  brand new CloudNativePG cluster using the PostgreSQL logical backup concept
  (`pg_dump`/`pg_restore`). The same method can be used to perform major
  PostgreSQL upgrades on a new cluster. The feature introduces two types of
  import: `microservice` (import one database only in the new cluster) and
  `monolith` (import the selected databases and roles from the existing
  instance).
- Anti-affinity rules for synchronous replication based on labels: make sure
  that synchronous replicas are running on nodes with different characteristics
  than the node where the primary is running, for example, availability zone

Enhancements:

- Improve fencing by removing the existing limitation that disables failover
  when one or more instances are fenced
- Enhance the automated extension management framework by checking whether an
  extension exists in the catalog instead of  running `DROP EXTENSION IF EXISTS`
  unnecessarily
- Improve logging of the instance manager during switchover and failover
- Enable redefining the name of the database of the application, its owner, and
  the related secret when recovering from an object store or cloning an
  instance via `pg_basebackup` (this was only possible in the `initdb` bootstrap
  so far)
- Backup and recovery:
    - Require Barman >= 3.0.0 for future support of PostgreSQL 15
    - Enable Azure AD Workload Identity for Barman Cloud backups through the
      `inheritFromAzureAD` option
    - Introduce `barmanObjectStore.s3Credentials.region` to define the region
      in AWS (`AWS_DEFAULT_REGION`) for both backup and recovery object stores
- Support for Kubernetes 1.24

Changes:

- Set the default operand image to PostgreSQL 14.4
- Use conditions from the Kubernetes API instead of relying on our own
  implementation for backup and WAL archiving

Fixes:

- Fix the initialization order inside the `WithActiveInstance` function that
  starts the CSV log pipe for the PostgreSQL server, ensuring proper logging in
  the cluster initialization phase - this is especially useful in bootstrap
  operations like recovery from a backup are failing (before this patch, such
  logs were not sent to the standard output channel and were permanently lost)
- Avoid an unnecessary switchover when a hot standby sensitive parameter is
  decreased, and the primary has already restarted
- Properly quote role names in `ALTER ROLE` statements
- Backup and recovery:
    - Fix the algorithm detecting the closest Barman backup for PITR, which was
      comparing the requested recovery timestamp with the backup start instead
      of the end
    - Fix Point in Time Recovery based on a transaction ID, a named restore
      point, or the “immediate” target by providing a new field called
      `backupID` in the `recoveryTarget` section
    - Fix encryption parameters invoking `barman-cloud-wal-archive` and
      `barman-cloud-backup` commands
    - Stop ignoring `barmanObjectStore.serverName` option when recovering from
      a backup object store using a server name that doesn’t match the current
      cluster name
- `cnpg` plug-in:
    - Make sure that the plug-in complies with the `-n` parameter when
      specified by the user
    - Fix the `status` command to sort results and remove variability in the
      output

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
