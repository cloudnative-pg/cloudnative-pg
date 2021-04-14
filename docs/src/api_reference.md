# API Reference

Cloud Native PostgreSQL extends the Kubernetes API defining the following
custom resources:

-   [Backup](#backup)
-   [Cluster](#cluster)
-   [ScheduledBackup](#scheduledbackup)

All the resources are defined in the `postgresql.k8s.enterprisedb.io/v1`
API.

Please refer to the ["Configuration Samples" page](samples.md)" of the
documentation for examples of usage.

Below you will find a description of the defined resources:

<!-- Everything from now on is generated via `make apidoc` -->

- [AffinityConfiguration](#AffinityConfiguration)
- [Backup](#Backup)
- [BackupConfiguration](#BackupConfiguration)
- [BackupList](#BackupList)
- [BackupSpec](#BackupSpec)
- [BackupStatus](#BackupStatus)
- [BarmanObjectStoreConfiguration](#BarmanObjectStoreConfiguration)
- [BootstrapConfiguration](#BootstrapConfiguration)
- [BootstrapInitDB](#BootstrapInitDB)
- [BootstrapRecovery](#BootstrapRecovery)
- [Cluster](#Cluster)
- [ClusterList](#ClusterList)
- [ClusterSpec](#ClusterSpec)
- [ClusterStatus](#ClusterStatus)
- [DataBackupConfiguration](#DataBackupConfiguration)
- [MonitoringConfiguration](#MonitoringConfiguration)
- [NodeMaintenanceWindow](#NodeMaintenanceWindow)
- [PostgresConfiguration](#PostgresConfiguration)
- [RecoveryTarget](#RecoveryTarget)
- [RollingUpdateStatus](#RollingUpdateStatus)
- [S3Credentials](#S3Credentials)
- [ScheduledBackup](#ScheduledBackup)
- [ScheduledBackupList](#ScheduledBackupList)
- [ScheduledBackupSpec](#ScheduledBackupSpec)
- [ScheduledBackupStatus](#ScheduledBackupStatus)
- [SecretsResourceVersion](#SecretsResourceVersion)
- [StorageConfiguration](#StorageConfiguration)
- [WalBackupConfiguration](#WalBackupConfiguration)


## <a id='AffinityConfiguration'></a>`AffinityConfiguration`

AffinityConfiguration contains the info we need to create the affinity rules for Pods

Name                  | Description                                                                                                                                                              | Type             
--------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -----------------
`enablePodAntiAffinity` | Activates anti-affinity for the pods. The operator will define pods anti-affinity unless this field is explicitly set to false                                           | *bool            
`topologyKey          ` | TopologyKey to use for anti-affinity configuration. See k8s documentation for more info on that                                                                          - *mandatory*  | string           
`nodeSelector         ` | NodeSelector is map of key-value pairs used to define the nodes on which the pods can run. More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/ | map[string]string

## <a id='Backup'></a>`Backup`

Backup is the Schema for the backups API

Name     | Description                                                                                                                                                                                                                      | Type                                                                                                        
-------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------
`metadata` |                                                                                                                                                                                                                                  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#objectmeta-v1-meta)
`spec    ` | Specification of the desired behavior of the backup. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status                                                              | [BackupSpec](#BackupSpec)                                                                                   
`status  ` | Most recently observed status of the backup. This data may not be up to date. Populated by the system. Read-only. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | [BackupStatus](#BackupStatus)                                                                               

## <a id='BackupConfiguration'></a>`BackupConfiguration`

BackupConfiguration defines how the backup of the cluster are taken. Currently the only supported backup method is barmanObjectStore. For details and examples refer to the Backup and Recovery section of the documentation

Name              | Description                                       | Type                                                              
----------------- | ------------------------------------------------- | ------------------------------------------------------------------
`barmanObjectStore` | The configuration for the barman-cloud tool suite | [*BarmanObjectStoreConfiguration](#BarmanObjectStoreConfiguration)

## <a id='BackupList'></a>`BackupList`

BackupList contains a list of Backup

Name     | Description                                                                                                                        | Type                                                                                                    
-------- | ---------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------
`metadata` | Standard list metadata. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#listmeta-v1-meta)
`items   ` | List of backups                                                                                                                    - *mandatory*  | [[]Backup](#Backup)                                                                                     

## <a id='BackupSpec'></a>`BackupSpec`

BackupSpec defines the desired state of Backup

Name    | Description           | Type                                                                                                                        
------- | --------------------- | ----------------------------------------------------------------------------------------------------------------------------
`cluster` | The cluster to backup | [v1.LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#localobjectreference-v1-core)

## <a id='BackupStatus'></a>`BackupStatus`

BackupStatus defines the observed state of Backup

Name            | Description                                                                                                                                            | Type                                                                                             
--------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------
`s3Credentials  ` | The credentials to use to upload data to S3                                                                                                            - *mandatory*  | [S3Credentials](#S3Credentials)                                                                  
`endpointURL    ` | Endpoint to be used to upload data to the cloud, overriding the automatic endpoint discovery                                                           | string                                                                                           
`destinationPath` | The path where to store the backup (i.e. s3://bucket/path/to/folder) this path, with different destination folders, will be used for WALs and for data - *mandatory*  | string                                                                                           
`serverName     ` | The server name on S3, the cluster name is used if this parameter is omitted                                                                           | string                                                                                           
`encryption     ` | Encryption method required to S3 API                                                                                                                   | string                                                                                           
`backupId       ` | The ID of the Barman backup                                                                                                                            | string                                                                                           
`phase          ` | The last backup status                                                                                                                                 | BackupPhase                                                                                      
`startedAt      ` | When the backup was started                                                                                                                            | [*metav1.Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#time-v1-meta)
`stoppedAt      ` | When the backup was terminated                                                                                                                         | [*metav1.Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#time-v1-meta)
`error          ` | The detected error                                                                                                                                     | string                                                                                           
`commandOutput  ` | The backup command output                                                                                                                              | string                                                                                           
`commandError   ` | The backup command output                                                                                                                              | string                                                                                           

## <a id='BarmanObjectStoreConfiguration'></a>`BarmanObjectStoreConfiguration`

BarmanObjectStoreConfiguration contains the backup configuration using Barman against an S3-compatible object storage

Name            | Description                                                                                                                                                                                                | Type                                                
--------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------
`s3Credentials  ` | The credentials to use to upload data to S3                                                                                                                                                                - *mandatory*  | [S3Credentials](#S3Credentials)                     
`endpointURL    ` | Endpoint to be used to upload data to the cloud, overriding the automatic endpoint discovery                                                                                                               | string                                              
`destinationPath` | The path where to store the backup (i.e. s3://bucket/path/to/folder) this path, with different destination folders, will be used for WALs and for data                                                     - *mandatory*  | string                                              
`serverName     ` | The server name on S3, the cluster name is used if this parameter is omitted                                                                                                                               | string                                              
`wal            ` | The configuration for the backup of the WAL stream. When not defined, WAL files will be stored uncompressed and may be unencrypted in the object store, according to the bucket default policy.            | [*WalBackupConfiguration](#WalBackupConfiguration)  
`data           ` | The configuration to be used to backup the data files When not defined, base backups files will be stored uncompressed and may be unencrypted in the object store, according to the bucket default policy. | [*DataBackupConfiguration](#DataBackupConfiguration)

## <a id='BootstrapConfiguration'></a>`BootstrapConfiguration`

BootstrapConfiguration contains information about how to create the PostgreSQL cluster. Only a single bootstrap method can be defined among the supported ones. `initdb` will be used as the bootstrap method if left unspecified. Refer to the Bootstrap page of the documentation for more information.

Name     | Description                         | Type                                    
-------- | ----------------------------------- | ----------------------------------------
`initdb  ` | Bootstrap the cluster via initdb    | [*BootstrapInitDB](#BootstrapInitDB)    
`recovery` | Bootstrap the cluster from a backup | [*BootstrapRecovery](#BootstrapRecovery)

## <a id='BootstrapInitDB'></a>`BootstrapInitDB`

BootstrapInitDB is the configuration of the bootstrap process when initdb is used Refer to the Bootstrap page of the documentation for more information.

Name     | Description                                                                                                                                  | Type                                                                                                                             
-------- | -------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------
`database` | Name of the database used by the application. Default: `app`.                                                                                - *mandatory*  | string                                                                                                                           
`owner   ` | Name of the owner of the database in the instance to be used by applications. Defaults to the value of the `database` key.                   - *mandatory*  | string                                                                                                                           
`secret  ` | Name of the secret containing the initial credentials for the owner of the user database. If empty a new secret will be created from scratch | [*corev1.LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#localobjectreference-v1-core)
`options ` | The list of options that must be passed to initdb when creating the cluster                                                                  | []string                                                                                                                         

## <a id='BootstrapRecovery'></a>`BootstrapRecovery`

BootstrapRecovery contains the configuration required to restore the backup with the specified name and, after having changed the password with the one chosen for the superuser, will use it to bootstrap a full cluster cloning all the instances from the restored primary. Refer to the Bootstrap page of the documentation for more information.

Name           | Description                                                                                                                                                                     | Type                                                                                                                            
-------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------
`backup        ` | The backup we need to restore                                                                                                                                                   - *mandatory*  | [corev1.LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#localobjectreference-v1-core)
`recoveryTarget` | By default the recovery will end as soon as a consistent state is reached: in this case that means at the end of a backup. This option allows to fine tune the recovery process | [*RecoveryTarget](#RecoveryTarget)                                                                                              

## <a id='Cluster'></a>`Cluster`

Cluster is the Schema for the PostgreSQL API

Name     | Description                                                                                                                                                                                                                       | Type                                                                                                        
-------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------
`metadata` |                                                                                                                                                                                                                                   | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#objectmeta-v1-meta)
`spec    ` | Specification of the desired behavior of the cluster. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status                                                              | [ClusterSpec](#ClusterSpec)                                                                                 
`status  ` | Most recently observed status of the cluster. This data may not be up to date. Populated by the system. Read-only. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | [ClusterStatus](#ClusterStatus)                                                                             

## <a id='ClusterList'></a>`ClusterList`

ClusterList contains a list of Cluster

Name     | Description                                                                                                                        | Type                                                                                                    
-------- | ---------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------
`metadata` | Standard list metadata. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#listmeta-v1-meta)
`items   ` | List of clusters                                                                                                                   - *mandatory*  | [[]Cluster](#Cluster)                                                                                   

## <a id='ClusterSpec'></a>`ClusterSpec`

ClusterSpec defines the desired state of Cluster

Name                  | Description                                                                                                                                                                                                    | Type                                                                                                                              
--------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------
`description          ` | Description of this PostgreSQL cluster                                                                                                                                                                         | string                                                                                                                            
`imageName            ` | Name of the container image                                                                                                                                                                                    | string                                                                                                                            
`postgresUID          ` | The UID of the `postgres` user inside the image, defaults to `26`                                                                                                                                              | int64                                                                                                                             
`postgresGID          ` | The GID of the `postgres` user inside the image, defaults to `26`                                                                                                                                              | int64                                                                                                                             
`instances            ` | Number of instances required in the cluster                                                                                                                                                                    - *mandatory*  | int32                                                                                                                             
`minSyncReplicas      ` | Minimum number of instances required in synchronous replication with the primary. Undefined or 0 allow writes to complete when no standby is available.                                                        | int32                                                                                                                             
`maxSyncReplicas      ` | The target value for the synchronous replication quorum, that can be decreased if the number of ready standbys is lower than this. Undefined or 0 disable synchronous replication.                             | int32                                                                                                                             
`postgresql           ` | Configuration of the PostgreSQL server                                                                                                                                                                         | [PostgresConfiguration](#PostgresConfiguration)                                                                                   
`bootstrap            ` | Instructions to bootstrap this cluster                                                                                                                                                                         | [*BootstrapConfiguration](#BootstrapConfiguration)                                                                                
`superuserSecret      ` | The secret containing the superuser password. If not defined a new secret will be created with a randomly generated password                                                                                   | [*corev1.LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#localobjectreference-v1-core) 
`imagePullSecrets     ` | The list of pull secrets to be used to pull the images                                                                                                                                                         | [[]corev1.LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#localobjectreference-v1-core)
`storage              ` | Configuration of the storage of the instances                                                                                                                                                                  | [StorageConfiguration](#StorageConfiguration)                                                                                     
`startDelay           ` | The time in seconds that is allowed for a PostgreSQL instance to successfully start up (default 30)                                                                                                            | int32                                                                                                                             
`stopDelay            ` | The time in seconds that is allowed for a PostgreSQL instance node to gracefully shutdown (default 30)                                                                                                         | int32                                                                                                                             
`affinity             ` | Affinity/Anti-affinity rules for Pods                                                                                                                                                                          | [AffinityConfiguration](#AffinityConfiguration)                                                                                   
`resources            ` | Resources requirements of every generated Pod. Please refer to https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ for more information.                                            | [corev1.ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#resourcerequirements-v1-core)  
`primaryUpdateStrategy` | Strategy to follow to upgrade the primary server during a rolling update procedure, after all replicas have been successfully updated: it can be automated (`unsupervised` - default) or manual (`supervised`) | PrimaryUpdateStrategy                                                                                                             
`backup               ` | The configuration to be used for backups                                                                                                                                                                       | [*BackupConfiguration](#BackupConfiguration)                                                                                      
`nodeMaintenanceWindow` | Define a maintenance window for the Kubernetes nodes                                                                                                                                                           | [*NodeMaintenanceWindow](#NodeMaintenanceWindow)                                                                                  
`monitoring           ` | The configuration of the monitoring infrastructure of this cluster                                                                                                                                             | [*MonitoringConfiguration](#MonitoringConfiguration)                                                                              

## <a id='ClusterStatus'></a>`ClusterStatus`

ClusterStatus defines the observed state of Cluster

Name                   | Description                                                                                                                                                                 | Type                                             
---------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------
`instances             ` | Total number of instances in the cluster                                                                                                                                    | int32                                            
`readyInstances        ` | Total number of ready instances in the cluster                                                                                                                              | int32                                            
`instancesStatus       ` | Instances status                                                                                                                                                            | map[utils.PodStatus][]string                     
`latestGeneratedNode   ` | ID of the latest generated node (used to avoid node name clashing)                                                                                                          | int32                                            
`currentPrimary        ` | Current primary instance                                                                                                                                                    | string                                           
`targetPrimary         ` | Target primary instance, this is different from the previous one during a switchover or a failover                                                                          | string                                           
`pvcCount              ` | How many PVCs have been created by this cluster                                                                                                                             | int32                                            
`jobCount              ` | How many Jobs have been created by this cluster                                                                                                                             | int32                                            
`danglingPVC           ` | List of all the PVCs created by this cluster and still available which are not attached to a Pod                                                                            | []string                                         
`initializingPVC       ` | List of all the PVCs that are being initialized by this cluster                                                                                                             | []string                                         
`writeService          ` | Current write pod                                                                                                                                                           | string                                           
`readService           ` | Current list of read pods                                                                                                                                                   | string                                           
`phase                 ` | Current phase of the cluster                                                                                                                                                | string                                           
`phaseReason           ` | Reason for the current phase                                                                                                                                                | string                                           
`secretsResourceVersion` | The list of resource versions of the secrets managed by the operator. Every change here is done in the interest of the instance manager, which will refresh the secret data | [SecretsResourceVersion](#SecretsResourceVersion)

## <a id='DataBackupConfiguration'></a>`DataBackupConfiguration`

DataBackupConfiguration is the configuration of the backup of the data directory

Name                | Description                                                                                                                                                                                                                                                                                                          | Type           
------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------
`compression        ` | Compress a backup file (a tar file per tablespace) while streaming it to the object store. Available options are empty string (no compression, default), `gzip` or `bzip2`.                                                                                                                                          | CompressionType
`encryption         ` | Whenever to force the encryption of files (if the bucket is not already configured for that). Allowed options are empty string (use the bucket policy, default), `AES256` and `aws:kms`                                                                                                                              | EncryptionType 
`immediateCheckpoint` | Control whether the I/O workload for the backup initial checkpoint will be limited, according to the `checkpoint_completion_target` setting on the PostgreSQL server. If set to true, an immediate checkpoint will be used, meaning PostgreSQL will complete the checkpoint as soon as possible. `false` by default. | bool           
`jobs               ` | The number of parallel jobs to be used to upload the backup, defaults to 2                                                                                                                                                                                                                                           | *int32         

## <a id='MonitoringConfiguration'></a>`MonitoringConfiguration`

MonitoringConfiguration is the type containing all the monitoring configuration for a certain cluster

Name                   | Description                                           | Type                                                                                                                              
---------------------- | ----------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------
`customQueriesConfigMap` | The list of config maps containing the custom queries | [[]corev1.ConfigMapKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#configmapkeyselector-v1-core)
`customQueriesSecret   ` | The list of secrets containing the custom queries     | [[]corev1.SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#secretkeyselector-v1-core)      

## <a id='NodeMaintenanceWindow'></a>`NodeMaintenanceWindow`

NodeMaintenanceWindow contains information that the operator will use while upgrading the underlying node.

This option is only useful when the chosen storage prevents the Pods from being freely moved across nodes.

Name       | Description                                                                                | Type 
---------- | ------------------------------------------------------------------------------------------ | -----
`inProgress` | Is there a node maintenance activity in progress?                                          - *mandatory*  | bool 
`reusePVC  ` | Reuse the existing PVC (wait for the node to come up again) or not (recreate it elsewhere) - *mandatory*  | *bool

## <a id='PostgresConfiguration'></a>`PostgresConfiguration`

PostgresConfiguration defines the PostgreSQL configuration

Name       | Description                                                                               | Type             
---------- | ----------------------------------------------------------------------------------------- | -----------------
`parameters` | PostgreSQL configuration options (postgresql.conf)                                        | map[string]string
`pg_hba    ` | PostgreSQL Host Based Authentication rules (lines to be appended to the pg_hba.conf file) | []string         

## <a id='RecoveryTarget'></a>`RecoveryTarget`

RecoveryTarget allows to configure the moment where the recovery process will stop. All the target options except TargetTLI are mutually exclusive.

Name            | Description                                                               | Type  
--------------- | ------------------------------------------------------------------------- | ------
`targetTLI      ` | The target timeline ("latest", "current" or a positive integer)           | string
`targetXID      ` | The target transaction ID                                                 | string
`targetName     ` | The target name (to be previously created with `pg_create_restore_point`) | string
`targetLSN      ` | The target LSN (Log Sequence Number)                                      | string
`targetTime     ` | The target time, in any unambiguous representation allowed by PostgreSQL  | string
`targetImmediate` | End recovery as soon as a consistent state is reached                     | *bool 
`exclusive      ` | Set the target to be exclusive (defaults to true)                         | *bool 

## <a id='RollingUpdateStatus'></a>`RollingUpdateStatus`

RollingUpdateStatus contains the information about an instance which is being updated

Name      | Description                         | Type                                                                                            
--------- | ----------------------------------- | ------------------------------------------------------------------------------------------------
`imageName` | The image which we put into the Pod - *mandatory*  | string                                                                                          
`startedAt` | When the update has been started    | [metav1.Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#time-v1-meta)

## <a id='S3Credentials'></a>`S3Credentials`

S3Credentials is the type for the credentials to be used to upload files to S3

Name            | Description                            | Type                                                                                                                      
--------------- | -------------------------------------- | --------------------------------------------------------------------------------------------------------------------------
`accessKeyId    ` | The reference to the access key id     - *mandatory*  | [corev1.SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#secretkeyselector-v1-core)
`secretAccessKey` | The reference to the secret access key - *mandatory*  | [corev1.SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#secretkeyselector-v1-core)

## <a id='ScheduledBackup'></a>`ScheduledBackup`

ScheduledBackup is the Schema for the scheduledbackups API

Name     | Description                                                                                                                                                                                                                               | Type                                                                                                        
-------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------
`metadata` |                                                                                                                                                                                                                                           | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#objectmeta-v1-meta)
`spec    ` | Specification of the desired behavior of the ScheduledBackup. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status                                                              | [ScheduledBackupSpec](#ScheduledBackupSpec)                                                                 
`status  ` | Most recently observed status of the ScheduledBackup. This data may not be up to date. Populated by the system. Read-only. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | [ScheduledBackupStatus](#ScheduledBackupStatus)                                                             

## <a id='ScheduledBackupList'></a>`ScheduledBackupList`

ScheduledBackupList contains a list of ScheduledBackup

Name     | Description                                                                                                                        | Type                                                                                                    
-------- | ---------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------
`metadata` | Standard list metadata. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#listmeta-v1-meta)
`items   ` | List of clusters                                                                                                                   - *mandatory*  | [[]ScheduledBackup](#ScheduledBackup)                                                                   

## <a id='ScheduledBackupSpec'></a>`ScheduledBackupSpec`

ScheduledBackupSpec defines the desired state of ScheduledBackup

Name     | Description                                                          | Type                                                                                                                        
-------- | -------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------
`suspend ` | If this backup is suspended of not                                   | *bool                                                                                                                       
`schedule` | The schedule in Cron format, see https://en.wikipedia.org/wiki/Cron. - *mandatory*  | string                                                                                                                      
`cluster ` | The cluster to backup                                                | [v1.LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#localobjectreference-v1-core)

## <a id='ScheduledBackupStatus'></a>`ScheduledBackupStatus`

ScheduledBackupStatus defines the observed state of ScheduledBackup

Name             | Description                                                                | Type                                                                                             
---------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------
`lastCheckTime   ` | The latest time the schedule                                               | [*metav1.Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#time-v1-meta)
`lastScheduleTime` | Information when was the last time that backup was successfully scheduled. | [*metav1.Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#time-v1-meta)
`nextScheduleTime` | Next time we will run a backup                                             | [*metav1.Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#time-v1-meta)

## <a id='SecretsResourceVersion'></a>`SecretsResourceVersion`

SecretsResourceVersion is the resource versions of the secrets managed by the operator

Name                     | Description                                                       | Type  
------------------------ | ----------------------------------------------------------------- | ------
`superuserSecretVersion  ` | The resource version of the "postgres" user secret                - *mandatory*  | string
`replicationSecretVersion` | The resource version of the "streaming_replication" user secret   - *mandatory*  | string
`applicationSecretVersion` | The resource version of the "app" user secret                     - *mandatory*  | string
`caSecretVersion         ` | The resource version of the "ca" secret version                   - *mandatory*  | string
`serverSecretVersion     ` | The resource version of the PostgreSQL server-side secret version - *mandatory*  | string

## <a id='StorageConfiguration'></a>`StorageConfiguration`

StorageConfiguration is the configuration of the storage of the PostgreSQL instances

Name               | Description                                                                                                                                                                                | Type                                                                                                                                   
------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------
`storageClass      ` | StorageClass to use for database data (`PGDATA`). Applied after evaluating the PVC template, if available. If not specified, generated PVCs will be satisfied by the default storage class | *string                                                                                                                                
`size              ` | Size of the storage. Required if not already specified in the PVC template. Changes to this field are automatically reapplied to the created PVCs. Size cannot be decreased.               - *mandatory*  | string                                                                                                                                 
`resizeInUseVolumes` | Resize existent PVCs, defaults to true                                                                                                                                                     | *bool                                                                                                                                  
`pvcTemplate       ` | Template to be used to generate the Persistent Volume Claim                                                                                                                                | [*corev1.PersistentVolumeClaimSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#persistentvolumeclaim-v1-core)

## <a id='WalBackupConfiguration'></a>`WalBackupConfiguration`

WalBackupConfiguration is the configuration of the backup of the WAL stream

Name        | Description                                                                                                                                                                             | Type           
----------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------
`compression` | Compress a WAL file before sending it to the object store. Available options are empty string (no compression, default), `gzip` or `bzip2`.                                             | CompressionType
`encryption ` | Whenever to force the encryption of files (if the bucket is not already configured for that). Allowed options are empty string (use the bucket policy, default), `AES256` and `aws:kms` | EncryptionType 

