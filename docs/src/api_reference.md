# API Reference

Cloud Native PostgreSQL extends the Kubernetes API defining the following
custom resources:

-   [Backup](#backup)
-   [Cluster](#cluster)
-   [ScheduledBackup](#scheduledbackup)

All the resources are defined in the `postgresql.k8s.enterprisedb.io/v1alpha1`
API.

Please refer to the ["Configuration Samples" page](samples.md)" of the
documentation for examples of usage.

Below you will find a description of the defined resources:

<!-- Everything from now on is generated via `make apidoc` -->
<!-- TOC -->
* [Backup](#backup)
* [BackupList](#backuplist)
* [BackupSpec](#backupspec)
* [BackupStatus](#backupstatus)
* [AffinityConfiguration](#affinityconfiguration)
* [BackupConfiguration](#backupconfiguration)
* [BarmanObjectStoreConfiguration](#barmanobjectstoreconfiguration)
* [BootstrapConfiguration](#bootstrapconfiguration)
* [BootstrapInitDB](#bootstrapinitdb)
* [BootstrapRecovery](#bootstraprecovery)
* [Cluster](#cluster)
* [ClusterList](#clusterlist)
* [ClusterSpec](#clusterspec)
* [ClusterStatus](#clusterstatus)
* [DataBackupConfiguration](#databackupconfiguration)
* [NodeMaintenanceWindow](#nodemaintenancewindow)
* [PostgresConfiguration](#postgresconfiguration)
* [RecoveryTarget](#recoverytarget)
* [RollingUpdateStatus](#rollingupdatestatus)
* [S3Credentials](#s3credentials)
* [StorageConfiguration](#storageconfiguration)
* [WalBackupConfiguration](#walbackupconfiguration)
* [ScheduledBackup](#scheduledbackup)
* [ScheduledBackupList](#scheduledbackuplist)
* [ScheduledBackupSpec](#scheduledbackupspec)
* [ScheduledBackupStatus](#scheduledbackupstatus)

## Backup

Backup is the Schema for the backups API

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectmeta-v1-meta) | false |
| spec | Specification of the desired behavior of the backup. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | [BackupSpec](#backupspec) | false |
| status | Most recently observed status of the backup. This data may not be up to date. Populated by the system. Read-only. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | [BackupStatus](#backupstatus) | false |


## BackupList

BackupList contains a list of Backup

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| metadata | Standard list metadata. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#listmeta-v1-meta) | false |
| items | List of backups | [][Backup](#backup) | true |


## BackupSpec

BackupSpec defines the desired state of Backup

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| cluster | The cluster to backup | [v1.LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#localobjectreference-v1-core) | false |


## BackupStatus

BackupStatus defines the observed state of Backup

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| s3Credentials | The credentials to use to upload data to S3 | [S3Credentials](#s3credentials) | true |
| endpointURL | Endpoint to be used to upload data to the cloud, overriding the automatic endpoint discovery | string | false |
| destinationPath | The path where to store the backup (i.e. s3://bucket/path/to/folder) this path, with different destination folders, will be used for WALs and for data | string | true |
| serverName | The server name on S3, the cluster name is used if this parameter is omitted | string | false |
| encryption | Encryption method required to S3 API | string | false |
| backupId | The ID of the Barman backup | string | false |
| phase | The last backup status | BackupPhase | false |
| startedAt | When the backup was started | *metav1.Time | false |
| stoppedAt | When the backup was terminated | *metav1.Time | false |
| error | The detected error | string | false |
| commandOutput | The backup command output | string | false |
| commandError | The backup command output | string | false |


## AffinityConfiguration

AffinityConfiguration contains the info we need to create the affinity rules for Pods

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| enablePodAntiAffinity | Activates anti-affinity for the pods. The operator will define pods anti-affinity unless this field is explicitly set to false | *bool | false |
| topologyKey | TopologyKey to use for anti-affinity configuration. See k8s documentation for more info on that | string | true |
| nodeSelector | NodeSelector is map of key-value pairs used to define the nodes on which the pods can run. More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/ | map[string]string | false |


## BackupConfiguration

BackupConfiguration defines how the backup of the cluster are taken. Currently the only supported backup method is barmanObjectStore. For details and examples refer to the Backup and Recovery section of the documentation

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| barmanObjectStore | The configuration for the barman-cloud tool suite | *[BarmanObjectStoreConfiguration](#barmanobjectstoreconfiguration) | false |


## BarmanObjectStoreConfiguration

BarmanObjectStoreConfiguration contains the backup configuration using Barman against an S3-compatible object storage

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| s3Credentials | The credentials to use to upload data to S3 | [S3Credentials](#s3credentials) | true |
| endpointURL | Endpoint to be used to upload data to the cloud, overriding the automatic endpoint discovery | string | false |
| destinationPath | The path where to store the backup (i.e. s3://bucket/path/to/folder) this path, with different destination folders, will be used for WALs and for data | string | true |
| serverName | The server name on S3, the cluster name is used if this parameter is omitted | string | false |
| wal | The configuration for the backup of the WAL stream. When not defined, WAL files will be stored uncompressed and may be unencrypted in the object store, according to the bucket default policy. | *[WalBackupConfiguration](#walbackupconfiguration) | false |
| data | The configuration to be used to backup the data files When not defined, base backups files will be stored uncompressed and may be unencrypted in the object store, according to the bucket default policy. | *[DataBackupConfiguration](#databackupconfiguration) | false |


## BootstrapConfiguration

BootstrapConfiguration contains information about how to create the PostgreSQL cluster. Only a single bootstrap method can be defined among the supported ones. `initdb` will be used as the bootstrap method if left unspecified. Refer to the Bootstrap page of the documentation for more information.

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| initdb | Bootstrap the cluster via initdb | *[BootstrapInitDB](#bootstrapinitdb) | false |
| recovery | Bootstrap the cluster from a backup | *[BootstrapRecovery](#bootstraprecovery) | false |


## BootstrapInitDB

BootstrapInitDB is the configuration of the bootstrap process when initdb is used Refer to the Bootstrap page of the documentation for more information.

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| database | Name of the database used by the application. Default: `app`. | string | true |
| owner | Name of the owner of the database in the instance to be used by applications. Defaults to the value of the `database` key. | string | true |
| secret | Name of the secret containing the initial credentials for the owner of the user database. If empty a new secret will be created from scratch | *corev1.LocalObjectReference | false |
| options | The list of options that must be passed to initdb when creating the cluster | []string | false |


## BootstrapRecovery

BootstrapRecovery contains the configuration required to restore the backup with the specified name and, after having changed the password with the one chosen for the superuser, will use it to bootstrap a full cluster cloning all the instances from the restored primary. Refer to the Bootstrap page of the documentation for more information.

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| backup | The backup we need to restore | corev1.LocalObjectReference | true |
| recoveryTarget | By default the recovery will end as soon as a consistent state is reached: in this case that means at the end of a backup. This option allows to fine tune the recovery process | *[RecoveryTarget](#recoverytarget) | false |


## Cluster

Cluster is the Schema for the postgresql API

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectmeta-v1-meta) | false |
| spec | Specification of the desired behavior of the cluster. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | [ClusterSpec](#clusterspec) | false |
| status | Most recently observed status of the cluster. This data may not be up to date. Populated by the system. Read-only. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | [ClusterStatus](#clusterstatus) | false |


## ClusterList

ClusterList contains a list of Cluster

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| metadata | Standard list metadata. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#listmeta-v1-meta) | false |
| items | List of clusters | [][Cluster](#cluster) | true |


## ClusterSpec

ClusterSpec defines the desired state of Cluster

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| description | Description of this PostgreSQL cluster | string | false |
| imageName | Name of the container image | string | false |
| postgresUID | The UID of the `postgres` user inside the image, defaults to `26` | int64 | false |
| postgresGID | The GID of the `postgres` user inside the image, defaults to `26` | int64 | false |
| instances | Number of instances required in the cluster | int32 | true |
| minSyncReplicas | Minimum number of instances required in synchronous replication with the primary. Undefined or 0 allow writes to complete when no standby is available. | int32 | false |
| maxSyncReplicas | The target value for the synchronous replication quorum, that can be decreased if the number of ready standbys is lower than this. Undefined or 0 disable synchronous replication. | int32 | false |
| postgresql | Configuration of the PostgreSQL server | [PostgresConfiguration](#postgresconfiguration) | false |
| bootstrap | Instructions to bootstrap this cluster | *[BootstrapConfiguration](#bootstrapconfiguration) | false |
| superuserSecret | The secret containing the superuser password. If not defined a new secret will be created with a randomly generated password | *corev1.LocalObjectReference | false |
| imagePullSecrets | The list of pull secrets to be used to pull the images | []corev1.LocalObjectReference | false |
| storage | Configuration of the storage of the instances | [StorageConfiguration](#storageconfiguration) | false |
| startDelay | The time in seconds that is allowed for a PostgreSQL instance to successfully start up (default 30) | int32 | false |
| stopDelay | The time in seconds that is allowed for a PostgreSQL instance node to gracefully shutdown (default 30) | int32 | false |
| affinity | Affinity/Anti-affinity rules for Pods | [AffinityConfiguration](#affinityconfiguration) | false |
| resources | Resources requirements of every generated Pod. Please refer to https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ for more information. | corev1.ResourceRequirements | false |
| primaryUpdateStrategy | Strategy to follow to upgrade the primary server during a rolling update procedure, after all replicas have been successfully updated: it can be automated (`unsupervised` - default) or manual (`supervised`) | PrimaryUpdateStrategy | false |
| backup | The configuration to be used for backups | *[BackupConfiguration](#backupconfiguration) | false |
| nodeMaintenanceWindow | Define a maintenance window for the Kubernetes nodes | *[NodeMaintenanceWindow](#nodemaintenancewindow) | false |


## ClusterStatus

ClusterStatus defines the observed state of Cluster

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| instances | Total number of instances in the cluster | int32 | false |
| readyInstances | Total number of ready instances in the cluster | int32 | false |
| instancesStatus | Instances status | map[utils.PodStatus][]string | false |
| latestGeneratedNode | ID of the latest generated node (used to avoid node name clashing) | int32 | false |
| currentPrimary | Current primary instance | string | false |
| targetPrimary | Target primary instance, this is different from the previous one during a switchover or a failover | string | false |
| pvcCount | How many PVCs have been created by this cluster | int32 | false |
| jobCount | How many Jobs have been created by this cluster | int32 | false |
| danglingPVC | List of all the PVCs created by this cluster and still available which are not attached to a Pod | []string | false |
| writeService | Current write pod | string | false |
| readService | Current list of read pods | string | false |
| phase | Current phase of the cluster | string | false |
| phaseReason | Reason for the current phase | string | false |


## DataBackupConfiguration

DataBackupConfiguration is the configuration of the backup of the data directory

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| compression | Compress a backup file (a tar file per tablespace) while streaming it to the object store. Available options are empty string (no compression, default), `gzip` or `bzip2`. | CompressionType | false |
| encryption | Whenever to force the encryption of files (if the bucket is not already configured for that). Allowed options are empty string (use the bucket policy, default), `AES256` and `aws:kms` | EncryptionType | false |
| immediateCheckpoint | Control whether the I/O workload for the backup initial checkpoint will be limited, according to the `checkpoint_completion_target` setting on the PostgreSQL server. If set to true, an immediate checkpoint will be used, meaning PostgreSQL will complete the checkpoint as soon as possible. `false` by default. | bool | false |
| jobs | The number of parallel jobs to be used to upload the backup, defaults to 2 | *int32 | false |


## NodeMaintenanceWindow

NodeMaintenanceWindow contains information that the operator will use while upgrading the underlying node.

This option is only useful when the chosen storage prevents the Pods from being freely moved across nodes.

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| inProgress | Is there a node maintenance activity in progress? | bool | true |
| reusePVC | Reuse the existing PVC (wait for the node to come up again) or not (recreate it elsewhere) | *bool | true |


## PostgresConfiguration

PostgresConfiguration defines the PostgreSQL configuration

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| parameters | PostgreSQL configuration options (postgresql.conf) | map[string]string | false |
| pg_hba | PostgreSQL Host Based Authentication rules (lines to be appended to the pg_hba.conf file) | []string | false |


## RecoveryTarget

RecoveryTarget allows to configure the moment where the recovery process will stop. All the target options except TargetTLI are mutually exclusive.

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| targetTLI | The target timeline (\"latest\", \"current\" or a positive integer) | string | false |
| targetXID | The target transaction ID | string | false |
| targetName | The target name (to be previously created with `pg_create_restore_point`) | string | false |
| targetLSN | The target LSN (Log Sequence Number) | string | false |
| targetTime | The target time, in any unambiguous representation allowed by PostgreSQL | string | false |
| targetImmediate | End recovery as soon as a consistent state is reached | *bool | false |
| exclusive | Set the target to be exclusive (defaults to true) | *bool | false |


## RollingUpdateStatus

RollingUpdateStatus contains the information about an instance which is being updated

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| imageName | The image which we put into the Pod | string | true |
| startedAt | When the update has been started | metav1.Time | false |


## S3Credentials

S3Credentials is the type for the credentials to be used to upload files to S3

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| accessKeyId | The reference to the access key id | corev1.SecretKeySelector | true |
| secretAccessKey | The reference to the secret access key | corev1.SecretKeySelector | true |


## StorageConfiguration

StorageConfiguration is the configuration of the storage of the PostgreSQL instances

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| storageClass | StorageClass to use for database data (`PGDATA`). Applied after evaluating the PVC template, if available. If not specified, generated PVCs will be satisfied by the default storage class | *string | false |
| size | Size of the storage. Required if not already specified in the PVC template. Changes to this field are automatically reapplied to the created PVCs. Size cannot be decreased. | string | true |
| resizeInUseVolumes | Resize existent PVCs, defaults to true | *bool | false |
| pvcTemplate | Template to be used to generate the Persistent Volume Claim | *corev1.PersistentVolumeClaimSpec | false |


## WalBackupConfiguration

WalBackupConfiguration is the configuration of the backup of the WAL stream

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| compression | Compress a WAL file before sending it to the object store. Available options are empty string (no compression, default), `gzip` or `bzip2`. | CompressionType | false |
| encryption | Whenever to force the encryption of files (if the bucket is not already configured for that). Allowed options are empty string (use the bucket policy, default), `AES256` and `aws:kms` | EncryptionType | false |


## ScheduledBackup

ScheduledBackup is the Schema for the scheduledbackups API

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectmeta-v1-meta) | false |
| spec | Specification of the desired behavior of the ScheduledBackup. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | [ScheduledBackupSpec](#scheduledbackupspec) | false |
| status | Most recently observed status of the ScheduledBackup. This data may not be up to date. Populated by the system. Read-only. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | [ScheduledBackupStatus](#scheduledbackupstatus) | false |


## ScheduledBackupList

ScheduledBackupList contains a list of ScheduledBackup

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| metadata | Standard list metadata. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#listmeta-v1-meta) | false |
| items | List of clusters | [][ScheduledBackup](#scheduledbackup) | true |


## ScheduledBackupSpec

ScheduledBackupSpec defines the desired state of ScheduledBackup

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| suspend | If this backup is suspended of not | *bool | false |
| schedule | The schedule in Cron format, see https://en.wikipedia.org/wiki/Cron. | string | true |
| cluster | The cluster to backup | [v1.LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#localobjectreference-v1-core) | false |


## ScheduledBackupStatus

ScheduledBackupStatus defines the observed state of ScheduledBackup

| Field | Description | Scheme | Required |
| -------------------- | ------------------------------ | -------------------- | -------- |
| lastCheckTime | The latest time the schedule | *metav1.Time | false |
| lastScheduleTime | Information when was the last time that backup was successfully scheduled. | *metav1.Time | false |

