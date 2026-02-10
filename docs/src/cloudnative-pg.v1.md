---
id: cloudnative-pg.v1
sidebar_position: 550
title: API Reference
---

# API Reference

## Packages
- [postgresql.cnpg.io/v1](#postgresqlcnpgiov1)


## postgresql.cnpg.io/v1

Package v1 contains API Schema definitions for the postgresql v1 API group

### Resource Types
- [Backup](#backup)
- [Cluster](#cluster)
- [ClusterImageCatalog](#clusterimagecatalog)
- [Database](#database)
- [FailoverQuorum](#failoverquorum)
- [ImageCatalog](#imagecatalog)
- [Pooler](#pooler)
- [Publication](#publication)
- [ScheduledBackup](#scheduledbackup)
- [Subscription](#subscription)



#### AffinityConfiguration



AffinityConfiguration contains the info we need to create the
affinity rules for Pods



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `enablePodAntiAffinity` _boolean_ | Activates anti-affinity for the pods. The operator will define pods<br />anti-affinity unless this field is explicitly set to false |  |  |  |
| `topologyKey` _string_ | TopologyKey to use for anti-affinity configuration. See k8s documentation<br />for more info on that |  |  |  |
| `nodeSelector` _object (keys:string, values:string)_ | NodeSelector is map of key-value pairs used to define the nodes on which<br />the pods can run.<br />More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/ |  |  |  |
| `nodeAffinity` _[NodeAffinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#nodeaffinity-v1-core)_ | NodeAffinity describes node affinity scheduling rules for the pod.<br />More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#node-affinity |  |  |  |
| `tolerations` _[Toleration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#toleration-v1-core) array_ | Tolerations is a list of Tolerations that should be set for all the pods, in order to allow them to run<br />on tainted nodes.<br />More info: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/ |  |  |  |
| `podAntiAffinityType` _string_ | PodAntiAffinityType allows the user to decide whether pod anti-affinity between cluster instance has to be<br />considered a strong requirement during scheduling or not. Allowed values are: "preferred" (default if empty) or<br />"required". Setting it to "required", could lead to instances remaining pending until new kubernetes nodes are<br />added if all the existing nodes don't match the required pod anti-affinity rule.<br />More info:<br />https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#inter-pod-affinity-and-anti-affinity |  |  |  |
| `additionalPodAntiAffinity` _[PodAntiAffinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#podantiaffinity-v1-core)_ | AdditionalPodAntiAffinity allows to specify pod anti-affinity terms to be added to the ones generated<br />by the operator if EnablePodAntiAffinity is set to true (default) or to be used exclusively if set to false. |  |  |  |
| `additionalPodAffinity` _[PodAffinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#podaffinity-v1-core)_ | AdditionalPodAffinity allows to specify pod affinity terms to be passed to all the cluster's pods. |  |  |  |


#### AutoResizeEvent



AutoResizeEvent records a single auto-resize operation.



_Appears in:_

- [ClusterStatus](#clusterstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `timestamp` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | Timestamp is when the resize was initiated. |  |  |  |
| `instanceName` _string_ | InstanceName is the name of the instance that was resized. |  |  |  |
| `pvcName` _string_ | PVCName is the name of the PVC that was resized. |  |  |  |
| `volumeType` _[ResizeVolumeType](#resizevolumetype)_ | VolumeType is the type of volume that was resized (data/wal/tablespace). |  |  | Enum: [data wal tablespace] <br /> |
| `tablespace` _string_ | Tablespace is the tablespace name if VolumeType is tablespace. |  |  |  |
| `previousSize` _string_ | PreviousSize is the size of the PVC before the resize. |  |  |  |
| `newSize` _string_ | NewSize is the requested new size of the PVC. |  |  |  |
| `result` _[ResizeResult](#resizeresult)_ | Result is the outcome of the resize operation (success/failure). |  |  | Enum: [success failure] <br /> |


#### AvailableArchitecture



AvailableArchitecture represents the state of a cluster's architecture



_Appears in:_

- [ClusterStatus](#clusterstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `goArch` _string_ | GoArch is the name of the executable architecture | True |  |  |
| `hash` _string_ | Hash is the hash of the executable | True |  |  |




#### Backup



A Backup resource is a request for a PostgreSQL backup by the user.





| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `apiVersion` _string_ | `postgresql.cnpg.io/v1` | True | | |
| `kind` _string_ | `Backup` | True | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. | True |  |  |
| `spec` _[BackupSpec](#backupspec)_ | Specification of the desired behavior of the backup.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | True |  |  |
| `status` _[BackupStatus](#backupstatus)_ | Most recently observed status of the backup. This data may not be up to<br />date. Populated by the system. Read-only.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status |  |  |  |


#### BackupConfiguration



BackupConfiguration defines how the backup of the cluster are taken.
The supported backup methods are BarmanObjectStore and VolumeSnapshot.
For details and examples refer to the Backup and Recovery section of the
documentation



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `volumeSnapshot` _[VolumeSnapshotConfiguration](#volumesnapshotconfiguration)_ | VolumeSnapshot provides the configuration for the execution of volume snapshot backups. |  |  |  |
| `barmanObjectStore` _[BarmanObjectStoreConfiguration](https://pkg.go.dev/github.com/cloudnative-pg/barman-cloud/pkg/api#BarmanObjectStoreConfiguration)_ | The configuration for the barman-cloud tool suite |  |  |  |
| `retentionPolicy` _string_ | RetentionPolicy is the retention policy to be used for backups<br />and WALs (i.e. '60d'). The retention policy is expressed in the form<br />of `XXu` where `XX` is a positive integer and `u` is in `[dwm]` -<br />days, weeks, months.<br />It's currently only applicable when using the BarmanObjectStore method. |  |  | Pattern: `^[1-9][0-9]*[dwm]$` <br /> |
| `target` _[BackupTarget](#backuptarget)_ | The policy to decide which instance should perform backups. Available<br />options are empty string, which will default to `prefer-standby` policy,<br />`primary` to have backups run always on primary instances, `prefer-standby`<br />to have backups run preferably on the most updated standby, if available. |  | prefer-standby | Enum: [primary prefer-standby] <br /> |


#### BackupMethod

_Underlying type:_ _string_

BackupMethod defines the way of executing the physical base backups of
the selected PostgreSQL instance



_Appears in:_

- [BackupSpec](#backupspec)
- [BackupStatus](#backupstatus)
- [ClusterStatus](#clusterstatus)
- [ScheduledBackupSpec](#scheduledbackupspec)

| Field | Description |
| --- | --- |
| `volumeSnapshot` | BackupMethodVolumeSnapshot means using the volume snapshot<br />Kubernetes feature<br /> |
| `barmanObjectStore` | BackupMethodBarmanObjectStore means using barman to backup the<br />PostgreSQL cluster<br /> |
| `plugin` | BackupMethodPlugin means that this backup should be handled by<br />a plugin<br /> |


#### BackupPhase

_Underlying type:_ _string_

BackupPhase is the phase of the backup



_Appears in:_

- [BackupStatus](#backupstatus)



#### BackupPluginConfiguration



BackupPluginConfiguration contains the backup configuration used by
the backup plugin



_Appears in:_

- [BackupSpec](#backupspec)
- [ScheduledBackupSpec](#scheduledbackupspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | Name is the name of the plugin managing this backup | True |  |  |
| `parameters` _object (keys:string, values:string)_ | Parameters are the configuration parameters passed to the backup<br />plugin for this backup |  |  |  |


#### BackupSnapshotElementStatus



BackupSnapshotElementStatus is a volume snapshot that is part of a volume snapshot method backup



_Appears in:_

- [BackupSnapshotStatus](#backupsnapshotstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | Name is the snapshot resource name | True |  |  |
| `type` _string_ | Type is tho role of the snapshot in the cluster, such as PG_DATA, PG_WAL and PG_TABLESPACE | True |  |  |
| `tablespaceName` _string_ | TablespaceName is the name of the snapshotted tablespace. Only set<br />when type is PG_TABLESPACE |  |  |  |


#### BackupSnapshotStatus



BackupSnapshotStatus the fields exclusive to the volumeSnapshot method backup



_Appears in:_

- [BackupStatus](#backupstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `elements` _[BackupSnapshotElementStatus](#backupsnapshotelementstatus) array_ | The elements list, populated with the gathered volume snapshots |  |  |  |


#### BackupSource



BackupSource contains the backup we need to restore from, plus some
information that could be needed to correctly restore it.



_Appears in:_

- [BootstrapRecovery](#bootstraprecovery)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | Name of the referent. | True |  |  |
| `endpointCA` _[SecretKeySelector](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#SecretKeySelector)_ | EndpointCA store the CA bundle of the barman endpoint.<br />Useful when using self-signed certificates to avoid<br />errors with certificate issuer and barman-cloud-wal-archive. |  |  |  |


#### BackupSpec



BackupSpec defines the desired state of Backup



_Appears in:_

- [Backup](#backup)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `cluster` _[LocalObjectReference](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#LocalObjectReference)_ | The cluster to backup | True |  |  |
| `target` _[BackupTarget](#backuptarget)_ | The policy to decide which instance should perform this backup. If empty,<br />it defaults to `cluster.spec.backup.target`.<br />Available options are empty string, `primary` and `prefer-standby`.<br />`primary` to have backups run always on primary instances,<br />`prefer-standby` to have backups run preferably on the most updated<br />standby, if available. |  |  | Enum: [primary prefer-standby] <br /> |
| `method` _[BackupMethod](#backupmethod)_ | The backup method to be used, possible options are `barmanObjectStore`,<br />`volumeSnapshot` or `plugin`. Defaults to: `barmanObjectStore`. |  | barmanObjectStore | Enum: [barmanObjectStore volumeSnapshot plugin] <br /> |
| `pluginConfiguration` _[BackupPluginConfiguration](#backuppluginconfiguration)_ | Configuration parameters passed to the plugin managing this backup |  |  |  |
| `online` _boolean_ | Whether the default type of backup with volume snapshots is<br />online/hot (`true`, default) or offline/cold (`false`)<br />Overrides the default setting specified in the cluster field '.spec.backup.volumeSnapshot.online' |  |  |  |
| `onlineConfiguration` _[OnlineConfiguration](#onlineconfiguration)_ | Configuration parameters to control the online/hot backup with volume snapshots<br />Overrides the default settings specified in the cluster '.backup.volumeSnapshot.onlineConfiguration' stanza |  |  |  |


#### BackupStatus



BackupStatus defines the observed state of Backup



_Appears in:_

- [Backup](#backup)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `googleCredentials` _[GoogleCredentials](https://pkg.go.dev/github.com/cloudnative-pg/barman-cloud/pkg/api#GoogleCredentials)_ | The credentials to use to upload data to Google Cloud Storage |  |  |  |
| `s3Credentials` _[S3Credentials](https://pkg.go.dev/github.com/cloudnative-pg/barman-cloud/pkg/api#S3Credentials)_ | The credentials to use to upload data to S3 |  |  |  |
| `azureCredentials` _[AzureCredentials](https://pkg.go.dev/github.com/cloudnative-pg/barman-cloud/pkg/api#AzureCredentials)_ | The credentials to use to upload data to Azure Blob Storage |  |  |  |
| `majorVersion` _integer_ | The PostgreSQL major version that was running when the<br />backup was taken. | True |  |  |
| `endpointCA` _[SecretKeySelector](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#SecretKeySelector)_ | EndpointCA store the CA bundle of the barman endpoint.<br />Useful when using self-signed certificates to avoid<br />errors with certificate issuer and barman-cloud-wal-archive. |  |  |  |
| `endpointURL` _string_ | Endpoint to be used to upload data to the cloud,<br />overriding the automatic endpoint discovery |  |  |  |
| `destinationPath` _string_ | The path where to store the backup (i.e. s3://bucket/path/to/folder)<br />this path, with different destination folders, will be used for WALs<br />and for data. This may not be populated in case of errors. |  |  |  |
| `serverName` _string_ | The server name on S3, the cluster name is used if this<br />parameter is omitted |  |  |  |
| `encryption` _string_ | Encryption method required to S3 API |  |  |  |
| `backupId` _string_ | The ID of the Barman backup |  |  |  |
| `backupName` _string_ | The Name of the Barman backup |  |  |  |
| `phase` _[BackupPhase](#backupphase)_ | The last backup status |  |  |  |
| `startedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | When the backup execution was started by the backup tool |  |  |  |
| `stoppedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | When the backup execution was terminated by the backup tool |  |  |  |
| `reconciliationStartedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | When the backup process was started by the operator |  |  |  |
| `reconciliationTerminatedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | When the reconciliation was terminated by the operator (either successfully or not) |  |  |  |
| `beginWal` _string_ | The starting WAL |  |  |  |
| `endWal` _string_ | The ending WAL |  |  |  |
| `beginLSN` _string_ | The starting xlog |  |  |  |
| `endLSN` _string_ | The ending xlog |  |  |  |
| `error` _string_ | The detected error |  |  |  |
| `commandOutput` _string_ | Unused. Retained for compatibility with old versions. |  |  |  |
| `commandError` _string_ | The backup command output in case of error |  |  |  |
| `backupLabelFile` _integer array_ | Backup label file content as returned by Postgres in case of online (hot) backups |  |  |  |
| `tablespaceMapFile` _integer array_ | Tablespace map file content as returned by Postgres in case of online (hot) backups |  |  |  |
| `instanceID` _[InstanceID](#instanceid)_ | Information to identify the instance where the backup has been taken from |  |  |  |
| `snapshotBackupStatus` _[BackupSnapshotStatus](#backupsnapshotstatus)_ | Status of the volumeSnapshot backup |  |  |  |
| `method` _[BackupMethod](#backupmethod)_ | The backup method being used |  |  |  |
| `online` _boolean_ | Whether the backup was online/hot (`true`) or offline/cold (`false`) |  |  |  |
| `pluginMetadata` _object (keys:string, values:string)_ | A map containing the plugin metadata |  |  |  |


#### BackupTarget

_Underlying type:_ _string_

BackupTarget describes the preferred targets for a backup



_Appears in:_

- [BackupConfiguration](#backupconfiguration)
- [BackupSpec](#backupspec)
- [ScheduledBackupSpec](#scheduledbackupspec)







#### BootstrapConfiguration



BootstrapConfiguration contains information about how to create the PostgreSQL
cluster. Only a single bootstrap method can be defined among the supported
ones. `initdb` will be used as the bootstrap method if left
unspecified. Refer to the Bootstrap page of the documentation for more
information.



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `initdb` _[BootstrapInitDB](#bootstrapinitdb)_ | Bootstrap the cluster via initdb |  |  |  |
| `recovery` _[BootstrapRecovery](#bootstraprecovery)_ | Bootstrap the cluster from a backup |  |  |  |
| `pg_basebackup` _[BootstrapPgBaseBackup](#bootstrappgbasebackup)_ | Bootstrap the cluster taking a physical backup of another compatible<br />PostgreSQL instance |  |  |  |


#### BootstrapInitDB



BootstrapInitDB is the configuration of the bootstrap process when
initdb is used
Refer to the Bootstrap page of the documentation for more information.



_Appears in:_

- [BootstrapConfiguration](#bootstrapconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `database` _string_ | Name of the database used by the application. Default: `app`. |  |  |  |
| `owner` _string_ | Name of the owner of the database in the instance to be used<br />by applications. Defaults to the value of the `database` key. |  |  |  |
| `secret` _[LocalObjectReference](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#LocalObjectReference)_ | Name of the secret containing the initial credentials for the<br />owner of the user database. If empty a new secret will be<br />created from scratch |  |  |  |
| `options` _string array_ | The list of options that must be passed to initdb when creating the cluster.<br />Deprecated: This could lead to inconsistent configurations,<br />please use the explicit provided parameters instead.<br />If defined, explicit values will be ignored. |  |  |  |
| `dataChecksums` _boolean_ | Whether the `-k` option should be passed to initdb,<br />enabling checksums on data pages (default: `false`) |  |  |  |
| `encoding` _string_ | The value to be passed as option `--encoding` for initdb (default:`UTF8`) |  |  |  |
| `localeCollate` _string_ | The value to be passed as option `--lc-collate` for initdb (default:`C`) |  |  |  |
| `localeCType` _string_ | The value to be passed as option `--lc-ctype` for initdb (default:`C`) |  |  |  |
| `locale` _string_ | Sets the default collation order and character classification in the new database. |  |  |  |
| `localeProvider` _string_ | This option sets the locale provider for databases created in the new cluster.<br />Available from PostgreSQL 16. |  |  |  |
| `icuLocale` _string_ | Specifies the ICU locale when the ICU provider is used.<br />This option requires `localeProvider` to be set to `icu`.<br />Available from PostgreSQL 15. |  |  |  |
| `icuRules` _string_ | Specifies additional collation rules to customize the behavior of the default collation.<br />This option requires `localeProvider` to be set to `icu`.<br />Available from PostgreSQL 16. |  |  |  |
| `builtinLocale` _string_ | Specifies the locale name when the builtin provider is used.<br />This option requires `localeProvider` to be set to `builtin`.<br />Available from PostgreSQL 17. |  |  |  |
| `walSegmentSize` _integer_ | The value in megabytes (1 to 1024) to be passed to the `--wal-segsize`<br />option for initdb (default: empty, resulting in PostgreSQL default: 16MB) |  |  | Maximum: 1024 <br />Minimum: 1 <br /> |
| `postInitSQL` _string array_ | List of SQL queries to be executed as a superuser in the `postgres`<br />database right after the cluster has been created - to be used with extreme care<br />(by default empty) |  |  |  |
| `postInitApplicationSQL` _string array_ | List of SQL queries to be executed as a superuser in the application<br />database right after the cluster has been created - to be used with extreme care<br />(by default empty) |  |  |  |
| `postInitTemplateSQL` _string array_ | List of SQL queries to be executed as a superuser in the `template1`<br />database right after the cluster has been created - to be used with extreme care<br />(by default empty) |  |  |  |
| `import` _[Import](#import)_ | Bootstraps the new cluster by importing data from an existing PostgreSQL<br />instance using logical backup (`pg_dump` and `pg_restore`) |  |  |  |
| `postInitApplicationSQLRefs` _[SQLRefs](#sqlrefs)_ | List of references to ConfigMaps or Secrets containing SQL files<br />to be executed as a superuser in the application database right after<br />the cluster has been created. The references are processed in a specific order:<br />first, all Secrets are processed, followed by all ConfigMaps.<br />Within each group, the processing order follows the sequence specified<br />in their respective arrays.<br />(by default empty) |  |  |  |
| `postInitTemplateSQLRefs` _[SQLRefs](#sqlrefs)_ | List of references to ConfigMaps or Secrets containing SQL files<br />to be executed as a superuser in the `template1` database right after<br />the cluster has been created. The references are processed in a specific order:<br />first, all Secrets are processed, followed by all ConfigMaps.<br />Within each group, the processing order follows the sequence specified<br />in their respective arrays.<br />(by default empty) |  |  |  |
| `postInitSQLRefs` _[SQLRefs](#sqlrefs)_ | List of references to ConfigMaps or Secrets containing SQL files<br />to be executed as a superuser in the `postgres` database right after<br />the cluster has been created. The references are processed in a specific order:<br />first, all Secrets are processed, followed by all ConfigMaps.<br />Within each group, the processing order follows the sequence specified<br />in their respective arrays.<br />(by default empty) |  |  |  |


#### BootstrapPgBaseBackup



BootstrapPgBaseBackup contains the configuration required to take
a physical backup of an existing PostgreSQL cluster



_Appears in:_

- [BootstrapConfiguration](#bootstrapconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `source` _string_ | The name of the server of which we need to take a physical backup | True |  | MinLength: 1 <br /> |
| `database` _string_ | Name of the database used by the application. Default: `app`. |  |  |  |
| `owner` _string_ | Name of the owner of the database in the instance to be used<br />by applications. Defaults to the value of the `database` key. |  |  |  |
| `secret` _[LocalObjectReference](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#LocalObjectReference)_ | Name of the secret containing the initial credentials for the<br />owner of the user database. If empty a new secret will be<br />created from scratch |  |  |  |


#### BootstrapRecovery



BootstrapRecovery contains the configuration required to restore
from an existing cluster using 3 methodologies: external cluster,
volume snapshots or backup objects. Full recovery and Point-In-Time
Recovery are supported.
The method can be also be used to create clusters in continuous recovery
(replica clusters), also supporting cascading replication when `instances` >
1. Once the cluster exits recovery, the password for the superuser
will be changed through the provided secret.
Refer to the Bootstrap page of the documentation for more information.



_Appears in:_

- [BootstrapConfiguration](#bootstrapconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `backup` _[BackupSource](#backupsource)_ | The backup object containing the physical base backup from which to<br />initiate the recovery procedure.<br />Mutually exclusive with `source` and `volumeSnapshots`. |  |  |  |
| `source` _string_ | The external cluster whose backup we will restore. This is also<br />used as the name of the folder under which the backup is stored,<br />so it must be set to the name of the source cluster<br />Mutually exclusive with `backup`. |  |  |  |
| `volumeSnapshots` _[DataSource](#datasource)_ | The static PVC data source(s) from which to initiate the<br />recovery procedure. Currently supporting `VolumeSnapshot`<br />and `PersistentVolumeClaim` resources that map an existing<br />PVC group, compatible with CloudNativePG, and taken with<br />a cold backup copy on a fenced Postgres instance (limitation<br />which will be removed in the future when online backup<br />will be implemented).<br />Mutually exclusive with `backup`. |  |  |  |
| `recoveryTarget` _[RecoveryTarget](#recoverytarget)_ | By default, the recovery process applies all the available<br />WAL files in the archive (full recovery). However, you can also<br />end the recovery as soon as a consistent state is reached or<br />recover to a point-in-time (PITR) by specifying a `RecoveryTarget` object,<br />as expected by PostgreSQL (i.e., timestamp, transaction Id, LSN, ...).<br />More info: https://www.postgresql.org/docs/current/runtime-config-wal.html#RUNTIME-CONFIG-WAL-RECOVERY-TARGET |  |  |  |
| `database` _string_ | Name of the database used by the application. Default: `app`. |  |  |  |
| `owner` _string_ | Name of the owner of the database in the instance to be used<br />by applications. Defaults to the value of the `database` key. |  |  |  |
| `secret` _[LocalObjectReference](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#LocalObjectReference)_ | Name of the secret containing the initial credentials for the<br />owner of the user database. If empty a new secret will be<br />created from scratch |  |  |  |


#### CatalogImage



CatalogImage defines the image and major version



_Appears in:_

- [ImageCatalogSpec](#imagecatalogspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `image` _string_ | The image reference | True |  |  |
| `major` _integer_ | The PostgreSQL major version of the image. Must be unique within the catalog. | True |  | Minimum: 10 <br /> |


#### CertificatesConfiguration



CertificatesConfiguration contains the needed configurations to handle server certificates.



_Appears in:_

- [CertificatesStatus](#certificatesstatus)
- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `serverCASecret` _string_ | The secret containing the Server CA certificate. If not defined, a new secret will be created<br />with a self-signed CA and will be used to generate the TLS certificate ServerTLSSecret.<br /><br />Contains:<br /><br />- `ca.crt`: CA that should be used to validate the server certificate,<br />used as `sslrootcert` in client connection strings.<br />- `ca.key`: key used to generate Server SSL certs, if ServerTLSSecret is provided,<br />this can be omitted.<br /> |  |  |  |
| `serverTLSSecret` _string_ | The secret of type kubernetes.io/tls containing the server TLS certificate and key that will be set as<br />`ssl_cert_file` and `ssl_key_file` so that clients can connect to postgres securely.<br />If not defined, ServerCASecret must provide also `ca.key` and a new secret will be<br />created using the provided CA. |  |  |  |
| `replicationTLSSecret` _string_ | The secret of type kubernetes.io/tls containing the client certificate to authenticate as<br />the `streaming_replica` user.<br />If not defined, ClientCASecret must provide also `ca.key`, and a new secret will be<br />created using the provided CA. |  |  |  |
| `clientCASecret` _string_ | The secret containing the Client CA certificate. If not defined, a new secret will be created<br />with a self-signed CA and will be used to generate all the client certificates.<br /><br />Contains:<br /><br />- `ca.crt`: CA that should be used to validate the client certificates,<br />used as `ssl_ca_file` of all the instances.<br />- `ca.key`: key used to generate client certificates, if ReplicationTLSSecret is provided,<br />this can be omitted.<br /> |  |  |  |
| `serverAltDNSNames` _string array_ | The list of the server alternative DNS names to be added to the generated server TLS certificates, when required. |  |  |  |


#### CertificatesStatus



CertificatesStatus contains configuration certificates and related expiration dates.



_Appears in:_

- [ClusterStatus](#clusterstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `serverCASecret` _string_ | The secret containing the Server CA certificate. If not defined, a new secret will be created<br />with a self-signed CA and will be used to generate the TLS certificate ServerTLSSecret.<br /><br />Contains:<br /><br />- `ca.crt`: CA that should be used to validate the server certificate,<br />used as `sslrootcert` in client connection strings.<br />- `ca.key`: key used to generate Server SSL certs, if ServerTLSSecret is provided,<br />this can be omitted.<br /> |  |  |  |
| `serverTLSSecret` _string_ | The secret of type kubernetes.io/tls containing the server TLS certificate and key that will be set as<br />`ssl_cert_file` and `ssl_key_file` so that clients can connect to postgres securely.<br />If not defined, ServerCASecret must provide also `ca.key` and a new secret will be<br />created using the provided CA. |  |  |  |
| `replicationTLSSecret` _string_ | The secret of type kubernetes.io/tls containing the client certificate to authenticate as<br />the `streaming_replica` user.<br />If not defined, ClientCASecret must provide also `ca.key`, and a new secret will be<br />created using the provided CA. |  |  |  |
| `clientCASecret` _string_ | The secret containing the Client CA certificate. If not defined, a new secret will be created<br />with a self-signed CA and will be used to generate all the client certificates.<br /><br />Contains:<br /><br />- `ca.crt`: CA that should be used to validate the client certificates,<br />used as `ssl_ca_file` of all the instances.<br />- `ca.key`: key used to generate client certificates, if ReplicationTLSSecret is provided,<br />this can be omitted.<br /> |  |  |  |
| `serverAltDNSNames` _string array_ | The list of the server alternative DNS names to be added to the generated server TLS certificates, when required. |  |  |  |
| `expirations` _object (keys:string, values:string)_ | Expiration dates for all certificates. |  |  |  |


#### Cluster



Cluster defines the API schema for a highly available PostgreSQL database cluster
managed by CloudNativePG.





| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `apiVersion` _string_ | `postgresql.cnpg.io/v1` | True | | |
| `kind` _string_ | `Cluster` | True | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. | True |  |  |
| `spec` _[ClusterSpec](#clusterspec)_ | Specification of the desired behavior of the cluster.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | True |  |  |
| `status` _[ClusterStatus](#clusterstatus)_ | Most recently observed status of the cluster. This data may not be up<br />to date. Populated by the system. Read-only.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status |  |  |  |




#### ClusterDiskStatus



ClusterDiskStatus contains disk usage status for all instances.



_Appears in:_

- [ClusterStatus](#clusterstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `instances` _object (keys:string, values:[InstanceDiskStatus](#instancediskstatus))_ | Instances contains the disk status for each instance. |  |  |  |


#### ClusterImageCatalog



ClusterImageCatalog is the Schema for the clusterimagecatalogs API





| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `apiVersion` _string_ | `postgresql.cnpg.io/v1` | True | | |
| `kind` _string_ | `ClusterImageCatalog` | True | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. | True |  |  |
| `spec` _[ImageCatalogSpec](#imagecatalogspec)_ | Specification of the desired behavior of the ClusterImageCatalog.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | True |  |  |


#### ClusterMonitoringTLSConfiguration



ClusterMonitoringTLSConfiguration is the type containing the TLS configuration
for the cluster's monitoring



_Appears in:_

- [MonitoringConfiguration](#monitoringconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `enabled` _boolean_ | Enable TLS for the monitoring endpoint.<br />Changing this option will force a rollout of all instances. |  | false |  |


#### ClusterSpec



ClusterSpec defines the desired state of a PostgreSQL cluster managed by
CloudNativePG.



_Appears in:_

- [Cluster](#cluster)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `description` _string_ | Description of this PostgreSQL cluster |  |  |  |
| `inheritedMetadata` _[EmbeddedObjectMetadata](#embeddedobjectmetadata)_ | Metadata that will be inherited by all objects related to the Cluster |  |  |  |
| `imageName` _string_ | Name of the container image, supporting both tags (`<image>:<tag>`)<br />and digests for deterministic and repeatable deployments<br />(`<image>:<tag>@sha256:<digestValue>`) |  |  |  |
| `imageCatalogRef` _[ImageCatalogRef](#imagecatalogref)_ | Defines the major PostgreSQL version we want to use within an ImageCatalog |  |  |  |
| `imagePullPolicy` _[PullPolicy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#pullpolicy-v1-core)_ | Image pull policy.<br />One of `Always`, `Never` or `IfNotPresent`.<br />If not defined, it defaults to `IfNotPresent`.<br />Cannot be updated.<br />More info: https://kubernetes.io/docs/concepts/containers/images#updating-images |  |  |  |
| `schedulerName` _string_ | If specified, the pod will be dispatched by specified Kubernetes<br />scheduler. If not specified, the pod will be dispatched by the default<br />scheduler. More info:<br />https://kubernetes.io/docs/concepts/scheduling-eviction/kube-scheduler/ |  |  |  |
| `postgresUID` _integer_ | The UID of the `postgres` user inside the image, defaults to `26` |  | 26 |  |
| `postgresGID` _integer_ | The GID of the `postgres` user inside the image, defaults to `26` |  | 26 |  |
| `instances` _integer_ | Number of instances required in the cluster | True | 1 | Minimum: 1 <br /> |
| `minSyncReplicas` _integer_ | Minimum number of instances required in synchronous replication with the<br />primary. Undefined or 0 allow writes to complete when no standby is<br />available. |  | 0 | Minimum: 0 <br /> |
| `maxSyncReplicas` _integer_ | The target value for the synchronous replication quorum, that can be<br />decreased if the number of ready standbys is lower than this.<br />Undefined or 0 disable synchronous replication. |  | 0 | Minimum: 0 <br /> |
| `postgresql` _[PostgresConfiguration](#postgresconfiguration)_ | Configuration of the PostgreSQL server |  |  |  |
| `replicationSlots` _[ReplicationSlotsConfiguration](#replicationslotsconfiguration)_ | Replication slots management configuration |  | \{ highAvailability\: \{ enabled:true \} \} |  |
| `bootstrap` _[BootstrapConfiguration](#bootstrapconfiguration)_ | Instructions to bootstrap this cluster |  |  |  |
| `replica` _[ReplicaClusterConfiguration](#replicaclusterconfiguration)_ | Replica cluster configuration |  |  |  |
| `superuserSecret` _[LocalObjectReference](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#LocalObjectReference)_ | The secret containing the superuser password. If not defined a new<br />secret will be created with a randomly generated password |  |  |  |
| `enableSuperuserAccess` _boolean_ | When this option is enabled, the operator will use the `SuperuserSecret`<br />to update the `postgres` user password (if the secret is<br />not present, the operator will automatically create one). When this<br />option is disabled, the operator will ignore the `SuperuserSecret` content, delete<br />it when automatically created, and then blank the password of the `postgres`<br />user by setting it to `NULL`. Disabled by default. |  | false |  |
| `certificates` _[CertificatesConfiguration](#certificatesconfiguration)_ | The configuration for the CA and related certificates |  |  |  |
| `imagePullSecrets` _[LocalObjectReference](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#LocalObjectReference) array_ | The list of pull secrets to be used to pull the images |  |  |  |
| `storage` _[StorageConfiguration](#storageconfiguration)_ | Configuration of the storage of the instances |  |  |  |
| `serviceAccountTemplate` _[ServiceAccountTemplate](#serviceaccounttemplate)_ | Configure the generation of the service account |  |  |  |
| `walStorage` _[StorageConfiguration](#storageconfiguration)_ | Configuration of the storage for PostgreSQL WAL (Write-Ahead Log) |  |  |  |
| `ephemeralVolumeSource` _[EphemeralVolumeSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#ephemeralvolumesource-v1-core)_ | EphemeralVolumeSource allows the user to configure the source of ephemeral volumes. |  |  |  |
| `startDelay` _integer_ | The time in seconds that is allowed for a PostgreSQL instance to<br />successfully start up (default 3600).<br />The startup probe failure threshold is derived from this value using the formula:<br />ceiling(startDelay / 10). |  | 3600 |  |
| `stopDelay` _integer_ | The time in seconds that is allowed for a PostgreSQL instance to<br />gracefully shutdown (default 1800) |  | 1800 |  |
| `smartShutdownTimeout` _integer_ | The time in seconds that controls the window of time reserved for the smart shutdown of Postgres to complete.<br />Make sure you reserve enough time for the operator to request a fast shutdown of Postgres<br />(that is: `stopDelay` - `smartShutdownTimeout`). Default is 180 seconds. |  | 180 |  |
| `switchoverDelay` _integer_ | The time in seconds that is allowed for a primary PostgreSQL instance<br />to gracefully shutdown during a switchover.<br />Default value is 3600 seconds (1 hour). |  | 3600 |  |
| `failoverDelay` _integer_ | The amount of time (in seconds) to wait before triggering a failover<br />after the primary PostgreSQL instance in the cluster was detected<br />to be unhealthy |  | 0 |  |
| `livenessProbeTimeout` _integer_ | LivenessProbeTimeout is the time (in seconds) that is allowed for a PostgreSQL instance<br />to successfully respond to the liveness probe (default 30).<br />The Liveness probe failure threshold is derived from this value using the formula:<br />ceiling(livenessProbe / 10). |  |  |  |
| `affinity` _[AffinityConfiguration](#affinityconfiguration)_ | Affinity/Anti-affinity rules for Pods |  |  |  |
| `topologySpreadConstraints` _[TopologySpreadConstraint](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#topologyspreadconstraint-v1-core) array_ | TopologySpreadConstraints specifies how to spread matching pods among the given topology.<br />More info:<br />https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/ |  |  |  |
| `resources` _[ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcerequirements-v1-core)_ | Resources requirements of every generated Pod. Please refer to<br />https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/<br />for more information. |  |  |  |
| `ephemeralVolumesSizeLimit` _[EphemeralVolumesSizeLimitConfiguration](#ephemeralvolumessizelimitconfiguration)_ | EphemeralVolumesSizeLimit allows the user to set the limits for the ephemeral<br />volumes |  |  |  |
| `priorityClassName` _string_ | Name of the priority class which will be used in every generated Pod, if the PriorityClass<br />specified does not exist, the pod will not be able to schedule.  Please refer to<br />https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/#priorityclass<br />for more information |  |  |  |
| `primaryUpdateStrategy` _[PrimaryUpdateStrategy](#primaryupdatestrategy)_ | Deployment strategy to follow to upgrade the primary server during a rolling<br />update procedure, after all replicas have been successfully updated:<br />it can be automated (`unsupervised` - default) or manual (`supervised`) |  | unsupervised | Enum: [unsupervised supervised] <br /> |
| `primaryUpdateMethod` _[PrimaryUpdateMethod](#primaryupdatemethod)_ | Method to follow to upgrade the primary server during a rolling<br />update procedure, after all replicas have been successfully updated:<br />it can be with a switchover (`switchover`) or in-place (`restart` - default).<br />Note: when using `switchover`, the operator will reject updates that change both<br />the image name and PostgreSQL configuration parameters simultaneously to avoid<br />configuration mismatches during the switchover process. |  | restart | Enum: [switchover restart] <br /> |
| `backup` _[BackupConfiguration](#backupconfiguration)_ | The configuration to be used for backups |  |  |  |
| `nodeMaintenanceWindow` _[NodeMaintenanceWindow](#nodemaintenancewindow)_ | Define a maintenance window for the Kubernetes nodes |  |  |  |
| `monitoring` _[MonitoringConfiguration](#monitoringconfiguration)_ | The configuration of the monitoring infrastructure of this cluster |  |  |  |
| `externalClusters` _[ExternalCluster](#externalcluster) array_ | The list of external clusters which are used in the configuration |  |  |  |
| `logLevel` _string_ | The instances' log level, one of the following values: error, warning, info (default), debug, trace |  | info | Enum: [error warning info debug trace] <br /> |
| `projectedVolumeTemplate` _[ProjectedVolumeSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#projectedvolumesource-v1-core)_ | Template to be used to define projected volumes, projected volumes will be mounted<br />under `/projected` base folder |  |  |  |
| `env` _[EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#envvar-v1-core) array_ | Env follows the Env format to pass environment variables<br />to the pods created in the cluster |  |  |  |
| `envFrom` _[EnvFromSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#envfromsource-v1-core) array_ | EnvFrom follows the EnvFrom format to pass environment variables<br />sources to the pods to be used by Env |  |  |  |
| `managed` _[ManagedConfiguration](#managedconfiguration)_ | The configuration that is used by the portions of PostgreSQL that are managed by the instance manager |  |  |  |
| `seccompProfile` _[SeccompProfile](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#seccompprofile-v1-core)_ | The SeccompProfile applied to every Pod and Container.<br />Defaults to: `RuntimeDefault` |  |  |  |
| `podSecurityContext` _[PodSecurityContext](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#podsecuritycontext-v1-core)_ | Override the PodSecurityContext applied to every Pod of the cluster.<br />When set, this overrides the operator's default PodSecurityContext for the cluster.<br />If omitted, the operator defaults are used.<br />This field doesn't have any effect if SecurityContextConstraints are present. |  |  |  |
| `securityContext` _[SecurityContext](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#securitycontext-v1-core)_ | Override the SecurityContext applied to every Container in the Pod of the cluster.<br />When set, this overrides the operator's default Container SecurityContext.<br />If omitted, the operator defaults are used. |  |  |  |
| `tablespaces` _[TablespaceConfiguration](#tablespaceconfiguration) array_ | The tablespaces configuration |  |  |  |
| `enablePDB` _boolean_ | Manage the `PodDisruptionBudget` resources within the cluster. When<br />configured as `true` (default setting), the pod disruption budgets<br />will safeguard the primary node from being terminated. Conversely,<br />setting it to `false` will result in the absence of any<br />`PodDisruptionBudget` resource, permitting the shutdown of all nodes<br />hosting the PostgreSQL cluster. This latter configuration is<br />advisable for any PostgreSQL cluster employed for<br />development/staging purposes. |  | true |  |
| `plugins` _[PluginConfiguration](#pluginconfiguration) array_ | The plugins configuration, containing<br />any plugin to be loaded with the corresponding configuration |  |  |  |
| `probes` _[ProbesConfiguration](#probesconfiguration)_ | The configuration of the probes to be injected<br />in the PostgreSQL Pods. |  |  |  |


#### ClusterStatus



ClusterStatus defines the observed state of a PostgreSQL cluster managed by
CloudNativePG.



_Appears in:_

- [Cluster](#cluster)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `instances` _integer_ | The total number of PVC Groups detected in the cluster. It may differ from the number of existing instance pods. |  |  |  |
| `readyInstances` _integer_ | The total number of ready instances in the cluster. It is equal to the number of ready instance pods. |  |  |  |
| `instancesStatus` _object (keys:[PodStatus](#podstatus), values:string array)_ | InstancesStatus indicates in which status the instances are |  |  |  |
| `instancesReportedState` _object (keys:[PodName](#podname), values:[InstanceReportedState](#instancereportedstate))_ | The reported state of the instances during the last reconciliation loop |  |  |  |
| `managedRolesStatus` _[ManagedRoles](#managedroles)_ | ManagedRolesStatus reports the state of the managed roles in the cluster |  |  |  |
| `tablespacesStatus` _[TablespaceState](#tablespacestate) array_ | TablespacesStatus reports the state of the declarative tablespaces in the cluster |  |  |  |
| `timelineID` _integer_ | The timeline of the Postgres cluster |  |  |  |
| `topology` _[Topology](#topology)_ | Instances topology. |  |  |  |
| `latestGeneratedNode` _integer_ | ID of the latest generated node (used to avoid node name clashing) |  |  |  |
| `currentPrimary` _string_ | Current primary instance |  |  |  |
| `targetPrimary` _string_ | Target primary instance, this is different from the previous one<br />during a switchover or a failover |  |  |  |
| `lastPromotionToken` _string_ | LastPromotionToken is the last verified promotion token that<br />was used to promote a replica cluster |  |  |  |
| `pvcCount` _integer_ | How many PVCs have been created by this cluster |  |  |  |
| `jobCount` _integer_ | How many Jobs have been created by this cluster |  |  |  |
| `danglingPVC` _string array_ | List of all the PVCs created by this cluster and still available<br />which are not attached to a Pod |  |  |  |
| `resizingPVC` _string array_ | List of all the PVCs that have ResizingPVC condition. |  |  |  |
| `initializingPVC` _string array_ | List of all the PVCs that are being initialized by this cluster |  |  |  |
| `healthyPVC` _string array_ | List of all the PVCs not dangling nor initializing |  |  |  |
| `unusablePVC` _string array_ | List of all the PVCs that are unusable because another PVC is missing |  |  |  |
| `writeService` _string_ | Current write pod |  |  |  |
| `readService` _string_ | Current list of read pods |  |  |  |
| `phase` _string_ | Current phase of the cluster |  |  |  |
| `phaseReason` _string_ | Reason for the current phase |  |  |  |
| `secretsResourceVersion` _[SecretsResourceVersion](#secretsresourceversion)_ | The list of resource versions of the secrets<br />managed by the operator. Every change here is done in the<br />interest of the instance manager, which will refresh the<br />secret data |  |  |  |
| `configMapResourceVersion` _[ConfigMapResourceVersion](#configmapresourceversion)_ | The list of resource versions of the configmaps,<br />managed by the operator. Every change here is done in the<br />interest of the instance manager, which will refresh the<br />configmap data |  |  |  |
| `certificates` _[CertificatesStatus](#certificatesstatus)_ | The configuration for the CA and related certificates, initialized with defaults. |  |  |  |
| `firstRecoverabilityPoint` _string_ | The first recoverability point, stored as a date in RFC3339 format.<br />This field is calculated from the content of FirstRecoverabilityPointByMethod.<br />Deprecated: the field is not set for backup plugins. |  |  |  |
| `firstRecoverabilityPointByMethod` _object (keys:[BackupMethod](#backupmethod), values:[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta))_ | The first recoverability point, stored as a date in RFC3339 format, per backup method type.<br />Deprecated: the field is not set for backup plugins. |  |  |  |
| `lastSuccessfulBackup` _string_ | Last successful backup, stored as a date in RFC3339 format.<br />This field is calculated from the content of LastSuccessfulBackupByMethod.<br />Deprecated: the field is not set for backup plugins. |  |  |  |
| `lastSuccessfulBackupByMethod` _object (keys:[BackupMethod](#backupmethod), values:[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta))_ | Last successful backup, stored as a date in RFC3339 format, per backup method type.<br />Deprecated: the field is not set for backup plugins. |  |  |  |
| `lastFailedBackup` _string_ | Last failed backup, stored as a date in RFC3339 format.<br />Deprecated: the field is not set for backup plugins. |  |  |  |
| `cloudNativePGCommitHash` _string_ | The commit hash number of which this operator running |  |  |  |
| `currentPrimaryTimestamp` _string_ | The timestamp when the last actual promotion to primary has occurred |  |  |  |
| `currentPrimaryFailingSinceTimestamp` _string_ | The timestamp when the primary was detected to be unhealthy<br />This field is reported when `.spec.failoverDelay` is populated or during online upgrades |  |  |  |
| `targetPrimaryTimestamp` _string_ | The timestamp when the last request for a new primary has occurred |  |  |  |
| `poolerIntegrations` _[PoolerIntegrations](#poolerintegrations)_ | The integration needed by poolers referencing the cluster |  |  |  |
| `cloudNativePGOperatorHash` _string_ | The hash of the binary of the operator |  |  |  |
| `availableArchitectures` _[AvailableArchitecture](#availablearchitecture) array_ | AvailableArchitectures reports the available architectures of a cluster |  |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions for cluster object |  |  |  |
| `instanceNames` _string array_ | List of instance names in the cluster |  |  |  |
| `onlineUpdateEnabled` _boolean_ | OnlineUpdateEnabled shows if the online upgrade is enabled inside the cluster |  |  |  |
| `image` _string_ | Image contains the image name used by the pods |  |  |  |
| `pgDataImageInfo` _[ImageInfo](#imageinfo)_ | PGDataImageInfo contains the details of the latest image that has run on the current data directory. |  |  |  |
| `pluginStatus` _[PluginStatus](#pluginstatus) array_ | PluginStatus is the status of the loaded plugins |  |  |  |
| `switchReplicaClusterStatus` _[SwitchReplicaClusterStatus](#switchreplicaclusterstatus)_ | SwitchReplicaClusterStatus is the status of the switch to replica cluster |  |  |  |
| `demotionToken` _string_ | DemotionToken is a JSON token containing the information<br />from pg_controldata such as Database system identifier, Latest checkpoint's<br />TimeLineID, Latest checkpoint's REDO location, Latest checkpoint's REDO<br />WAL file, and Time of latest checkpoint |  |  |  |
| `systemID` _string_ | SystemID is the latest detected PostgreSQL SystemID |  |  |  |
| `diskStatus` _[ClusterDiskStatus](#clusterdiskstatus)_ | DiskStatus contains the disk usage status for all instances. |  |  |  |
| `autoResizeEvents` _[AutoResizeEvent](#autoresizeevent) array_ | AutoResizeEvents contains the history of auto-resize operations. |  |  |  |








#### ConfigMapResourceVersion



ConfigMapResourceVersion is the resource versions of the secrets
managed by the operator



_Appears in:_

- [ClusterStatus](#clusterstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `metrics` _object (keys:string, values:string)_ | A map with the versions of all the config maps used to pass metrics.<br />Map keys are the config map names, map values are the versions |  |  |  |




#### DataDurabilityLevel

_Underlying type:_ _string_

DataDurabilityLevel specifies how strictly to enforce synchronous replication
when cluster instances are unavailable. Options are `required` or `preferred`.



_Appears in:_

- [SynchronousReplicaConfiguration](#synchronousreplicaconfiguration)

| Field | Description |
| --- | --- |
| `required` | DataDurabilityLevelRequired means that data durability is strictly enforced<br /> |
| `preferred` | DataDurabilityLevelPreferred means that data durability is enforced<br />only when healthy replicas are available<br /> |


#### DataSource



DataSource contains the configuration required to bootstrap a
PostgreSQL cluster from an existing storage



_Appears in:_

- [BootstrapRecovery](#bootstraprecovery)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `storage` _[TypedLocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#typedlocalobjectreference-v1-core)_ | Configuration of the storage of the instances | True |  |  |
| `walStorage` _[TypedLocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#typedlocalobjectreference-v1-core)_ | Configuration of the storage for PostgreSQL WAL (Write-Ahead Log) |  |  |  |
| `tablespaceStorage` _object (keys:string, values:[TypedLocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#typedlocalobjectreference-v1-core))_ | Configuration of the storage for PostgreSQL tablespaces |  |  |  |


#### Database



Database is the Schema for the databases API





| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `apiVersion` _string_ | `postgresql.cnpg.io/v1` | True | | |
| `kind` _string_ | `Database` | True | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. | True |  |  |
| `spec` _[DatabaseSpec](#databasespec)_ | Specification of the desired Database.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | True |  |  |
| `status` _[DatabaseStatus](#databasestatus)_ | Most recently observed status of the Database. This data may not be up to<br />date. Populated by the system. Read-only.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status |  |  |  |


#### DatabaseObjectSpec



DatabaseObjectSpec contains the fields which are common to every
database object



_Appears in:_

- [ExtensionSpec](#extensionspec)
- [FDWSpec](#fdwspec)
- [SchemaSpec](#schemaspec)
- [ServerSpec](#serverspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | Name of the object (extension, schema, FDW, server) | True |  |  |
| `ensure` _[EnsureOption](#ensureoption)_ | Specifies whether an object (e.g schema) should be present or absent<br />in the database. If set to `present`, the object will be created if<br />it does not exist. If set to `absent`, the extension/schema will be<br />removed if it exists. |  | present | Enum: [present absent] <br /> |


#### DatabaseObjectStatus



DatabaseObjectStatus is the status of the managed database objects



_Appears in:_

- [DatabaseStatus](#databasestatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | The name of the object | True |  |  |
| `applied` _boolean_ | True of the object has been installed successfully in<br />the database | True |  |  |
| `message` _string_ | Message is the object reconciliation message |  |  |  |


#### DatabaseReclaimPolicy

_Underlying type:_ _string_

DatabaseReclaimPolicy describes a policy for end-of-life maintenance of databases.



_Appears in:_

- [DatabaseSpec](#databasespec)

| Field | Description |
| --- | --- |
| `delete` | DatabaseReclaimDelete means the database will be deleted from its PostgreSQL Cluster on release<br />from its claim.<br /> |
| `retain` | DatabaseReclaimRetain means the database will be left in its current phase for manual<br />reclamation by the administrator. The default policy is Retain.<br /> |


#### DatabaseRoleRef



DatabaseRoleRef is a reference an a role available inside PostgreSQL



_Appears in:_

- [TablespaceConfiguration](#tablespaceconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ |  |  |  |  |


#### DatabaseSpec



DatabaseSpec is the specification of a Postgresql Database, built around the
`CREATE DATABASE`, `ALTER DATABASE`, and `DROP DATABASE` SQL commands of
PostgreSQL.



_Appears in:_

- [Database](#database)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `cluster` _[LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#localobjectreference-v1-core)_ | The name of the PostgreSQL cluster hosting the database. | True |  |  |
| `ensure` _[EnsureOption](#ensureoption)_ | Ensure the PostgreSQL database is `present` or `absent` - defaults to "present". |  | present | Enum: [present absent] <br /> |
| `name` _string_ | The name of the database to create inside PostgreSQL. This setting cannot be changed. | True |  |  |
| `owner` _string_ | Maps to the `OWNER` parameter of `CREATE DATABASE`.<br />Maps to the `OWNER TO` command of `ALTER DATABASE`.<br />The role name of the user who owns the database inside PostgreSQL. | True |  |  |
| `template` _string_ | Maps to the `TEMPLATE` parameter of `CREATE DATABASE`. This setting<br />cannot be changed. The name of the template from which to create<br />this database. |  |  |  |
| `encoding` _string_ | Maps to the `ENCODING` parameter of `CREATE DATABASE`. This setting<br />cannot be changed. Character set encoding to use in the database. |  |  |  |
| `locale` _string_ | Maps to the `LOCALE` parameter of `CREATE DATABASE`. This setting<br />cannot be changed. Sets the default collation order and character<br />classification in the new database. |  |  |  |
| `localeProvider` _string_ | Maps to the `LOCALE_PROVIDER` parameter of `CREATE DATABASE`. This<br />setting cannot be changed. This option sets the locale provider for<br />databases created in the new cluster. Available from PostgreSQL 16. |  |  |  |
| `localeCollate` _string_ | Maps to the `LC_COLLATE` parameter of `CREATE DATABASE`. This<br />setting cannot be changed. |  |  |  |
| `localeCType` _string_ | Maps to the `LC_CTYPE` parameter of `CREATE DATABASE`. This setting<br />cannot be changed. |  |  |  |
| `icuLocale` _string_ | Maps to the `ICU_LOCALE` parameter of `CREATE DATABASE`. This<br />setting cannot be changed. Specifies the ICU locale when the ICU<br />provider is used. This option requires `localeProvider` to be set to<br />`icu`. Available from PostgreSQL 15. |  |  |  |
| `icuRules` _string_ | Maps to the `ICU_RULES` parameter of `CREATE DATABASE`. This setting<br />cannot be changed. Specifies additional collation rules to customize<br />the behavior of the default collation. This option requires<br />`localeProvider` to be set to `icu`. Available from PostgreSQL 16. |  |  |  |
| `builtinLocale` _string_ | Maps to the `BUILTIN_LOCALE` parameter of `CREATE DATABASE`. This<br />setting cannot be changed. Specifies the locale name when the<br />builtin provider is used. This option requires `localeProvider` to<br />be set to `builtin`. Available from PostgreSQL 17. |  |  |  |
| `collationVersion` _string_ | Maps to the `COLLATION_VERSION` parameter of `CREATE DATABASE`. This<br />setting cannot be changed. |  |  |  |
| `isTemplate` _boolean_ | Maps to the `IS_TEMPLATE` parameter of `CREATE DATABASE` and `ALTER<br />DATABASE`. If true, this database is considered a template and can<br />be cloned by any user with `CREATEDB` privileges. |  |  |  |
| `allowConnections` _boolean_ | Maps to the `ALLOW_CONNECTIONS` parameter of `CREATE DATABASE` and<br />`ALTER DATABASE`. If false then no one can connect to this database. |  |  |  |
| `connectionLimit` _integer_ | Maps to the `CONNECTION LIMIT` clause of `CREATE DATABASE` and<br />`ALTER DATABASE`. How many concurrent connections can be made to<br />this database. -1 (the default) means no limit. |  |  |  |
| `tablespace` _string_ | Maps to the `TABLESPACE` parameter of `CREATE DATABASE`.<br />Maps to the `SET TABLESPACE` command of `ALTER DATABASE`.<br />The name of the tablespace (in PostgreSQL) that will be associated<br />with the new database. This tablespace will be the default<br />tablespace used for objects created in this database. |  |  |  |
| `databaseReclaimPolicy` _[DatabaseReclaimPolicy](#databasereclaimpolicy)_ | The policy for end-of-life maintenance of this database. |  | retain | Enum: [delete retain] <br /> |
| `schemas` _[SchemaSpec](#schemaspec) array_ | The list of schemas to be managed in the database |  |  |  |
| `extensions` _[ExtensionSpec](#extensionspec) array_ | The list of extensions to be managed in the database |  |  |  |
| `fdws` _[FDWSpec](#fdwspec) array_ | The list of foreign data wrappers to be managed in the database |  |  |  |
| `servers` _[ServerSpec](#serverspec) array_ | The list of foreign servers to be managed in the database |  |  |  |


#### DatabaseStatus



DatabaseStatus defines the observed state of Database



_Appears in:_

- [Database](#database)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `observedGeneration` _integer_ | A sequence number representing the latest<br />desired state that was synchronized |  |  |  |
| `applied` _boolean_ | Applied is true if the database was reconciled correctly |  |  |  |
| `message` _string_ | Message is the reconciliation output message |  |  |  |
| `schemas` _[DatabaseObjectStatus](#databaseobjectstatus) array_ | Schemas is the status of the managed schemas |  |  |  |
| `extensions` _[DatabaseObjectStatus](#databaseobjectstatus) array_ | Extensions is the status of the managed extensions |  |  |  |
| `fdws` _[DatabaseObjectStatus](#databaseobjectstatus) array_ | FDWs is the status of the managed FDWs |  |  |  |
| `servers` _[DatabaseObjectStatus](#databaseobjectstatus) array_ | Servers is the status of the managed servers |  |  |  |


#### EmbeddedObjectMetadata



EmbeddedObjectMetadata contains metadata to be inherited by all resources related to a Cluster



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `labels` _object (keys:string, values:string)_ |  |  |  |  |
| `annotations` _object (keys:string, values:string)_ |  |  |  |  |


#### EnsureOption

_Underlying type:_ _string_

EnsureOption represents whether we should enforce the presence or absence of
a Role in a PostgreSQL instance



_Appears in:_

- [DatabaseObjectSpec](#databaseobjectspec)
- [DatabaseSpec](#databasespec)
- [ExtensionSpec](#extensionspec)
- [FDWSpec](#fdwspec)
- [OptionSpec](#optionspec)
- [RoleConfiguration](#roleconfiguration)
- [SchemaSpec](#schemaspec)
- [ServerSpec](#serverspec)

| Field | Description |
| --- | --- |
| `present` |  |
| `absent` |  |


#### EphemeralVolumesSizeLimitConfiguration



EphemeralVolumesSizeLimitConfiguration contains the configuration of the ephemeral
storage



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `shm` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#quantity-resource-api)_ | Shm is the size limit of the shared memory volume |  |  |  |
| `temporaryData` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#quantity-resource-api)_ | TemporaryData is the size limit of the temporary data volume |  |  |  |


#### ExpansionPolicy



ExpansionPolicy defines how much to expand the PVC when triggered.



_Appears in:_

- [ResizeConfiguration](#resizeconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `step` _[IntOrString](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#intorstring-intstr-util)_ | Step is the amount to increase the PVC by on each resize.<br />Can be a percentage (e.g., "20%") of current size or an absolute<br />value (e.g., "10Gi"). Defaults to "20%". |  |  |  |
| `minStep` _string_ | MinStep is the minimum expansion step when using percentage-based steps.<br />Prevents tiny expansions on small volumes. Defaults to "2Gi".<br />Ignored when step is an absolute value. |  | 2Gi |  |
| `maxStep` _string_ | MaxStep is the maximum expansion step when using percentage-based steps.<br />Prevents oversized expansions on large volumes. Defaults to "500Gi".<br />Ignored when step is an absolute value. |  | 500Gi |  |
| `limit` _string_ | Limit is the maximum size the PVC can be expanded to.<br />Once this limit is reached, no further automatic resizing will occur. |  |  |  |


#### ExtensionConfiguration



ExtensionConfiguration is the configuration used to add
PostgreSQL extensions to the Cluster.



_Appears in:_

- [PostgresConfiguration](#postgresconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | The name of the extension, required | True |  | MinLength: 1 <br />Pattern: `^[a-z0-9]([-a-z0-9_]*[a-z0-9])?$` <br /> |
| `image` _[ImageVolumeSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#imagevolumesource-v1-core)_ | The image containing the extension, required | True |  |  |
| `extension_control_path` _string array_ | The list of directories inside the image which should be added to extension_control_path.<br />If not defined, defaults to "/share". |  |  |  |
| `dynamic_library_path` _string array_ | The list of directories inside the image which should be added to dynamic_library_path.<br />If not defined, defaults to "/lib". |  |  |  |
| `ld_library_path` _string array_ | The list of directories inside the image which should be added to ld_library_path. |  |  |  |


#### ExtensionSpec



ExtensionSpec configures an extension in a database



_Appears in:_

- [DatabaseSpec](#databasespec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | Name of the object (extension, schema, FDW, server) | True |  |  |
| `ensure` _[EnsureOption](#ensureoption)_ | Specifies whether an object (e.g schema) should be present or absent<br />in the database. If set to `present`, the object will be created if<br />it does not exist. If set to `absent`, the extension/schema will be<br />removed if it exists. |  | present | Enum: [present absent] <br /> |
| `version` _string_ | The version of the extension to install. If empty, the operator will<br />install the default version (whatever is specified in the<br />extension's control file) | True |  |  |
| `schema` _string_ | The name of the schema in which to install the extension's objects,<br />in case the extension allows its contents to be relocated. If not<br />specified (default), and the extension's control file does not<br />specify a schema either, the current default object creation schema<br />is used. | True |  |  |


#### ExternalCluster



ExternalCluster represents the connection parameters to an
external cluster which is used in the other sections of the configuration



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | The server name, required | True |  |  |
| `connectionParameters` _object (keys:string, values:string)_ | The list of connection parameters, such as dbname, host, username, etc |  |  |  |
| `sslCert` _[SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#secretkeyselector-v1-core)_ | The reference to an SSL certificate to be used to connect to this<br />instance |  |  |  |
| `sslKey` _[SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#secretkeyselector-v1-core)_ | The reference to an SSL private key to be used to connect to this<br />instance |  |  |  |
| `sslRootCert` _[SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#secretkeyselector-v1-core)_ | The reference to an SSL CA public key to be used to connect to this<br />instance |  |  |  |
| `password` _[SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#secretkeyselector-v1-core)_ | The reference to the password to be used to connect to the server.<br />If a password is provided, CloudNativePG creates a PostgreSQL<br />passfile at `/controller/external/NAME/pass` (where "NAME" is the<br />cluster's name). This passfile is automatically referenced in the<br />connection string when establishing a connection to the remote<br />PostgreSQL server from the current PostgreSQL `Cluster`. This ensures<br />secure and efficient password management for external clusters. |  |  |  |
| `barmanObjectStore` _[BarmanObjectStoreConfiguration](https://pkg.go.dev/github.com/cloudnative-pg/barman-cloud/pkg/api#BarmanObjectStoreConfiguration)_ | The configuration for the barman-cloud tool suite |  |  |  |
| `plugin` _[PluginConfiguration](#pluginconfiguration)_ | The configuration of the plugin that is taking care<br />of WAL archiving and backups for this external cluster | True |  |  |


#### FDWSpec



FDWSpec configures an Foreign Data Wrapper in a database



_Appears in:_

- [DatabaseSpec](#databasespec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | Name of the object (extension, schema, FDW, server) | True |  |  |
| `ensure` _[EnsureOption](#ensureoption)_ | Specifies whether an object (e.g schema) should be present or absent<br />in the database. If set to `present`, the object will be created if<br />it does not exist. If set to `absent`, the extension/schema will be<br />removed if it exists. |  | present | Enum: [present absent] <br /> |
| `handler` _string_ | Name of the handler function (e.g., "postgres_fdw_handler").<br />This will be empty if no handler is specified. In that case,<br />the default handler is registered when the FDW extension is created. |  |  |  |
| `validator` _string_ | Name of the validator function (e.g., "postgres_fdw_validator").<br />This will be empty if no validator is specified. In that case,<br />the default validator is registered when the FDW extension is created. |  |  |  |
| `owner` _string_ | Owner specifies the database role that will own the Foreign Data Wrapper.<br />The role must have superuser privileges in the target database. |  |  |  |
| `options` _[OptionSpec](#optionspec) array_ | Options specifies the configuration options for the FDW. |  |  |  |
| `usage` _[UsageSpec](#usagespec) array_ | List of roles for which `USAGE` privileges on the FDW are granted or revoked. |  |  |  |


#### FailoverQuorum



FailoverQuorum contains the information about the current failover
quorum status of a PG cluster. It is updated by the instance manager
of the primary node and reset to zero by the operator to trigger
an update.





| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `apiVersion` _string_ | `postgresql.cnpg.io/v1` | True | | |
| `kind` _string_ | `FailoverQuorum` | True | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. | True |  |  |
| `status` _[FailoverQuorumStatus](#failoverquorumstatus)_ | Most recently observed status of the failover quorum. |  |  |  |


#### FailoverQuorumStatus



FailoverQuorumStatus is the latest observed status of the failover
quorum of the PG cluster.



_Appears in:_

- [FailoverQuorum](#failoverquorum)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `method` _string_ | Contains the latest reported Method value. |  |  |  |
| `standbyNames` _string array_ | StandbyNames is the list of potentially synchronous<br />instance names. |  |  |  |
| `standbyNumber` _integer_ | StandbyNumber is the number of synchronous standbys that transactions<br />need to wait for replies from. |  |  |  |
| `primary` _string_ | Primary is the name of the primary instance that updated<br />this object the latest time. |  |  |  |






#### ImageCatalog



ImageCatalog is the Schema for the imagecatalogs API





| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `apiVersion` _string_ | `postgresql.cnpg.io/v1` | True | | |
| `kind` _string_ | `ImageCatalog` | True | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. | True |  |  |
| `spec` _[ImageCatalogSpec](#imagecatalogspec)_ | Specification of the desired behavior of the ImageCatalog.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | True |  |  |


#### ImageCatalogRef



ImageCatalogRef defines the reference to a major version in an ImageCatalog



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `apiGroup` _string_ | APIGroup is the group for the resource being referenced.<br />If APIGroup is not specified, the specified Kind must be in the core API group.<br />For any other third-party types, APIGroup is required. |  |  |  |
| `kind` _string_ | Kind is the type of resource being referenced | True |  |  |
| `name` _string_ | Name is the name of resource being referenced | True |  |  |
| `major` _integer_ | The major version of PostgreSQL we want to use from the ImageCatalog | True |  |  |


#### ImageCatalogSpec



ImageCatalogSpec defines the desired ImageCatalog



_Appears in:_

- [ClusterImageCatalog](#clusterimagecatalog)
- [ImageCatalog](#imagecatalog)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `images` _[CatalogImage](#catalogimage) array_ | List of CatalogImages available in the catalog | True |  | MaxItems: 8 <br />MinItems: 1 <br /> |


#### ImageInfo



ImageInfo contains the information about a PostgreSQL image



_Appears in:_

- [ClusterStatus](#clusterstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `image` _string_ | Image is the image name | True |  |  |
| `majorVersion` _integer_ | MajorVersion is the major version of the image | True |  |  |


#### Import



Import contains the configuration to init a database from a logic snapshot of an externalCluster



_Appears in:_

- [BootstrapInitDB](#bootstrapinitdb)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `source` _[ImportSource](#importsource)_ | The source of the import | True |  |  |
| `type` _[SnapshotType](#snapshottype)_ | The import type. Can be `microservice` or `monolith`. | True |  | Enum: [microservice monolith] <br /> |
| `databases` _string array_ | The databases to import | True |  |  |
| `roles` _string array_ | The roles to import |  |  |  |
| `postImportApplicationSQL` _string array_ | List of SQL queries to be executed as a superuser in the application<br />database right after is imported - to be used with extreme care<br />(by default empty). Only available in microservice type. |  |  |  |
| `schemaOnly` _boolean_ | When set to true, only the `pre-data` and `post-data` sections of<br />`pg_restore` are invoked, avoiding data import. Default: `false`. |  |  |  |
| `pgDumpExtraOptions` _string array_ | List of custom options to pass to the `pg_dump` command.<br />IMPORTANT: Use with caution. The operator does not validate these options,<br />and certain flags may interfere with its intended functionality or design.<br />You are responsible for ensuring that the provided options are compatible<br />with your environment and desired behavior. |  |  |  |
| `pgRestoreExtraOptions` _string array_ | List of custom options to pass to the `pg_restore` command.<br />IMPORTANT: Use with caution. The operator does not validate these options,<br />and certain flags may interfere with its intended functionality or design.<br />You are responsible for ensuring that the provided options are compatible<br />with your environment and desired behavior. |  |  |  |
| `pgRestorePredataOptions` _string array_ | Custom options to pass to the `pg_restore` command during the `pre-data`<br />section. This setting overrides the generic `pgRestoreExtraOptions` value.<br />IMPORTANT: Use with caution. The operator does not validate these options,<br />and certain flags may interfere with its intended functionality or design.<br />You are responsible for ensuring that the provided options are compatible<br />with your environment and desired behavior. |  |  |  |
| `pgRestoreDataOptions` _string array_ | Custom options to pass to the `pg_restore` command during the `data`<br />section. This setting overrides the generic `pgRestoreExtraOptions` value.<br />IMPORTANT: Use with caution. The operator does not validate these options,<br />and certain flags may interfere with its intended functionality or design.<br />You are responsible for ensuring that the provided options are compatible<br />with your environment and desired behavior. |  |  |  |
| `pgRestorePostdataOptions` _string array_ | Custom options to pass to the `pg_restore` command during the `post-data`<br />section. This setting overrides the generic `pgRestoreExtraOptions` value.<br />IMPORTANT: Use with caution. The operator does not validate these options,<br />and certain flags may interfere with its intended functionality or design.<br />You are responsible for ensuring that the provided options are compatible<br />with your environment and desired behavior. |  |  |  |


#### ImportSource



ImportSource describes the source for the logical snapshot



_Appears in:_

- [Import](#import)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `externalCluster` _string_ | The name of the externalCluster used for import | True |  |  |


#### InactiveSlotInfo



InactiveSlotInfo contains information about an inactive replication slot.



_Appears in:_

- [WALHealthInfo](#walhealthinfo)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `slotName` _string_ | SlotName is the name of the replication slot. | True |  |  |
| `retentionBytes` _integer_ | RetentionBytes is the amount of WAL retained by this slot in bytes. | True |  |  |


#### InstanceDiskStatus



InstanceDiskStatus contains disk usage status for a single instance.



_Appears in:_

- [ClusterDiskStatus](#clusterdiskstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `dataVolume` _[VolumeDiskStatus](#volumediskstatus)_ | DataVolume contains disk stats for the PGDATA volume. |  |  |  |
| `walVolume` _[VolumeDiskStatus](#volumediskstatus)_ | WALVolume contains disk stats for the WAL volume (if separate from PGDATA). |  |  |  |
| `tablespaces` _object (keys:string, values:[VolumeDiskStatus](#volumediskstatus))_ | Tablespaces contains disk stats for tablespace volumes. |  |  |  |
| `walHealth` _[WALHealthInfo](#walhealthinfo)_ | WALHealth contains the WAL archive health status. |  |  |  |
| `lastUpdated` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | LastUpdated is the timestamp when this status was last updated. |  |  |  |


#### InstanceID



InstanceID contains the information to identify an instance



_Appears in:_

- [BackupStatus](#backupstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `podName` _string_ | The pod name |  |  |  |
| `ContainerID` _string_ | The container ID |  |  |  |
| `sessionID` _string_ | The instance manager session ID. This is a unique identifier generated at instance manager<br />startup and changes on every restart (including container reboots). Used to detect if<br />the instance manager was restarted during long-running operations like backups, which<br />would terminate any running backup process. |  |  |  |


#### InstanceReportedState



InstanceReportedState describes the last reported state of an instance during a reconciliation loop



_Appears in:_

- [ClusterStatus](#clusterstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `isPrimary` _boolean_ | indicates if an instance is the primary one | True |  |  |
| `timeLineID` _integer_ | indicates on which TimelineId the instance is |  |  |  |
| `ip` _string_ | IP address of the instance | True |  |  |


#### IsolationCheckConfiguration



IsolationCheckConfiguration contains the configuration for the isolation check
functionality in the liveness probe



_Appears in:_

- [LivenessProbe](#livenessprobe)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `enabled` _boolean_ | Whether primary isolation checking is enabled for the liveness probe |  | true |  |
| `requestTimeout` _integer_ | Timeout in milliseconds for requests during the primary isolation check |  | 1000 |  |
| `connectionTimeout` _integer_ | Timeout in milliseconds for connections during the primary isolation check |  | 1000 |  |




#### LDAPBindAsAuth



LDAPBindAsAuth provides the required fields to use the
bind authentication for LDAP



_Appears in:_

- [LDAPConfig](#ldapconfig)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `prefix` _string_ | Prefix for the bind authentication option |  |  |  |
| `suffix` _string_ | Suffix for the bind authentication option |  |  |  |


#### LDAPBindSearchAuth



LDAPBindSearchAuth provides the required fields to use
the bind+search LDAP authentication process



_Appears in:_

- [LDAPConfig](#ldapconfig)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `baseDN` _string_ | Root DN to begin the user search |  |  |  |
| `bindDN` _string_ | DN of the user to bind to the directory |  |  |  |
| `bindPassword` _[SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#secretkeyselector-v1-core)_ | Secret with the password for the user to bind to the directory |  |  |  |
| `searchAttribute` _string_ | Attribute to match against the username |  |  |  |
| `searchFilter` _string_ | Search filter to use when doing the search+bind authentication |  |  |  |


#### LDAPConfig



LDAPConfig contains the parameters needed for LDAP authentication



_Appears in:_

- [PostgresConfiguration](#postgresconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `server` _string_ | LDAP hostname or IP address |  |  |  |
| `port` _integer_ | LDAP server port |  |  |  |
| `scheme` _[LDAPScheme](#ldapscheme)_ | LDAP schema to be used, possible options are `ldap` and `ldaps` |  |  | Enum: [ldap ldaps] <br /> |
| `bindAsAuth` _[LDAPBindAsAuth](#ldapbindasauth)_ | Bind as authentication configuration |  |  |  |
| `bindSearchAuth` _[LDAPBindSearchAuth](#ldapbindsearchauth)_ | Bind+Search authentication configuration |  |  |  |
| `tls` _boolean_ | Set to 'true' to enable LDAP over TLS. 'false' is default |  |  |  |


#### LDAPScheme

_Underlying type:_ _string_

LDAPScheme defines the possible schemes for LDAP



_Appears in:_

- [LDAPConfig](#ldapconfig)

| Field | Description |
| --- | --- |
| `ldap` |  |
| `ldaps` |  |


#### LivenessProbe



LivenessProbe is the configuration of the liveness probe



_Appears in:_

- [ProbesConfiguration](#probesconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `initialDelaySeconds` _integer_ | Number of seconds after the container has started before liveness probes are initiated.<br />More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes |  |  |  |
| `timeoutSeconds` _integer_ | Number of seconds after which the probe times out.<br />Defaults to 1 second. Minimum value is 1.<br />More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes |  |  |  |
| `periodSeconds` _integer_ | How often (in seconds) to perform the probe.<br />Default to 10 seconds. Minimum value is 1. |  |  |  |
| `successThreshold` _integer_ | Minimum consecutive successes for the probe to be considered successful after having failed.<br />Defaults to 1. Must be 1 for liveness and startup. Minimum value is 1. |  |  |  |
| `failureThreshold` _integer_ | Minimum consecutive failures for the probe to be considered failed after having succeeded.<br />Defaults to 3. Minimum value is 1. |  |  |  |
| `terminationGracePeriodSeconds` _integer_ | Optional duration in seconds the pod needs to terminate gracefully upon probe failure.<br />The grace period is the duration in seconds after the processes running in the pod are sent<br />a termination signal and the time when the processes are forcibly halted with a kill signal.<br />Set this value longer than the expected cleanup time for your process.<br />If this value is nil, the pod's terminationGracePeriodSeconds will be used. Otherwise, this<br />value overrides the value provided by the pod spec.<br />Value must be non-negative integer. The value zero indicates stop immediately via<br />the kill signal (no opportunity to shut down).<br />This is a beta field and requires enabling ProbeTerminationGracePeriod feature gate.<br />Minimum value is 1. spec.terminationGracePeriodSeconds is used if unset. |  |  |  |
| `isolationCheck` _[IsolationCheckConfiguration](#isolationcheckconfiguration)_ | Configure the feature that extends the liveness probe for a primary<br />instance. In addition to the basic checks, this verifies whether the<br />primary is isolated from the Kubernetes API server and from its<br />replicas, ensuring that it can be safely shut down if network<br />partition or API unavailability is detected. Enabled by default. |  |  |  |




#### ManagedConfiguration



ManagedConfiguration represents the portions of PostgreSQL that are managed
by the instance manager



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `roles` _[RoleConfiguration](#roleconfiguration) array_ | Database roles managed by the `Cluster` |  |  |  |
| `services` _[ManagedServices](#managedservices)_ | Services roles managed by the `Cluster` |  |  |  |


#### ManagedRoles



ManagedRoles tracks the status of a cluster's managed roles



_Appears in:_

- [ClusterStatus](#clusterstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `byStatus` _object (keys:[RoleStatus](#rolestatus), values:string array)_ | ByStatus gives the list of roles in each state |  |  |  |
| `cannotReconcile` _object (keys:string, values:string array)_ | CannotReconcile lists roles that cannot be reconciled in PostgreSQL,<br />with an explanation of the cause |  |  |  |
| `passwordStatus` _object (keys:string, values:[PasswordState](#passwordstate))_ | PasswordStatus gives the last transaction id and password secret version for each managed role |  |  |  |


#### ManagedService



ManagedService represents a specific service managed by the cluster.
It includes the type of service and its associated template specification.



_Appears in:_

- [ManagedServices](#managedservices)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `selectorType` _[ServiceSelectorType](#serviceselectortype)_ | SelectorType specifies the type of selectors that the service will have.<br />Valid values are "rw", "r", and "ro", representing read-write, read, and read-only services. | True |  | Enum: [rw r ro] <br /> |
| `updateStrategy` _[ServiceUpdateStrategy](#serviceupdatestrategy)_ | UpdateStrategy describes how the service differences should be reconciled |  | patch | Enum: [patch replace] <br /> |
| `serviceTemplate` _[ServiceTemplateSpec](#servicetemplatespec)_ | ServiceTemplate is the template specification for the service. | True |  |  |


#### ManagedServices



ManagedServices represents the services managed by the cluster.



_Appears in:_

- [ManagedConfiguration](#managedconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `disabledDefaultServices` _[ServiceSelectorType](#serviceselectortype) array_ | DisabledDefaultServices is a list of service types that are disabled by default.<br />Valid values are "r", and "ro", representing read, and read-only services. |  |  | Enum: [rw r ro] <br /> |
| `additional` _[ManagedService](#managedservice) array_ | Additional is a list of additional managed services specified by the user. |  |  |  |


#### Metadata



Metadata is a structure similar to the metav1.ObjectMeta, but still
parseable by controller-gen to create a suitable CRD for the user.
The comment of PodTemplateSpec has an explanation of why we are
not using the core data types.



_Appears in:_

- [PodTemplateSpec](#podtemplatespec)
- [ServiceAccountTemplate](#serviceaccounttemplate)
- [ServiceTemplateSpec](#servicetemplatespec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | The name of the resource. Only supported for certain types |  |  |  |
| `labels` _object (keys:string, values:string)_ | Map of string keys and values that can be used to organize and categorize<br />(scope and select) objects. May match selectors of replication controllers<br />and services.<br />More info: http://kubernetes.io/docs/user-guide/labels |  |  |  |
| `annotations` _object (keys:string, values:string)_ | Annotations is an unstructured key value map stored with a resource that may be<br />set by external tools to store and retrieve arbitrary metadata. They are not<br />queryable and should be preserved when modifying objects.<br />More info: http://kubernetes.io/docs/user-guide/annotations |  |  |  |


#### MonitoringConfiguration



MonitoringConfiguration is the type containing all the monitoring
configuration for a certain cluster



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `disableDefaultQueries` _boolean_ | Whether the default queries should be injected.<br />Set it to `true` if you don't want to inject default queries into the cluster.<br />Default: false. |  | false |  |
| `customQueriesConfigMap` _[ConfigMapKeySelector](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#ConfigMapKeySelector) array_ | The list of config maps containing the custom queries |  |  |  |
| `customQueriesSecret` _[SecretKeySelector](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#SecretKeySelector) array_ | The list of secrets containing the custom queries |  |  |  |
| `enablePodMonitor` _boolean_ | Enable or disable the `PodMonitor`<br />Deprecated: This feature will be removed in an upcoming release. If<br />you need this functionality, you can create a PodMonitor manually. |  | false |  |
| `tls` _[ClusterMonitoringTLSConfiguration](#clustermonitoringtlsconfiguration)_ | Configure TLS communication for the metrics endpoint.<br />Changing tls.enabled option will force a rollout of all instances. |  |  |  |
| `podMonitorMetricRelabelings` _[RelabelConfig](https://pkg.go.dev/github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1#RelabelConfig) array_ | The list of metric relabelings for the `PodMonitor`. Applied to samples before ingestion.<br />Deprecated: This feature will be removed in an upcoming release. If<br />you need this functionality, you can create a PodMonitor manually. |  |  |  |
| `podMonitorRelabelings` _[RelabelConfig](https://pkg.go.dev/github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1#RelabelConfig) array_ | The list of relabelings for the `PodMonitor`. Applied to samples before scraping.<br />Deprecated: This feature will be removed in an upcoming release. If<br />you need this functionality, you can create a PodMonitor manually. |  |  |  |
| `metricsQueriesTTL` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | The interval during which metrics computed from queries are considered current.<br />Once it is exceeded, a new scrape will trigger a rerun<br />of the queries.<br />If not set, defaults to 30 seconds, in line with Prometheus scraping defaults.<br />Setting this to zero disables the caching mechanism and can cause heavy load on the PostgreSQL server. |  |  |  |


#### NodeMaintenanceWindow



NodeMaintenanceWindow contains information that the operator
will use while upgrading the underlying node.

This option is only useful when the chosen storage prevents the Pods
from being freely moved across nodes.



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `reusePVC` _boolean_ | Reuse the existing PVC (wait for the node to come<br />up again) or not (recreate it elsewhere - when `instances` >1) |  | true |  |
| `inProgress` _boolean_ | Is there a node maintenance activity in progress? |  | false |  |


#### OnlineConfiguration



OnlineConfiguration contains the configuration parameters for the online volume snapshot



_Appears in:_

- [BackupSpec](#backupspec)
- [ScheduledBackupSpec](#scheduledbackupspec)
- [VolumeSnapshotConfiguration](#volumesnapshotconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `waitForArchive` _boolean_ | If false, the function will return immediately after the backup is completed,<br />without waiting for WAL to be archived.<br />This behavior is only useful with backup software that independently monitors WAL archiving.<br />Otherwise, WAL required to make the backup consistent might be missing and make the backup useless.<br />By default, or when this parameter is true, pg_backup_stop will wait for WAL to be archived when archiving is<br />enabled.<br />On a standby, this means that it will wait only when archive_mode = always.<br />If write activity on the primary is low, it may be useful to run pg_switch_wal on the primary in order to trigger<br />an immediate segment switch. |  | true |  |
| `immediateCheckpoint` _boolean_ | Control whether the I/O workload for the backup initial checkpoint will<br />be limited, according to the `checkpoint_completion_target` setting on<br />the PostgreSQL server. If set to true, an immediate checkpoint will be<br />used, meaning PostgreSQL will complete the checkpoint as soon as<br />possible. `false` by default. |  |  |  |


#### OptionSpec



OptionSpec holds the name, value and the ensure field for an option



_Appears in:_

- [FDWSpec](#fdwspec)
- [ServerSpec](#serverspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | Name of the option | True |  |  |
| `value` _string_ | Value of the option | True |  |  |
| `ensure` _[EnsureOption](#ensureoption)_ | Specifies whether an option should be present or absent in<br />the database. If set to `present`, the option will be<br />created if it does not exist. If set to `absent`, the<br />option will be removed if it exists. |  | present | Enum: [present absent] <br /> |


#### PasswordState



PasswordState represents the state of the password of a managed RoleConfiguration



_Appears in:_

- [ManagedRoles](#managedroles)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `transactionID` _integer_ | the last transaction ID to affect the role definition in PostgreSQL |  |  |  |
| `resourceVersion` _string_ | the resource version of the password secret |  |  |  |


#### PgBouncerIntegrationStatus



PgBouncerIntegrationStatus encapsulates the needed integration for the pgbouncer poolers referencing the cluster



_Appears in:_

- [PoolerIntegrations](#poolerintegrations)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `secrets` _string array_ |  |  |  |  |


#### PgBouncerPoolMode

_Underlying type:_ _string_

PgBouncerPoolMode is the mode of PgBouncer

_Validation:_

- Enum: [session transaction]

_Appears in:_

- [PgBouncerSpec](#pgbouncerspec)



#### PgBouncerSecrets



PgBouncerSecrets contains the versions of the secrets used
by pgbouncer



_Appears in:_

- [PoolerSecrets](#poolersecrets)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `authQuery` _[SecretVersion](#secretversion)_ | The auth query secret version |  |  |  |


#### PgBouncerSpec



PgBouncerSpec defines how to configure PgBouncer



_Appears in:_

- [PoolerSpec](#poolerspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `poolMode` _[PgBouncerPoolMode](#pgbouncerpoolmode)_ | The pool mode. Default: `session`. |  | session | Enum: [session transaction] <br /> |
| `serverTLSSecret` _[LocalObjectReference](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#LocalObjectReference)_ | ServerTLSSecret, when pointing to a TLS secret, provides pgbouncer's<br />`server_tls_key_file` and `server_tls_cert_file`, used when<br />authenticating against PostgreSQL. |  |  |  |
| `serverCASecret` _[LocalObjectReference](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#LocalObjectReference)_ | ServerCASecret provides PgBouncer’s server_tls_ca_file, the root<br />CA for validating PostgreSQL certificates |  |  |  |
| `clientCASecret` _[LocalObjectReference](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#LocalObjectReference)_ | ClientCASecret provides PgBouncer’s client_tls_ca_file, the root<br />CA for validating client certificates |  |  |  |
| `clientTLSSecret` _[LocalObjectReference](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#LocalObjectReference)_ | ClientTLSSecret provides PgBouncer’s client_tls_key_file (private key)<br />and client_tls_cert_file (certificate) used to accept client connections |  |  |  |
| `authQuerySecret` _[LocalObjectReference](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#LocalObjectReference)_ | The credentials of the user that need to be used for the authentication<br />query. In case it is specified, also an AuthQuery<br />(e.g. "SELECT usename, passwd FROM pg_catalog.pg_shadow WHERE usename=$1")<br />has to be specified and no automatic CNPG Cluster integration will be triggered.<br />Deprecated. |  |  |  |
| `authQuery` _string_ | The query that will be used to download the hash of the password<br />of a certain user. Default: "SELECT usename, passwd FROM public.user_search($1)".<br />In case it is specified, also an AuthQuerySecret has to be specified and<br />no automatic CNPG Cluster integration will be triggered. |  |  |  |
| `parameters` _object (keys:string, values:string)_ | Additional parameters to be passed to PgBouncer - please check<br />the CNPG documentation for a list of options you can configure |  |  |  |
| `pg_hba` _string array_ | PostgreSQL Host Based Authentication rules (lines to be appended<br />to the pg_hba.conf file) |  |  |  |
| `paused` _boolean_ | When set to `true`, PgBouncer will disconnect from the PostgreSQL<br />server, first waiting for all queries to complete, and pause all new<br />client connections until this value is set to `false` (default). Internally,<br />the operator calls PgBouncer's `PAUSE` and `RESUME` commands. |  | false |  |


#### PluginConfiguration



PluginConfiguration specifies a plugin that need to be loaded for this
cluster to be reconciled



_Appears in:_

- [ClusterSpec](#clusterspec)
- [ExternalCluster](#externalcluster)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | Name is the plugin name | True |  |  |
| `enabled` _boolean_ | Enabled is true if this plugin will be used |  | true |  |
| `isWALArchiver` _boolean_ | Marks the plugin as the WAL archiver. At most one plugin can be<br />designated as a WAL archiver. This cannot be enabled if the<br />`.spec.backup.barmanObjectStore` configuration is present. |  | false |  |
| `parameters` _object (keys:string, values:string)_ | Parameters is the configuration of the plugin |  |  |  |


#### PluginStatus



PluginStatus is the status of a loaded plugin



_Appears in:_

- [ClusterStatus](#clusterstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | Name is the name of the plugin | True |  |  |
| `version` _string_ | Version is the version of the plugin loaded by the<br />latest reconciliation loop | True |  |  |
| `capabilities` _string array_ | Capabilities are the list of capabilities of the<br />plugin |  |  |  |
| `operatorCapabilities` _string array_ | OperatorCapabilities are the list of capabilities of the<br />plugin regarding the reconciler |  |  |  |
| `walCapabilities` _string array_ | WALCapabilities are the list of capabilities of the<br />plugin regarding the WAL management |  |  |  |
| `backupCapabilities` _string array_ | BackupCapabilities are the list of capabilities of the<br />plugin regarding the Backup management |  |  |  |
| `restoreJobHookCapabilities` _string array_ | RestoreJobHookCapabilities are the list of capabilities of the<br />plugin regarding the RestoreJobHook management |  |  |  |
| `status` _string_ | Status contain the status reported by the plugin through the SetStatusInCluster interface |  |  |  |


#### PodName

_Underlying type:_ _string_

PodName is the name of a Pod



_Appears in:_

- [ClusterStatus](#clusterstatus)
- [Topology](#topology)



#### PodStatus

_Underlying type:_ _string_

PodStatus represent the possible status of pods



_Appears in:_

- [ClusterStatus](#clusterstatus)



#### PodTemplateSpec



PodTemplateSpec is a structure allowing the user to set
a template for Pod generation.

Unfortunately we can't use the corev1.PodTemplateSpec
type because the generated CRD won't have the field for the
metadata section.

References:
https://github.com/kubernetes-sigs/controller-tools/issues/385
https://github.com/kubernetes-sigs/controller-tools/issues/448
https://github.com/prometheus-operator/prometheus-operator/issues/3041



_Appears in:_

- [PoolerSpec](#poolerspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `metadata` _[Metadata](#metadata)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |  |
| `spec` _[PodSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#podspec-v1-core)_ | Specification of the desired behavior of the pod.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status |  |  |  |


#### PodTopologyLabels

_Underlying type:_ _object_

PodTopologyLabels represent the topology of a Pod. map[labelName]labelValue



_Appears in:_

- [Topology](#topology)



#### Pooler



Pooler is the Schema for the poolers API





| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `apiVersion` _string_ | `postgresql.cnpg.io/v1` | True | | |
| `kind` _string_ | `Pooler` | True | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. | True |  |  |
| `spec` _[PoolerSpec](#poolerspec)_ | Specification of the desired behavior of the Pooler.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | True |  |  |
| `status` _[PoolerStatus](#poolerstatus)_ | Most recently observed status of the Pooler. This data may not be up to<br />date. Populated by the system. Read-only.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status |  |  |  |


#### PoolerIntegrations



PoolerIntegrations encapsulates the needed integration for the poolers referencing the cluster



_Appears in:_

- [ClusterStatus](#clusterstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `pgBouncerIntegration` _[PgBouncerIntegrationStatus](#pgbouncerintegrationstatus)_ |  |  |  |  |


#### PoolerMonitoringConfiguration



PoolerMonitoringConfiguration is the type containing all the monitoring
configuration for a certain Pooler.

Mirrors the Cluster's MonitoringConfiguration but without the custom queries
part for now.



_Appears in:_

- [PoolerSpec](#poolerspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `enablePodMonitor` _boolean_ | Enable or disable the `PodMonitor` |  | false |  |
| `podMonitorMetricRelabelings` _[RelabelConfig](https://pkg.go.dev/github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1#RelabelConfig) array_ | The list of metric relabelings for the `PodMonitor`. Applied to samples before ingestion. |  |  |  |
| `podMonitorRelabelings` _[RelabelConfig](https://pkg.go.dev/github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1#RelabelConfig) array_ | The list of relabelings for the `PodMonitor`. Applied to samples before scraping. |  |  |  |


#### PoolerSecrets



PoolerSecrets contains the versions of all the secrets used



_Appears in:_

- [PoolerStatus](#poolerstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `clientTLS` _[SecretVersion](#secretversion)_ | The client TLS secret version |  |  |  |
| `serverTLS` _[SecretVersion](#secretversion)_ | The server TLS secret version |  |  |  |
| `serverCA` _[SecretVersion](#secretversion)_ | The server CA secret version |  |  |  |
| `clientCA` _[SecretVersion](#secretversion)_ | The client CA secret version |  |  |  |
| `pgBouncerSecrets` _[PgBouncerSecrets](#pgbouncersecrets)_ | The version of the secrets used by PgBouncer |  |  |  |


#### PoolerSpec



PoolerSpec defines the desired state of Pooler



_Appears in:_

- [Pooler](#pooler)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `cluster` _[LocalObjectReference](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#LocalObjectReference)_ | This is the cluster reference on which the Pooler will work.<br />Pooler name should never match with any cluster name within the same namespace. | True |  |  |
| `type` _[PoolerType](#poolertype)_ | Type of service to forward traffic to. Default: `rw`. |  | rw | Enum: [rw ro r] <br /> |
| `instances` _integer_ | The number of replicas we want. Default: 1. |  | 1 |  |
| `template` _[PodTemplateSpec](#podtemplatespec)_ | The template of the Pod to be created |  |  |  |
| `pgbouncer` _[PgBouncerSpec](#pgbouncerspec)_ | The PgBouncer configuration | True |  |  |
| `deploymentStrategy` _[DeploymentStrategy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#deploymentstrategy-v1-apps)_ | The deployment strategy to use for pgbouncer to replace existing pods with new ones |  |  |  |
| `monitoring` _[PoolerMonitoringConfiguration](#poolermonitoringconfiguration)_ | The configuration of the monitoring infrastructure of this pooler.<br />Deprecated: This feature will be removed in an upcoming release. If<br />you need this functionality, you can create a PodMonitor manually. |  |  |  |
| `serviceTemplate` _[ServiceTemplateSpec](#servicetemplatespec)_ | Template for the Service to be created |  |  |  |


#### PoolerStatus



PoolerStatus defines the observed state of Pooler



_Appears in:_

- [Pooler](#pooler)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `secrets` _[PoolerSecrets](#poolersecrets)_ | The resource version of the config object |  |  |  |
| `instances` _integer_ | The number of pods trying to be scheduled |  |  |  |


#### PoolerType

_Underlying type:_ _string_

PoolerType is the type of the connection pool, meaning the service
we are targeting. Allowed values are `rw` and `ro`.

_Validation:_

- Enum: [rw ro r]

_Appears in:_

- [PoolerSpec](#poolerspec)



#### PostgresConfiguration



PostgresConfiguration defines the PostgreSQL configuration



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `parameters` _object (keys:string, values:string)_ | PostgreSQL configuration options (postgresql.conf) |  |  |  |
| `synchronous` _[SynchronousReplicaConfiguration](#synchronousreplicaconfiguration)_ | Configuration of the PostgreSQL synchronous replication feature |  |  |  |
| `pg_hba` _string array_ | PostgreSQL Host Based Authentication rules (lines to be appended<br />to the pg_hba.conf file) |  |  |  |
| `pg_ident` _string array_ | PostgreSQL User Name Maps rules (lines to be appended<br />to the pg_ident.conf file) |  |  |  |
| `syncReplicaElectionConstraint` _[SyncReplicaElectionConstraints](#syncreplicaelectionconstraints)_ | Requirements to be met by sync replicas. This will affect how the "synchronous_standby_names" parameter will be<br />set up. |  |  |  |
| `shared_preload_libraries` _string array_ | Lists of shared preload libraries to add to the default ones |  |  |  |
| `ldap` _[LDAPConfig](#ldapconfig)_ | Options to specify LDAP configuration |  |  |  |
| `promotionTimeout` _integer_ | Specifies the maximum number of seconds to wait when promoting an instance to primary.<br />Default value is 40000000, greater than one year in seconds,<br />big enough to simulate an infinite timeout |  |  |  |
| `enableAlterSystem` _boolean_ | If this parameter is true, the user will be able to invoke `ALTER SYSTEM`<br />on this CloudNativePG Cluster.<br />This should only be used for debugging and troubleshooting.<br />Defaults to false. |  |  |  |
| `extensions` _[ExtensionConfiguration](#extensionconfiguration) array_ | The configuration of the extensions to be added |  |  |  |


#### PrimaryUpdateMethod

_Underlying type:_ _string_

PrimaryUpdateMethod contains the method to use when upgrading
the primary server of the cluster as part of rolling updates



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description |
| --- | --- |
| `switchover` | PrimaryUpdateMethodSwitchover means that the operator will switchover to another updated<br />replica when it needs to upgrade the primary instance.<br />Note: when using this method, the operator will reject updates that change both<br />the image name and PostgreSQL configuration parameters simultaneously to avoid<br />configuration mismatches during the switchover process.<br /> |
| `restart` | PrimaryUpdateMethodRestart means that the operator will restart the primary instance in-place<br />when it needs to upgrade it<br /> |


#### PrimaryUpdateStrategy

_Underlying type:_ _string_

PrimaryUpdateStrategy contains the strategy to follow when upgrading
the primary server of the cluster as part of rolling updates



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description |
| --- | --- |
| `supervised` | PrimaryUpdateStrategySupervised means that the operator need to wait for the<br />user to manually issue a switchover request before updating the primary<br />server (`supervised`)<br /> |
| `unsupervised` | PrimaryUpdateStrategyUnsupervised means that the operator will proceed with the<br />selected PrimaryUpdateMethod to another updated replica and then automatically update<br />the primary server (`unsupervised`, default)<br /> |


#### Probe



Probe describes a health check to be performed against a container to determine whether it is
alive or ready to receive traffic.



_Appears in:_

- [LivenessProbe](#livenessprobe)
- [ProbeWithStrategy](#probewithstrategy)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `initialDelaySeconds` _integer_ | Number of seconds after the container has started before liveness probes are initiated.<br />More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes |  |  |  |
| `timeoutSeconds` _integer_ | Number of seconds after which the probe times out.<br />Defaults to 1 second. Minimum value is 1.<br />More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes |  |  |  |
| `periodSeconds` _integer_ | How often (in seconds) to perform the probe.<br />Default to 10 seconds. Minimum value is 1. |  |  |  |
| `successThreshold` _integer_ | Minimum consecutive successes for the probe to be considered successful after having failed.<br />Defaults to 1. Must be 1 for liveness and startup. Minimum value is 1. |  |  |  |
| `failureThreshold` _integer_ | Minimum consecutive failures for the probe to be considered failed after having succeeded.<br />Defaults to 3. Minimum value is 1. |  |  |  |
| `terminationGracePeriodSeconds` _integer_ | Optional duration in seconds the pod needs to terminate gracefully upon probe failure.<br />The grace period is the duration in seconds after the processes running in the pod are sent<br />a termination signal and the time when the processes are forcibly halted with a kill signal.<br />Set this value longer than the expected cleanup time for your process.<br />If this value is nil, the pod's terminationGracePeriodSeconds will be used. Otherwise, this<br />value overrides the value provided by the pod spec.<br />Value must be non-negative integer. The value zero indicates stop immediately via<br />the kill signal (no opportunity to shut down).<br />This is a beta field and requires enabling ProbeTerminationGracePeriod feature gate.<br />Minimum value is 1. spec.terminationGracePeriodSeconds is used if unset. |  |  |  |


#### ProbeStrategyType

_Underlying type:_ _string_

ProbeStrategyType is the type of the strategy used to declare a PostgreSQL instance
ready



_Appears in:_

- [ProbeWithStrategy](#probewithstrategy)

| Field | Description |
| --- | --- |
| `pg_isready` | ProbeStrategyPgIsReady means that the pg_isready tool is used to determine<br />whether PostgreSQL is started up<br /> |
| `streaming` | ProbeStrategyStreaming means that pg_isready is positive and the replica is<br />connected via streaming replication to the current primary and the lag is, if specified,<br />within the limit.<br /> |
| `query` | ProbeStrategyQuery means that the server is able to connect to the superuser database<br />and able to execute a simple query like "-- ping"<br /> |


#### ProbeWithStrategy



ProbeWithStrategy is the configuration of the startup probe



_Appears in:_

- [ProbesConfiguration](#probesconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `initialDelaySeconds` _integer_ | Number of seconds after the container has started before liveness probes are initiated.<br />More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes |  |  |  |
| `timeoutSeconds` _integer_ | Number of seconds after which the probe times out.<br />Defaults to 1 second. Minimum value is 1.<br />More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes |  |  |  |
| `periodSeconds` _integer_ | How often (in seconds) to perform the probe.<br />Default to 10 seconds. Minimum value is 1. |  |  |  |
| `successThreshold` _integer_ | Minimum consecutive successes for the probe to be considered successful after having failed.<br />Defaults to 1. Must be 1 for liveness and startup. Minimum value is 1. |  |  |  |
| `failureThreshold` _integer_ | Minimum consecutive failures for the probe to be considered failed after having succeeded.<br />Defaults to 3. Minimum value is 1. |  |  |  |
| `terminationGracePeriodSeconds` _integer_ | Optional duration in seconds the pod needs to terminate gracefully upon probe failure.<br />The grace period is the duration in seconds after the processes running in the pod are sent<br />a termination signal and the time when the processes are forcibly halted with a kill signal.<br />Set this value longer than the expected cleanup time for your process.<br />If this value is nil, the pod's terminationGracePeriodSeconds will be used. Otherwise, this<br />value overrides the value provided by the pod spec.<br />Value must be non-negative integer. The value zero indicates stop immediately via<br />the kill signal (no opportunity to shut down).<br />This is a beta field and requires enabling ProbeTerminationGracePeriod feature gate.<br />Minimum value is 1. spec.terminationGracePeriodSeconds is used if unset. |  |  |  |
| `type` _[ProbeStrategyType](#probestrategytype)_ | The probe strategy |  |  | Enum: [pg_isready streaming query] <br /> |
| `maximumLag` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#quantity-resource-api)_ | Lag limit. Used only for `streaming` strategy |  |  |  |


#### ProbesConfiguration



ProbesConfiguration represent the configuration for the probes
to be injected in the PostgreSQL Pods



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `startup` _[ProbeWithStrategy](#probewithstrategy)_ | The startup probe configuration | True |  |  |
| `liveness` _[LivenessProbe](#livenessprobe)_ | The liveness probe configuration | True |  |  |
| `readiness` _[ProbeWithStrategy](#probewithstrategy)_ | The readiness probe configuration | True |  |  |


#### Publication



Publication is the Schema for the publications API





| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `apiVersion` _string_ | `postgresql.cnpg.io/v1` | True | | |
| `kind` _string_ | `Publication` | True | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. | True |  |  |
| `spec` _[PublicationSpec](#publicationspec)_ |  | True |  |  |
| `status` _[PublicationStatus](#publicationstatus)_ |  | True |  |  |


#### PublicationReclaimPolicy

_Underlying type:_ _string_

PublicationReclaimPolicy defines a policy for end-of-life maintenance of Publications.



_Appears in:_

- [PublicationSpec](#publicationspec)

| Field | Description |
| --- | --- |
| `delete` | PublicationReclaimDelete means the publication will be deleted from Kubernetes on release<br />from its claim.<br /> |
| `retain` | PublicationReclaimRetain means the publication will be left in its current phase for manual<br />reclamation by the administrator. The default policy is Retain.<br /> |


#### PublicationSpec



PublicationSpec defines the desired state of Publication



_Appears in:_

- [Publication](#publication)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `cluster` _[LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#localobjectreference-v1-core)_ | The name of the PostgreSQL cluster that identifies the "publisher" | True |  |  |
| `name` _string_ | The name of the publication inside PostgreSQL | True |  |  |
| `dbname` _string_ | The name of the database where the publication will be installed in<br />the "publisher" cluster | True |  |  |
| `parameters` _object (keys:string, values:string)_ | Publication parameters part of the `WITH` clause as expected by<br />PostgreSQL `CREATE PUBLICATION` command |  |  |  |
| `target` _[PublicationTarget](#publicationtarget)_ | Target of the publication as expected by PostgreSQL `CREATE PUBLICATION` command | True |  |  |
| `publicationReclaimPolicy` _[PublicationReclaimPolicy](#publicationreclaimpolicy)_ | The policy for end-of-life maintenance of this publication |  | retain | Enum: [delete retain] <br /> |


#### PublicationStatus



PublicationStatus defines the observed state of Publication



_Appears in:_

- [Publication](#publication)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `observedGeneration` _integer_ | A sequence number representing the latest<br />desired state that was synchronized |  |  |  |
| `applied` _boolean_ | Applied is true if the publication was reconciled correctly |  |  |  |
| `message` _string_ | Message is the reconciliation output message |  |  |  |


#### PublicationTarget



PublicationTarget is what this publication should publish



_Appears in:_

- [PublicationSpec](#publicationspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `allTables` _boolean_ | Marks the publication as one that replicates changes for all tables<br />in the database, including tables created in the future.<br />Corresponding to `FOR ALL TABLES` in PostgreSQL. |  |  |  |
| `objects` _[PublicationTargetObject](#publicationtargetobject) array_ | Just the following schema objects |  |  | MaxItems: 100000 <br /> |


#### PublicationTargetObject



PublicationTargetObject is an object to publish



_Appears in:_

- [PublicationTarget](#publicationtarget)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `tablesInSchema` _string_ | Marks the publication as one that replicates changes for all tables<br />in the specified list of schemas, including tables created in the<br />future. Corresponding to `FOR TABLES IN SCHEMA` in PostgreSQL. |  |  |  |
| `table` _[PublicationTargetTable](#publicationtargettable)_ | Specifies a list of tables to add to the publication. Corresponding<br />to `FOR TABLE` in PostgreSQL. |  |  |  |


#### PublicationTargetTable



PublicationTargetTable is a table to publish



_Appears in:_

- [PublicationTargetObject](#publicationtargetobject)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `only` _boolean_ | Whether to limit to the table only or include all its descendants |  |  |  |
| `name` _string_ | The table name | True |  |  |
| `schema` _string_ | The schema name |  |  |  |
| `columns` _string array_ | The columns to publish |  |  |  |


#### RecoveryTarget



RecoveryTarget allows to configure the moment where the recovery process
will stop. All the target options except TargetTLI are mutually exclusive.



_Appears in:_

- [BootstrapRecovery](#bootstraprecovery)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `backupID` _string_ | The ID of the backup from which to start the recovery process.<br />If empty (default) the operator will automatically detect the backup<br />based on targetTime or targetLSN if specified. Otherwise use the<br />latest available backup in chronological order. |  |  |  |
| `targetTLI` _string_ | The target timeline ("latest" or a positive integer) |  |  |  |
| `targetXID` _string_ | The target transaction ID |  |  |  |
| `targetName` _string_ | The target name (to be previously created<br />with `pg_create_restore_point`) |  |  |  |
| `targetLSN` _string_ | The target LSN (Log Sequence Number) |  |  |  |
| `targetTime` _string_ | The target time as a timestamp in RFC3339 format or PostgreSQL timestamp format.<br />Timestamps without an explicit timezone are interpreted as UTC. |  |  |  |
| `targetImmediate` _boolean_ | End recovery as soon as a consistent state is reached |  |  |  |
| `exclusive` _boolean_ | Set the target to be exclusive. If omitted, defaults to false, so that<br />in Postgres, `recovery_target_inclusive` will be true |  |  |  |


#### ReplicaClusterConfiguration



ReplicaClusterConfiguration encapsulates the configuration of a replica
cluster



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `self` _string_ | Self defines the name of this cluster. It is used to determine if this is a primary<br />or a replica cluster, comparing it with `primary` |  |  |  |
| `primary` _string_ | Primary defines which Cluster is defined to be the primary in the distributed PostgreSQL cluster, based on the<br />topology specified in externalClusters |  |  |  |
| `source` _string_ | The name of the external cluster which is the replication origin | True |  | MinLength: 1 <br /> |
| `enabled` _boolean_ | If replica mode is enabled, this cluster will be a replica of an<br />existing cluster. Replica cluster can be created from a recovery<br />object store or via streaming through pg_basebackup.<br />Refer to the Replica clusters page of the documentation for more information. |  |  |  |
| `promotionToken` _string_ | A demotion token generated by an external cluster used to<br />check if the promotion requirements are met. |  |  |  |
| `minApplyDelay` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | When replica mode is enabled, this parameter allows you to replay<br />transactions only when the system time is at least the configured<br />time past the commit time. This provides an opportunity to correct<br />data loss errors. Note that when this parameter is set, a promotion<br />token cannot be used. |  |  |  |


#### ReplicationSlotsConfiguration



ReplicationSlotsConfiguration encapsulates the configuration
of replication slots



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `highAvailability` _[ReplicationSlotsHAConfiguration](#replicationslotshaconfiguration)_ | Replication slots for high availability configuration |  | \{ enabled:true \} |  |
| `updateInterval` _integer_ | Standby will update the status of the local replication slots<br />every `updateInterval` seconds (default 30). |  | 30 | Minimum: 1 <br /> |
| `synchronizeReplicas` _[SynchronizeReplicasConfiguration](#synchronizereplicasconfiguration)_ | Configures the synchronization of the user defined physical replication slots |  |  |  |


#### ReplicationSlotsHAConfiguration



ReplicationSlotsHAConfiguration encapsulates the configuration
of the replication slots that are automatically managed by
the operator to control the streaming replication connections
with the standby instances for high availability (HA) purposes.
Replication slots are a PostgreSQL feature that makes sure
that PostgreSQL automatically keeps WAL files in the primary
when a streaming client (in this specific case a replica that
is part of the HA cluster) gets disconnected.



_Appears in:_

- [ReplicationSlotsConfiguration](#replicationslotsconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `enabled` _boolean_ | If enabled (default), the operator will automatically manage replication slots<br />on the primary instance and use them in streaming replication<br />connections with all the standby instances that are part of the HA<br />cluster. If disabled, the operator will not take advantage<br />of replication slots in streaming connections with the replicas.<br />This feature also controls replication slots in replica cluster,<br />from the designated primary to its cascading replicas. |  | true |  |
| `slotPrefix` _string_ | Prefix for replication slots managed by the operator for HA.<br />It may only contain lower case letters, numbers, and the underscore character.<br />This can only be set at creation time. By default set to `_cnpg_`. |  | _cnpg_ | Pattern: `^[0-9a-z_]*$` <br /> |
| `synchronizeLogicalDecoding` _boolean_ | When enabled, the operator automatically manages synchronization of logical<br />decoding (replication) slots across high-availability clusters.<br />Requires one of the following conditions:<br />- PostgreSQL version 17 or later<br />- PostgreSQL version < 17 with pg_failover_slots extension enabled |  |  |  |


#### ResizeConfiguration



ResizeConfiguration defines the automatic PVC resize behavior.



_Appears in:_

- [StorageConfiguration](#storageconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `enabled` _boolean_ | Enabled activates automatic PVC resizing. |  | false |  |
| `triggers` _[ResizeTriggers](#resizetriggers)_ | Triggers defines the conditions that trigger a resize operation. |  |  |  |
| `expansion` _[ExpansionPolicy](#expansionpolicy)_ | Expansion defines the expansion policy including step size and limits. |  |  |  |
| `strategy` _[ResizeStrategy](#resizestrategy)_ | Strategy defines how resize operations are performed, including<br />rate limiting and WAL safety policies. |  |  |  |


#### ResizeMode

_Underlying type:_ _string_

ResizeMode represents the mode of auto-resize operations.

_Validation:_

- Enum: [Standard]

_Appears in:_

- [ResizeStrategy](#resizestrategy)

| Field | Description |
| --- | --- |
| `Standard` | ResizeModeStandard is the standard resize mode using Kubernetes PVC patching.<br /> |


#### ResizeResult

_Underlying type:_ _string_

ResizeResult represents the outcome of a resize operation.

_Validation:_

- Enum: [success failure]

_Appears in:_

- [AutoResizeEvent](#autoresizeevent)

| Field | Description |
| --- | --- |
| `success` | ResizeResultSuccess indicates the resize operation succeeded.<br /> |
| `failure` | ResizeResultFailure indicates the resize operation failed.<br /> |


#### ResizeStrategy



ResizeStrategy defines the operational strategy for auto-resize.



_Appears in:_

- [ResizeConfiguration](#resizeconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `mode` _[ResizeMode](#resizemode)_ | Mode defines the resize mode. Currently only "Standard" is supported. |  | Standard | Enum: [Standard] <br /> |
| `maxActionsPerDay` _integer_ | MaxActionsPerDay is the maximum number of resize operations per volume<br />within a 24-hour rolling window. Reflects cloud provider limits<br />(e.g., AWS EBS allows ~4 modifications per day). |  |  | Maximum: 10 <br />Minimum: 0 <br /> |
| `walSafetyPolicy` _[WALSafetyPolicy](#walsafetypolicy)_ | WALSafetyPolicy defines safety checks for WAL-related volumes.<br />When the data volume shares WAL storage (single-volume clusters)<br />or when resizing the WAL volume, these checks ensure archiving<br />and replication are healthy before allowing resize. |  |  |  |


#### ResizeTriggers



ResizeTriggers defines the conditions that trigger an auto-resize.



_Appears in:_

- [ResizeConfiguration](#resizeconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `usageThreshold` _integer_ | UsageThreshold is the disk usage percentage (1-99) that triggers a resize.<br />When the volume usage exceeds this threshold, a resize is triggered.<br />Either condition (UsageThreshold or MinAvailable) alone is sufficient. |  |  | Maximum: 99 <br />Minimum: 1 <br /> |
| `minAvailable` _string_ | MinAvailable is the minimum available space that must remain on the volume.<br />When available space drops below this value, a resize is triggered.<br />Can be specified as an absolute value (e.g., "10Gi"). |  |  |  |


#### ResizeVolumeType

_Underlying type:_ _string_

ResizeVolumeType represents the type of volume in a resize operation.

_Validation:_

- Enum: [data wal tablespace]

_Appears in:_

- [AutoResizeEvent](#autoresizeevent)

| Field | Description |
| --- | --- |
| `data` | ResizeVolumeTypeData represents a PostgreSQL data volume.<br /> |
| `wal` | ResizeVolumeTypeWAL represents a PostgreSQL WAL volume.<br /> |
| `tablespace` | ResizeVolumeTypeTablespace represents a PostgreSQL tablespace volume.<br /> |


#### RoleConfiguration



RoleConfiguration is the representation, in Kubernetes, of a PostgreSQL role
with the additional field Ensure specifying whether to ensure the presence or
absence of the role in the database

The defaults of the CREATE ROLE command are applied
Reference: https://www.postgresql.org/docs/current/sql-createrole.html



_Appears in:_

- [ManagedConfiguration](#managedconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | Name of the role | True |  |  |
| `comment` _string_ | Description of the role |  |  |  |
| `ensure` _[EnsureOption](#ensureoption)_ | Ensure the role is `present` or `absent` - defaults to "present" |  | present | Enum: [present absent] <br /> |
| `passwordSecret` _[LocalObjectReference](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#LocalObjectReference)_ | Secret containing the password of the role (if present)<br />If null, the password will be ignored unless DisablePassword is set |  |  |  |
| `connectionLimit` _integer_ | If the role can log in, this specifies how many concurrent<br />connections the role can make. `-1` (the default) means no limit. |  | -1 |  |
| `validUntil` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | Date and time after which the role's password is no longer valid.<br />When omitted, the password will never expire (default). |  |  |  |
| `inRoles` _string array_ | List of one or more existing roles to which this role will be<br />immediately added as a new member. Default empty. |  |  |  |
| `inherit` _boolean_ | Whether a role "inherits" the privileges of roles it is a member of.<br />Defaults is `true`. |  | true |  |
| `disablePassword` _boolean_ | DisablePassword indicates that a role's password should be set to NULL in Postgres |  |  |  |
| `superuser` _boolean_ | Whether the role is a `superuser` who can override all access<br />restrictions within the database - superuser status is dangerous and<br />should be used only when really needed. You must yourself be a<br />superuser to create a new superuser. Defaults is `false`. |  |  |  |
| `createdb` _boolean_ | When set to `true`, the role being defined will be allowed to create<br />new databases. Specifying `false` (default) will deny a role the<br />ability to create databases. |  |  |  |
| `createrole` _boolean_ | Whether the role will be permitted to create, alter, drop, comment<br />on, change the security label for, and grant or revoke membership in<br />other roles. Default is `false`. |  |  |  |
| `login` _boolean_ | Whether the role is allowed to log in. A role having the `login`<br />attribute can be thought of as a user. Roles without this attribute<br />are useful for managing database privileges, but are not users in<br />the usual sense of the word. Default is `false`. |  |  |  |
| `replication` _boolean_ | Whether a role is a replication role. A role must have this<br />attribute (or be a superuser) in order to be able to connect to the<br />server in replication mode (physical or logical replication) and in<br />order to be able to create or drop replication slots. A role having<br />the `replication` attribute is a very highly privileged role, and<br />should only be used on roles actually used for replication. Default<br />is `false`. |  |  |  |
| `bypassrls` _boolean_ | Whether a role bypasses every row-level security (RLS) policy.<br />Default is `false`. |  |  |  |


#### RoleStatus

_Underlying type:_ _string_

RoleStatus represents the status of a managed role in the cluster



_Appears in:_

- [ManagedRoles](#managedroles)

| Field | Description |
| --- | --- |
| `reconciled` | RoleStatusReconciled indicates the role in DB matches the Spec<br /> |
| `not-managed` | RoleStatusNotManaged indicates the role is not in the Spec, therefore not managed<br /> |
| `pending-reconciliation` | RoleStatusPendingReconciliation indicates the role in Spec requires updated/creation in DB<br /> |
| `reserved` | RoleStatusReserved indicates this is one of the roles reserved by the operator. E.g. `postgres`<br /> |




#### SQLRefs



SQLRefs holds references to ConfigMaps or Secrets
containing SQL files. The references are processed in a specific order:
first, all Secrets are processed, followed by all ConfigMaps.
Within each group, the processing order follows the sequence specified
in their respective arrays.



_Appears in:_

- [BootstrapInitDB](#bootstrapinitdb)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `secretRefs` _[SecretKeySelector](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#SecretKeySelector) array_ | SecretRefs holds a list of references to Secrets |  |  |  |
| `configMapRefs` _[ConfigMapKeySelector](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#ConfigMapKeySelector) array_ | ConfigMapRefs holds a list of references to ConfigMaps |  |  |  |


#### ScheduledBackup



ScheduledBackup is the Schema for the scheduledbackups API





| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `apiVersion` _string_ | `postgresql.cnpg.io/v1` | True | | |
| `kind` _string_ | `ScheduledBackup` | True | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. | True |  |  |
| `spec` _[ScheduledBackupSpec](#scheduledbackupspec)_ | Specification of the desired behavior of the ScheduledBackup.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | True |  |  |
| `status` _[ScheduledBackupStatus](#scheduledbackupstatus)_ | Most recently observed status of the ScheduledBackup. This data may not be up<br />to date. Populated by the system. Read-only.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status |  |  |  |


#### ScheduledBackupSpec



ScheduledBackupSpec defines the desired state of ScheduledBackup



_Appears in:_

- [ScheduledBackup](#scheduledbackup)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `suspend` _boolean_ | If this backup is suspended or not |  |  |  |
| `immediate` _boolean_ | If the first backup has to be immediately start after creation or not |  |  |  |
| `schedule` _string_ | The schedule does not follow the same format used in Kubernetes CronJobs<br />as it includes an additional seconds specifier,<br />see https://pkg.go.dev/github.com/robfig/cron#hdr-CRON_Expression_Format | True |  |  |
| `cluster` _[LocalObjectReference](https://pkg.go.dev/github.com/cloudnative-pg/machinery/pkg/api#LocalObjectReference)_ | The cluster to backup | True |  |  |
| `backupOwnerReference` _string_ | Indicates which ownerReference should be put inside the created backup resources.<br />- none: no owner reference for created backup objects (same behavior as before the field was introduced)<br />- self: sets the Scheduled backup object as owner of the backup<br />- cluster: set the cluster as owner of the backup<br /> |  | none | Enum: [none self cluster] <br /> |
| `target` _[BackupTarget](#backuptarget)_ | The policy to decide which instance should perform this backup. If empty,<br />it defaults to `cluster.spec.backup.target`.<br />Available options are empty string, `primary` and `prefer-standby`.<br />`primary` to have backups run always on primary instances,<br />`prefer-standby` to have backups run preferably on the most updated<br />standby, if available. |  |  | Enum: [primary prefer-standby] <br /> |
| `method` _[BackupMethod](#backupmethod)_ | The backup method to be used, possible options are `barmanObjectStore`,<br />`volumeSnapshot` or `plugin`. Defaults to: `barmanObjectStore`. |  | barmanObjectStore | Enum: [barmanObjectStore volumeSnapshot plugin] <br /> |
| `pluginConfiguration` _[BackupPluginConfiguration](#backuppluginconfiguration)_ | Configuration parameters passed to the plugin managing this backup |  |  |  |
| `online` _boolean_ | Whether the default type of backup with volume snapshots is<br />online/hot (`true`, default) or offline/cold (`false`)<br />Overrides the default setting specified in the cluster field '.spec.backup.volumeSnapshot.online' |  |  |  |
| `onlineConfiguration` _[OnlineConfiguration](#onlineconfiguration)_ | Configuration parameters to control the online/hot backup with volume snapshots<br />Overrides the default settings specified in the cluster '.backup.volumeSnapshot.onlineConfiguration' stanza |  |  |  |


#### ScheduledBackupStatus



ScheduledBackupStatus defines the observed state of ScheduledBackup



_Appears in:_

- [ScheduledBackup](#scheduledbackup)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `lastCheckTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | The latest time the schedule |  |  |  |
| `lastScheduleTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | Information when was the last time that backup was successfully scheduled. |  |  |  |
| `nextScheduleTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | Next time we will run a backup |  |  |  |


#### SchemaSpec



SchemaSpec configures a schema in a database



_Appears in:_

- [DatabaseSpec](#databasespec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | Name of the object (extension, schema, FDW, server) | True |  |  |
| `ensure` _[EnsureOption](#ensureoption)_ | Specifies whether an object (e.g schema) should be present or absent<br />in the database. If set to `present`, the object will be created if<br />it does not exist. If set to `absent`, the extension/schema will be<br />removed if it exists. |  | present | Enum: [present absent] <br /> |
| `owner` _string_ | The role name of the user who owns the schema inside PostgreSQL.<br />It maps to the `AUTHORIZATION` parameter of `CREATE SCHEMA` and the<br />`OWNER TO` command of `ALTER SCHEMA`. | True |  |  |




#### SecretVersion



SecretVersion contains a secret name and its ResourceVersion



_Appears in:_

- [PgBouncerSecrets](#pgbouncersecrets)
- [PoolerSecrets](#poolersecrets)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | The name of the secret |  |  |  |
| `version` _string_ | The ResourceVersion of the secret |  |  |  |


#### SecretsResourceVersion



SecretsResourceVersion is the resource versions of the secrets
managed by the operator



_Appears in:_

- [ClusterStatus](#clusterstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `superuserSecretVersion` _string_ | The resource version of the "postgres" user secret |  |  |  |
| `replicationSecretVersion` _string_ | The resource version of the "streaming_replica" user secret |  |  |  |
| `applicationSecretVersion` _string_ | The resource version of the "app" user secret |  |  |  |
| `managedRoleSecretVersion` _object (keys:string, values:string)_ | The resource versions of the managed roles secrets |  |  |  |
| `caSecretVersion` _string_ | Unused. Retained for compatibility with old versions. |  |  |  |
| `clientCaSecretVersion` _string_ | The resource version of the PostgreSQL client-side CA secret version |  |  |  |
| `serverCaSecretVersion` _string_ | The resource version of the PostgreSQL server-side CA secret version |  |  |  |
| `serverSecretVersion` _string_ | The resource version of the PostgreSQL server-side secret version |  |  |  |
| `barmanEndpointCA` _string_ | The resource version of the Barman Endpoint CA if provided |  |  |  |
| `externalClusterSecretVersion` _object (keys:string, values:string)_ | The resource versions of the external cluster secrets |  |  |  |
| `metrics` _object (keys:string, values:string)_ | A map with the versions of all the secrets used to pass metrics.<br />Map keys are the secret names, map values are the versions |  |  |  |


#### ServerSpec



ServerSpec configures a server of a foreign data wrapper



_Appears in:_

- [DatabaseSpec](#databasespec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | Name of the object (extension, schema, FDW, server) | True |  |  |
| `ensure` _[EnsureOption](#ensureoption)_ | Specifies whether an object (e.g schema) should be present or absent<br />in the database. If set to `present`, the object will be created if<br />it does not exist. If set to `absent`, the extension/schema will be<br />removed if it exists. |  | present | Enum: [present absent] <br /> |
| `fdw` _string_ | The name of the Foreign Data Wrapper (FDW) | True |  |  |
| `options` _[OptionSpec](#optionspec) array_ | Options specifies the configuration options for the server<br />(key is the option name, value is the option value). |  |  |  |
| `usage` _[UsageSpec](#usagespec) array_ | List of roles for which `USAGE` privileges on the server are granted or revoked. |  |  |  |


#### ServiceAccountTemplate



ServiceAccountTemplate contains the template needed to generate the service accounts



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `metadata` _[Metadata](#metadata)_ | Refer to Kubernetes API documentation for fields of `metadata`. | True |  |  |


#### ServiceSelectorType

_Underlying type:_ _string_

ServiceSelectorType describes a valid value for generating the service selectors.
It indicates which type of service the selector applies to, such as read-write, read, or read-only

_Validation:_

- Enum: [rw r ro]

_Appears in:_

- [ManagedService](#managedservice)
- [ManagedServices](#managedservices)

| Field | Description |
| --- | --- |
| `rw` | ServiceSelectorTypeRW selects the read-write service.<br /> |
| `r` | ServiceSelectorTypeR selects the read service.<br /> |
| `ro` | ServiceSelectorTypeRO selects the read-only service.<br /> |


#### ServiceTemplateSpec



ServiceTemplateSpec is a structure allowing the user to set
a template for Service generation.



_Appears in:_

- [ManagedService](#managedservice)
- [PoolerSpec](#poolerspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `metadata` _[Metadata](#metadata)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |  |
| `spec` _[ServiceSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#servicespec-v1-core)_ | Specification of the desired behavior of the service.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status |  |  |  |


#### ServiceUpdateStrategy

_Underlying type:_ _string_

ServiceUpdateStrategy describes how the changes to the managed service should be handled

_Validation:_

- Enum: [patch replace]

_Appears in:_

- [ManagedService](#managedservice)



#### SnapshotOwnerReference

_Underlying type:_ _string_

SnapshotOwnerReference defines the reference type for the owner of the snapshot.
This specifies which owner the processed resources should relate to.



_Appears in:_

- [VolumeSnapshotConfiguration](#volumesnapshotconfiguration)

| Field | Description |
| --- | --- |
| `none` | SnapshotOwnerReferenceNone indicates that the snapshot does not have any owner reference.<br /> |
| `backup` | SnapshotOwnerReferenceBackup indicates that the snapshot is owned by the backup resource.<br /> |
| `cluster` | SnapshotOwnerReferenceCluster indicates that the snapshot is owned by the cluster resource.<br /> |


#### SnapshotType

_Underlying type:_ _string_

SnapshotType is a type of allowed import



_Appears in:_

- [Import](#import)

| Field | Description |
| --- | --- |
| `monolith` | MonolithSnapshotType indicates to execute the monolith clone typology<br /> |
| `microservice` | MicroserviceSnapshotType indicates to execute the microservice clone typology<br /> |


#### StorageConfiguration



StorageConfiguration is the configuration used to create and reconcile PVCs,
usable for WAL volumes, PGDATA volumes, or tablespaces



_Appears in:_

- [ClusterSpec](#clusterspec)
- [TablespaceConfiguration](#tablespaceconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `storageClass` _string_ | StorageClass to use for PVCs. Applied after<br />evaluating the PVC template, if available.<br />If not specified, the generated PVCs will use the<br />default storage class |  |  |  |
| `size` _string_ | Size of the storage. Required if not already specified in the PVC template.<br />Changes to this field are automatically reapplied to the created PVCs.<br />Size cannot be decreased. |  |  |  |
| `resizeInUseVolumes` _boolean_ | Resize existent PVCs, defaults to true |  | true |  |
| `pvcTemplate` _[PersistentVolumeClaimSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#persistentvolumeclaimspec-v1-core)_ | Template to be used to generate the Persistent Volume Claim |  |  |  |
| `resize` _[ResizeConfiguration](#resizeconfiguration)_ | Resize contains the configuration for automatic PVC resizing.<br />When enabled, CloudNativePG will monitor disk usage and automatically<br />expand PVCs when configured thresholds are reached. |  |  |  |


#### Subscription



Subscription is the Schema for the subscriptions API





| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `apiVersion` _string_ | `postgresql.cnpg.io/v1` | True | | |
| `kind` _string_ | `Subscription` | True | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. | True |  |  |
| `spec` _[SubscriptionSpec](#subscriptionspec)_ |  | True |  |  |
| `status` _[SubscriptionStatus](#subscriptionstatus)_ |  | True |  |  |


#### SubscriptionReclaimPolicy

_Underlying type:_ _string_

SubscriptionReclaimPolicy describes a policy for end-of-life maintenance of Subscriptions.



_Appears in:_

- [SubscriptionSpec](#subscriptionspec)

| Field | Description |
| --- | --- |
| `delete` | SubscriptionReclaimDelete means the subscription will be deleted from Kubernetes on release<br />from its claim.<br /> |
| `retain` | SubscriptionReclaimRetain means the subscription will be left in its current phase for manual<br />reclamation by the administrator. The default policy is Retain.<br /> |


#### SubscriptionSpec



SubscriptionSpec defines the desired state of Subscription



_Appears in:_

- [Subscription](#subscription)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `cluster` _[LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#localobjectreference-v1-core)_ | The name of the PostgreSQL cluster that identifies the "subscriber" | True |  |  |
| `name` _string_ | The name of the subscription inside PostgreSQL | True |  |  |
| `dbname` _string_ | The name of the database where the publication will be installed in<br />the "subscriber" cluster | True |  |  |
| `parameters` _object (keys:string, values:string)_ | Subscription parameters included in the `WITH` clause of the PostgreSQL<br />`CREATE SUBSCRIPTION` command. Most parameters cannot be changed<br />after the subscription is created and will be ignored if modified<br />later, except for a limited set documented at:<br />https://www.postgresql.org/docs/current/sql-altersubscription.html#SQL-ALTERSUBSCRIPTION-PARAMS-SET |  |  |  |
| `publicationName` _string_ | The name of the publication inside the PostgreSQL database in the<br />"publisher" | True |  |  |
| `publicationDBName` _string_ | The name of the database containing the publication on the external<br />cluster. Defaults to the one in the external cluster definition. |  |  |  |
| `externalClusterName` _string_ | The name of the external cluster with the publication ("publisher") | True |  |  |
| `subscriptionReclaimPolicy` _[SubscriptionReclaimPolicy](#subscriptionreclaimpolicy)_ | The policy for end-of-life maintenance of this subscription |  | retain | Enum: [delete retain] <br /> |


#### SubscriptionStatus



SubscriptionStatus defines the observed state of Subscription



_Appears in:_

- [Subscription](#subscription)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `observedGeneration` _integer_ | A sequence number representing the latest<br />desired state that was synchronized |  |  |  |
| `applied` _boolean_ | Applied is true if the subscription was reconciled correctly |  |  |  |
| `message` _string_ | Message is the reconciliation output message |  |  |  |


#### SwitchReplicaClusterStatus



SwitchReplicaClusterStatus contains all the statuses regarding the switch of a cluster to a replica cluster



_Appears in:_

- [ClusterStatus](#clusterstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `inProgress` _boolean_ | InProgress indicates if there is an ongoing procedure of switching a cluster to a replica cluster. |  |  |  |


#### SyncReplicaElectionConstraints



SyncReplicaElectionConstraints contains the constraints for sync replicas election.

For anti-affinity parameters two instances are considered in the same location
if all the labels values match.

In future synchronous replica election restriction by name will be supported.



_Appears in:_

- [PostgresConfiguration](#postgresconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `nodeLabelsAntiAffinity` _string array_ | A list of node labels values to extract and compare to evaluate if the pods reside in the same topology or not |  |  |  |
| `enabled` _boolean_ | This flag enables the constraints for sync replicas | True |  |  |


#### SynchronizeReplicasConfiguration



SynchronizeReplicasConfiguration contains the configuration for the synchronization of user defined
physical replication slots



_Appears in:_

- [ReplicationSlotsConfiguration](#replicationslotsconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `enabled` _boolean_ | When set to true, every replication slot that is on the primary is synchronized on each standby | True | true |  |
| `excludePatterns` _string array_ | List of regular expression patterns to match the names of replication slots to be excluded (by default empty) |  |  |  |


#### SynchronousReplicaConfiguration



SynchronousReplicaConfiguration contains the configuration of the
PostgreSQL synchronous replication feature.
Important: at this moment, also `.spec.minSyncReplicas` and `.spec.maxSyncReplicas`
need to be considered.



_Appears in:_

- [PostgresConfiguration](#postgresconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `method` _[SynchronousReplicaConfigurationMethod](#synchronousreplicaconfigurationmethod)_ | Method to select synchronous replication standbys from the listed<br />servers, accepting 'any' (quorum-based synchronous replication) or<br />'first' (priority-based synchronous replication) as values. | True |  | Enum: [any first] <br /> |
| `number` _integer_ | Specifies the number of synchronous standby servers that<br />transactions must wait for responses from. | True |  |  |
| `maxStandbyNamesFromCluster` _integer_ | Specifies the maximum number of local cluster pods that can be<br />automatically included in the `synchronous_standby_names` option in<br />PostgreSQL. |  |  |  |
| `standbyNamesPre` _string array_ | A user-defined list of application names to be added to<br />`synchronous_standby_names` before local cluster pods (the order is<br />only useful for priority-based synchronous replication). |  |  |  |
| `standbyNamesPost` _string array_ | A user-defined list of application names to be added to<br />`synchronous_standby_names` after local cluster pods (the order is<br />only useful for priority-based synchronous replication). |  |  |  |
| `dataDurability` _[DataDurabilityLevel](#datadurabilitylevel)_ | If set to "required", data durability is strictly enforced. Write operations<br />with synchronous commit settings (`on`, `remote_write`, or `remote_apply`) will<br />block if there are insufficient healthy replicas, ensuring data persistence.<br />If set to "preferred", data durability is maintained when healthy replicas<br />are available, but the required number of instances will adjust dynamically<br />if replicas become unavailable. This setting relaxes strict durability enforcement<br />to allow for operational continuity. This setting is only applicable if both<br />`standbyNamesPre` and `standbyNamesPost` are unset (empty). |  |  | Enum: [required preferred] <br /> |
| `failoverQuorum` _boolean_ | FailoverQuorum enables a quorum-based check before failover, improving<br />data durability and safety during failover events in CloudNativePG-managed<br />PostgreSQL clusters. |  |  |  |


#### SynchronousReplicaConfigurationMethod

_Underlying type:_ _string_

SynchronousReplicaConfigurationMethod configures whether to use
quorum based replication or a priority list



_Appears in:_

- [SynchronousReplicaConfiguration](#synchronousreplicaconfiguration)



#### TablespaceConfiguration



TablespaceConfiguration is the configuration of a tablespace, and includes
the storage specification for the tablespace



_Appears in:_

- [ClusterSpec](#clusterspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | The name of the tablespace | True |  |  |
| `storage` _[StorageConfiguration](#storageconfiguration)_ | The storage configuration for the tablespace | True |  |  |
| `owner` _[DatabaseRoleRef](#databaseroleref)_ | Owner is the PostgreSQL user owning the tablespace |  |  |  |
| `temporary` _boolean_ | When set to true, the tablespace will be added as a `temp_tablespaces`<br />entry in PostgreSQL, and will be available to automatically house temp<br />database objects, or other temporary files. Please refer to PostgreSQL<br />documentation for more information on the `temp_tablespaces` GUC. |  | false |  |


#### TablespaceState



TablespaceState represents the state of a tablespace in a cluster



_Appears in:_

- [ClusterStatus](#clusterstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | Name is the name of the tablespace | True |  |  |
| `owner` _string_ | Owner is the PostgreSQL user owning the tablespace |  |  |  |
| `state` _[TablespaceStatus](#tablespacestatus)_ | State is the latest reconciliation state | True |  |  |
| `error` _string_ | Error is the reconciliation error, if any |  |  |  |


#### TablespaceStatus

_Underlying type:_ _string_

TablespaceStatus represents the status of a tablespace in the cluster



_Appears in:_

- [TablespaceState](#tablespacestate)

| Field | Description |
| --- | --- |
| `reconciled` | TablespaceStatusReconciled indicates the tablespace in DB matches the Spec<br /> |
| `pending` | TablespaceStatusPendingReconciliation indicates the tablespace in Spec requires creation in the DB<br /> |


#### Topology



Topology contains the cluster topology



_Appears in:_

- [ClusterStatus](#clusterstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `instances` _object (keys:[PodName](#podname), values:[PodTopologyLabels](#podtopologylabels))_ | Instances contains the pod topology of the instances |  |  |  |
| `nodesUsed` _integer_ | NodesUsed represents the count of distinct nodes accommodating the instances.<br />A value of '1' suggests that all instances are hosted on a single node,<br />implying the absence of High Availability (HA). Ideally, this value should<br />be the same as the number of instances in the Postgres HA cluster, implying<br />shared nothing architecture on the compute side. |  |  |  |
| `successfullyExtracted` _boolean_ | SuccessfullyExtracted indicates if the topology data was extract. It is useful to enact fallback behaviors<br />in synchronous replica election in case of failures |  |  |  |


#### UsageSpec



UsageSpec configures a usage for a foreign data wrapper



_Appears in:_

- [FDWSpec](#fdwspec)
- [ServerSpec](#serverspec)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `name` _string_ | Name of the usage | True |  |  |
| `type` _[UsageSpecType](#usagespectype)_ | The type of usage |  | grant | Enum: [grant revoke] <br /> |


#### UsageSpecType

_Underlying type:_ _string_

UsageSpecType describes the type of usage specified in the `usage` field of the
`Database` object.



_Appears in:_

- [UsageSpec](#usagespec)

| Field | Description |
| --- | --- |
| `grant` | GrantUsageSpecType indicates a grant usage permission.<br />The default usage permission is grant.<br /> |
| `revoke` | RevokeUsageSpecType indicates a revoke usage permission.<br /> |


#### VolumeDiskStatus



VolumeDiskStatus contains the disk usage status of a single volume.



_Appears in:_

- [InstanceDiskStatus](#instancediskstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `totalBytes` _integer_ | TotalBytes is the total capacity of the volume in bytes. |  |  |  |
| `usedBytes` _integer_ | UsedBytes is the number of bytes currently in use. |  |  |  |
| `availableBytes` _integer_ | AvailableBytes is the number of bytes available for use (non-root). |  |  |  |
| `percentUsed` _integer_ | PercentUsed is the percentage of the volume in use (0-100), rounded. |  |  |  |
| `inodesTotal` _integer_ | InodesTotal is the total number of inodes on the volume. |  |  |  |
| `inodesUsed` _integer_ | InodesUsed is the number of inodes in use on the volume. |  |  |  |
| `inodesFree` _integer_ | InodesFree is the number of free inodes on the volume. |  |  |  |


#### VolumeSnapshotConfiguration



VolumeSnapshotConfiguration represents the configuration for the execution of snapshot backups.



_Appears in:_

- [BackupConfiguration](#backupconfiguration)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `labels` _object (keys:string, values:string)_ | Labels are key-value pairs that will be added to .metadata.labels snapshot resources. |  |  |  |
| `annotations` _object (keys:string, values:string)_ | Annotations key-value pairs that will be added to .metadata.annotations snapshot resources. |  |  |  |
| `className` _string_ | ClassName specifies the Snapshot Class to be used for PG_DATA PersistentVolumeClaim.<br />It is the default class for the other types if no specific class is present |  |  |  |
| `walClassName` _string_ | WalClassName specifies the Snapshot Class to be used for the PG_WAL PersistentVolumeClaim. |  |  |  |
| `tablespaceClassName` _object (keys:string, values:string)_ | TablespaceClassName specifies the Snapshot Class to be used for the tablespaces.<br />defaults to the PGDATA Snapshot Class, if set |  |  |  |
| `snapshotOwnerReference` _[SnapshotOwnerReference](#snapshotownerreference)_ | SnapshotOwnerReference indicates the type of owner reference the snapshot should have |  | none | Enum: [none cluster backup] <br /> |
| `online` _boolean_ | Whether the default type of backup with volume snapshots is<br />online/hot (`true`, default) or offline/cold (`false`) |  | true |  |
| `onlineConfiguration` _[OnlineConfiguration](#onlineconfiguration)_ | Configuration parameters to control the online/hot backup with volume snapshots |  | \{ immediateCheckpoint:false waitForArchive:true \} |  |


#### WALHealthInfo



WALHealthInfo contains WAL archive health information.



_Appears in:_

- [InstanceDiskStatus](#instancediskstatus)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `archiveHealthy` _boolean_ | ArchiveHealthy indicates whether the WAL archive process is healthy. |  |  |  |
| `pendingWALFiles` _integer_ | PendingWALFiles is the count of .ready files in pg_wal/archive_status/. |  |  |  |
| `inactiveSlotCount` _integer_ | InactiveSlotCount is the number of inactive physical replication slots. |  |  |  |
| `inactiveSlots` _[InactiveSlotInfo](#inactiveslotinfo) array_ | InactiveSlots lists inactive physical replication slots and their WAL retention. |  |  |  |


#### WALSafetyPolicy



WALSafetyPolicy defines safety checks for WAL volumes.



_Appears in:_

- [ResizeStrategy](#resizestrategy)

| Field | Description | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `acknowledgeWALRisk` _boolean_ | AcknowledgeWALRisk must be set to true for single-volume clusters<br />(where data and WAL share the same volume) to enable auto-resize.<br />This explicit acknowledgment is required because resizing without<br />separate WAL storage can mask WAL-related issues. |  |  |  |
| `requireArchiveHealthy` _boolean_ | RequireArchiveHealthy blocks resize when WAL archiving is unhealthy<br />(last_failed_time > last_archived_time). Defaults to true. |  | true |  |
| `maxPendingWALFiles` _integer_ | MaxPendingWALFiles blocks resize when the number of pending WAL files<br />(.ready files in archive_status) exceeds this threshold. Defaults to 100. |  | 100 |  |
| `maxSlotRetentionBytes` _integer_ | MaxSlotRetentionBytes blocks resize when any inactive physical<br />replication slot retains more WAL than this threshold. |  |  |  |
| `alertOnResize` _boolean_ | AlertOnResize emits a Kubernetes warning event when a WAL-related<br />resize occurs. Defaults to true. |  | true |  |




