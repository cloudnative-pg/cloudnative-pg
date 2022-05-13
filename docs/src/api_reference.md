# API Reference

CloudNativePG extends the Kubernetes API defining the following
custom resources:

-   [Backup](#backup)
-   [Cluster](#cluster)
-   [Pooler](#pooler)
-   [ScheduledBackup](#scheduledbackup)

All the resources are defined in the `postgresql.cnpg.io/v1`
API.

Please refer to the ["Configuration Samples" page](samples.md)" of the
documentation for examples of usage.

Below you will find a description of the defined resources:

<!-- Everything from now on is generated via `make apidoc` -->

- [AffinityConfiguration](#AffinityConfiguration)
- [AzureCredentials](#AzureCredentials)
- [Backup](#Backup)
- [BackupConfiguration](#BackupConfiguration)
- [BackupList](#BackupList)
- [BackupSource](#BackupSource)
- [BackupSpec](#BackupSpec)
- [BackupStatus](#BackupStatus)
- [BarmanObjectStoreConfiguration](#BarmanObjectStoreConfiguration)
- [BootstrapConfiguration](#BootstrapConfiguration)
- [BootstrapInitDB](#BootstrapInitDB)
- [BootstrapPgBaseBackup](#BootstrapPgBaseBackup)
- [BootstrapRecovery](#BootstrapRecovery)
- [CertificatesConfiguration](#CertificatesConfiguration)
- [CertificatesStatus](#CertificatesStatus)
- [Cluster](#Cluster)
- [ClusterList](#ClusterList)
- [ClusterSpec](#ClusterSpec)
- [ClusterStatus](#ClusterStatus)
- [ConfigMapKeySelector](#ConfigMapKeySelector)
- [ConfigMapResourceVersion](#ConfigMapResourceVersion)
- [DataBackupConfiguration](#DataBackupConfiguration)
- [EmbeddedObjectMetadata](#EmbeddedObjectMetadata)
- [ExternalCluster](#ExternalCluster)
- [GoogleCredentials](#GoogleCredentials)
- [InstanceID](#InstanceID)
- [LDAPBindAsAuth](#LDAPBindAsAuth)
- [LDAPBindSearchAuth](#LDAPBindSearchAuth)
- [LDAPConfig](#LDAPConfig)
- [LocalObjectReference](#LocalObjectReference)
- [MonitoringConfiguration](#MonitoringConfiguration)
- [NodeMaintenanceWindow](#NodeMaintenanceWindow)
- [PgBouncerIntegrationStatus](#PgBouncerIntegrationStatus)
- [PgBouncerSecrets](#PgBouncerSecrets)
- [PgBouncerSpec](#PgBouncerSpec)
- [PodMeta](#PodMeta)
- [PodTemplateSpec](#PodTemplateSpec)
- [Pooler](#Pooler)
- [PoolerIntegrations](#PoolerIntegrations)
- [PoolerList](#PoolerList)
- [PoolerSecrets](#PoolerSecrets)
- [PoolerSpec](#PoolerSpec)
- [PoolerStatus](#PoolerStatus)
- [PostgresConfiguration](#PostgresConfiguration)
- [RecoveryTarget](#RecoveryTarget)
- [ReplicaClusterConfiguration](#ReplicaClusterConfiguration)
- [RollingUpdateStatus](#RollingUpdateStatus)
- [S3Credentials](#S3Credentials)
- [ScheduledBackup](#ScheduledBackup)
- [ScheduledBackupList](#ScheduledBackupList)
- [ScheduledBackupSpec](#ScheduledBackupSpec)
- [ScheduledBackupStatus](#ScheduledBackupStatus)
- [SecretKeySelector](#SecretKeySelector)
- [SecretVersion](#SecretVersion)
- [SecretsResourceVersion](#SecretsResourceVersion)
- [StorageConfiguration](#StorageConfiguration)
- [WalBackupConfiguration](#WalBackupConfiguration)


<a id='AffinityConfiguration'></a>

## AffinityConfiguration

AffinityConfiguration contains the info we need to create the affinity rules for Pods

Name                      | Description                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | Type                   
------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -----------------------
`enablePodAntiAffinity    ` | Activates anti-affinity for the pods. The operator will define pods anti-affinity unless this field is explicitly set to false                                                                                                                                                                                                                                                                                                                                                                                                                      | *bool                  
`topologyKey              ` | TopologyKey to use for anti-affinity configuration. See k8s documentation for more info on that                                                                                                                                                                                                                                                                                                                                                                                                                                                     - *mandatory*  | string                 
`nodeSelector             ` | NodeSelector is map of key-value pairs used to define the nodes on which the pods can run. More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/                                                                                                                                                                                                                                                                                                                                                                            | map[string]string      
`tolerations              ` | Tolerations is a list of Tolerations that should be set for all the pods, in order to allow them to run on tainted nodes. More info: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/                                                                                                                                                                                                                                                                                                                                  | []corev1.Toleration    
`podAntiAffinityType      ` | PodAntiAffinityType allows the user to decide whether pod anti-affinity between cluster instance has to be considered a strong requirement during scheduling or not. Allowed values are: "preferred" (default if empty) or "required". Setting it to "required", could lead to instances remaining pending until new kubernetes nodes are added if all the existing nodes don't match the required pod anti-affinity rule. More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#inter-pod-affinity-and-anti-affinity | string                 
`additionalPodAntiAffinity` | AdditionalPodAntiAffinity allows to specify pod anti-affinity terms to be added to the ones generated by the operator if EnablePodAntiAffinity is set to true (default) or to be used exclusively if set to false.                                                                                                                                                                                                                                                                                                                                  | *corev1.PodAntiAffinity
`additionalPodAffinity    ` | AdditionalPodAffinity allows to specify pod affinity terms to be passed to all the cluster's pods.                                                                                                                                                                                                                                                                                                                                                                                                                                                  | *corev1.PodAffinity    

<a id='AzureCredentials'></a>

## AzureCredentials

AzureCredentials is the type for the credentials to be used to upload files to Azure Blob Storage. The connection string contains every needed information. If the connection string is not specified, we'll need the storage account name and also one (and only one) of:

- storageKey - storageSasToken

Name             | Description                                                                       | Type                                    
---------------- | --------------------------------------------------------------------------------- | ----------------------------------------
`connectionString` | The connection string to be used                                                  | [*SecretKeySelector](#SecretKeySelector)
`storageAccount  ` | The storage account where to upload data                                          | [*SecretKeySelector](#SecretKeySelector)
`storageKey      ` | The storage account key to be used in conjunction with the storage account name   | [*SecretKeySelector](#SecretKeySelector)
`storageSasToken ` | A shared-access-signature to be used in conjunction with the storage account name | [*SecretKeySelector](#SecretKeySelector)

<a id='Backup'></a>

## Backup

Backup is the Schema for the backups API

Name     | Description                                                                                                                                                                                                                      | Type                                                                                                        
-------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------
`metadata` |                                                                                                                                                                                                                                  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#objectmeta-v1-meta)
`spec    ` | Specification of the desired behavior of the backup. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status                                                              | [BackupSpec](#BackupSpec)                                                                                   
`status  ` | Most recently observed status of the backup. This data may not be up to date. Populated by the system. Read-only. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | [BackupStatus](#BackupStatus)                                                                               

<a id='BackupConfiguration'></a>

## BackupConfiguration

BackupConfiguration defines how the backup of the cluster are taken. Currently the only supported backup method is barmanObjectStore. For details and examples refer to the Backup and Recovery section of the documentation

Name              | Description                                                                                                                                                                                                                | Type                                                              
----------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------
`barmanObjectStore` | The configuration for the barman-cloud tool suite                                                                                                                                                                          | [*BarmanObjectStoreConfiguration](#BarmanObjectStoreConfiguration)
`retentionPolicy  ` | RetentionPolicy is the retention policy to be used for backups and WALs (i.e. '60d'). The retention policy is expressed in the form of `XXu` where `XX` is a positive integer and `u` is in `[dwm]` - days, weeks, months. | string                                                            

<a id='BackupList'></a>

## BackupList

BackupList contains a list of Backup

Name     | Description                                                                                                                        | Type                                                                                                    
-------- | ---------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------
`metadata` | Standard list metadata. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#listmeta-v1-meta)
`items   ` | List of backups                                                                                                                    - *mandatory*  | [[]Backup](#Backup)                                                                                     

<a id='BackupSource'></a>

## BackupSource

BackupSource contains the backup we need to restore from, plus some information that could be needed to correctly restore it.

Name       | Description                                                                                                                                                             | Type                                    
---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------
`endpointCA` | EndpointCA store the CA bundle of the barman endpoint. Useful when using self-signed certificates to avoid errors with certificate issuer and barman-cloud-wal-archive. | [*SecretKeySelector](#SecretKeySelector)

<a id='BackupSpec'></a>

## BackupSpec

BackupSpec defines the desired state of Backup

Name    | Description           | Type                                         
------- | --------------------- | ---------------------------------------------
`cluster` | The cluster to backup | [LocalObjectReference](#LocalObjectReference)

<a id='BackupStatus'></a>

## BackupStatus

BackupStatus defines the observed state of Backup

Name              | Description                                                                                                                                                             | Type                                                                                             
----------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------
`s3Credentials    ` | The credentials to be used to upload data to S3                                                                                                                         | [*S3Credentials](#S3Credentials)                                                                 
`azureCredentials ` | The credentials to be used to upload data to Azure Blob Storage                                                                                                         | [*AzureCredentials](#AzureCredentials)                                                           
`googleCredentials` | The credentials to use to upload data to Google Cloud Storage                                                                                                           | [*GoogleCredentials](#GoogleCredentials)                                                         
`endpointCA       ` | EndpointCA store the CA bundle of the barman endpoint. Useful when using self-signed certificates to avoid errors with certificate issuer and barman-cloud-wal-archive. | [*SecretKeySelector](#SecretKeySelector)                                                         
`endpointURL      ` | Endpoint to be used to upload data to the cloud, overriding the automatic endpoint discovery                                                                            | string                                                                                           
`destinationPath  ` | The path where to store the backup (i.e. s3://bucket/path/to/folder) this path, with different destination folders, will be used for WALs and for data                  - *mandatory*  | string                                                                                           
`serverName       ` | The server name on S3, the cluster name is used if this parameter is omitted                                                                                            | string                                                                                           
`encryption       ` | Encryption method required to S3 API                                                                                                                                    | string                                                                                           
`backupId         ` | The ID of the Barman backup                                                                                                                                             | string                                                                                           
`phase            ` | The last backup status                                                                                                                                                  | BackupPhase                                                                                      
`startedAt        ` | When the backup was started                                                                                                                                             | [*metav1.Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#time-v1-meta)
`stoppedAt        ` | When the backup was terminated                                                                                                                                          | [*metav1.Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#time-v1-meta)
`beginWal         ` | The starting WAL                                                                                                                                                        | string                                                                                           
`endWal           ` | The ending WAL                                                                                                                                                          | string                                                                                           
`beginLSN         ` | The starting xlog                                                                                                                                                       | string                                                                                           
`endLSN           ` | The ending xlog                                                                                                                                                         | string                                                                                           
`error            ` | The detected error                                                                                                                                                      | string                                                                                           
`commandOutput    ` | Unused. Retained for compatibility with old versions.                                                                                                                   | string                                                                                           
`commandError     ` | The backup command output in case of error                                                                                                                              | string                                                                                           
`instanceID       ` | Information to identify the instance where the backup has been taken from                                                                                               | [*InstanceID](#InstanceID)                                                                       

<a id='BarmanObjectStoreConfiguration'></a>

## BarmanObjectStoreConfiguration

BarmanObjectStoreConfiguration contains the backup configuration using Barman against an S3-compatible object storage

Name              | Description                                                                                                                                                                                                | Type                                                
----------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------
`s3Credentials    ` | The credentials to use to upload data to S3                                                                                                                                                                | [*S3Credentials](#S3Credentials)                    
`azureCredentials ` | The credentials to use to upload data to Azure Blob Storage                                                                                                                                                | [*AzureCredentials](#AzureCredentials)              
`googleCredentials` | The credentials to use to upload data to Google Cloud Storage                                                                                                                                              | [*GoogleCredentials](#GoogleCredentials)            
`endpointURL      ` | Endpoint to be used to upload data to the cloud, overriding the automatic endpoint discovery                                                                                                               | string                                              
`endpointCA       ` | EndpointCA store the CA bundle of the barman endpoint. Useful when using self-signed certificates to avoid errors with certificate issuer and barman-cloud-wal-archive                                     | [*SecretKeySelector](#SecretKeySelector)            
`destinationPath  ` | The path where to store the backup (i.e. s3://bucket/path/to/folder) this path, with different destination folders, will be used for WALs and for data                                                     - *mandatory*  | string                                              
`serverName       ` | The server name on S3, the cluster name is used if this parameter is omitted                                                                                                                               | string                                              
`wal              ` | The configuration for the backup of the WAL stream. When not defined, WAL files will be stored uncompressed and may be unencrypted in the object store, according to the bucket default policy.            | [*WalBackupConfiguration](#WalBackupConfiguration)  
`data             ` | The configuration to be used to backup the data files When not defined, base backups files will be stored uncompressed and may be unencrypted in the object store, according to the bucket default policy. | [*DataBackupConfiguration](#DataBackupConfiguration)
`tags             ` | Tags is a list of key value pairs that will be passed to the Barman --tags option.                                                                                                                         | map[string]string                                   
`historyTags      ` | HistoryTags is a list of key value pairs that will be passed to the Barman --history-tags option.                                                                                                          | map[string]string                                   

<a id='BootstrapConfiguration'></a>

## BootstrapConfiguration

BootstrapConfiguration contains information about how to create the PostgreSQL cluster. Only a single bootstrap method can be defined among the supported ones. `initdb` will be used as the bootstrap method if left unspecified. Refer to the Bootstrap page of the documentation for more information.

Name          | Description                                                                              | Type                                            
------------- | ---------------------------------------------------------------------------------------- | ------------------------------------------------
`initdb       ` | Bootstrap the cluster via initdb                                                         | [*BootstrapInitDB](#BootstrapInitDB)            
`recovery     ` | Bootstrap the cluster from a backup                                                      | [*BootstrapRecovery](#BootstrapRecovery)        
`pg_basebackup` | Bootstrap the cluster taking a physical backup of another compatible PostgreSQL instance | [*BootstrapPgBaseBackup](#BootstrapPgBaseBackup)

<a id='BootstrapInitDB'></a>

## BootstrapInitDB

BootstrapInitDB is the configuration of the bootstrap process when initdb is used Refer to the Bootstrap page of the documentation for more information.

Name                   | Description                                                                                                                                                                                                                                | Type                                          
---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------
`database              ` | Name of the database used by the application. Default: `app`.                                                                                                                                                                              - *mandatory*  | string                                        
`owner                 ` | Name of the owner of the database in the instance to be used by applications. Defaults to the value of the `database` key.                                                                                                                 - *mandatory*  | string                                        
`secret                ` | Name of the secret containing the initial credentials for the owner of the user database. If empty a new secret will be created from scratch                                                                                               | [*LocalObjectReference](#LocalObjectReference)
`options               ` | The list of options that must be passed to initdb when creating the cluster. Deprecated: This could lead to inconsistent configurations, please use the explicit provided parameters instead. If defined, explicit values will be ignored. | []string                                      
`dataChecksums         ` | Whether the `-k` option should be passed to initdb, enabling checksums on data pages (default: `false`)                                                                                                                                    | *bool                                         
`encoding              ` | The value to be passed as option `--encoding` for initdb (default:`UTF8`)                                                                                                                                                                  | string                                        
`localeCollate         ` | The value to be passed as option `--lc-collate` for initdb (default:`C`)                                                                                                                                                                   | string                                        
`localeCType           ` | The value to be passed as option `--lc-ctype` for initdb (default:`C`)                                                                                                                                                                     | string                                        
`walSegmentSize        ` | The value in megabytes (1 to 1024) to be passed to the `--wal-segsize` option for initdb (default: empty, resulting in PostgreSQL default: 16MB)                                                                                           | int                                           
`postInitSQL           ` | List of SQL queries to be executed as a superuser immediately after the cluster has been created - to be used with extreme care (by default empty)                                                                                         | []string                                      
`postInitApplicationSQL` | List of SQL queries to be executed as a superuser in the application database right after is created - to be used with extreme care (by default empty)                                                                                     | []string                                      
`postInitTemplateSQL   ` | List of SQL queries to be executed as a superuser in the `template1` after the cluster has been created - to be used with extreme care (by default empty)                                                                                  | []string                                      

<a id='BootstrapPgBaseBackup'></a>

## BootstrapPgBaseBackup

BootstrapPgBaseBackup contains the configuration required to take a physical backup of an existing PostgreSQL cluster

Name   | Description                                                       | Type  
------ | ----------------------------------------------------------------- | ------
`source` | The name of the server of which we need to take a physical backup - *mandatory*  | string

<a id='BootstrapRecovery'></a>

## BootstrapRecovery

BootstrapRecovery contains the configuration required to restore the backup with the specified name and, after having changed the password with the one chosen for the superuser, will use it to bootstrap a full cluster cloning all the instances from the restored primary. Refer to the Bootstrap page of the documentation for more information.

Name           | Description                                                                                                                                                                                                                                                                                                                                                                                                                                             | Type                              
-------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------
`backup        ` | The backup we need to restore                                                                                                                                                                                                                                                                                                                                                                                                                           | [*BackupSource](#BackupSource)    
`source        ` | The external cluster whose backup we will restore. This is also used as the name of the folder under which the backup is stored, so it must be set to the name of the source cluster                                                                                                                                                                                                                                                                    | string                            
`recoveryTarget` | By default, the recovery process applies all the available WAL files in the archive (full recovery). However, you can also end the recovery as soon as a consistent state is reached or recover to a point-in-time (PITR) by specifying a `RecoveryTarget` object, as expected by PostgreSQL (i.e., timestamp, transaction Id, LSN, ...). More info: https://www.postgresql.org/docs/current/runtime-config-wal.html#RUNTIME-CONFIG-WAL-RECOVERY-TARGET | [*RecoveryTarget](#RecoveryTarget)

<a id='CertificatesConfiguration'></a>

## CertificatesConfiguration

CertificatesConfiguration contains the needed configurations to handle server certificates.

Name                 | Description                                                                                                                                                                                                                                                                                                                                                                                                                                                              | Type    
-------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------
`serverCASecret      ` | The secret containing the Server CA certificate. If not defined, a new secret will be created with a self-signed CA and will be used to generate the TLS certificate ServerTLSSecret.<br /> <br /> Contains:<br /> <br /> - `ca.crt`: CA that should be used to validate the server certificate, used as `sslrootcert` in client connection strings.<br /> - `ca.key`: key used to generate Server SSL certs, if ServerTLSSecret is provided, this can be omitted.<br /> | string  
`serverTLSSecret     ` | The secret of type kubernetes.io/tls containing the server TLS certificate and key that will be set as `ssl_cert_file` and `ssl_key_file` so that clients can connect to postgres securely. If not defined, ServerCASecret must provide also `ca.key` and a new secret will be created using the provided CA.                                                                                                                                                            | string  
`replicationTLSSecret` | The secret of type kubernetes.io/tls containing the client certificate to authenticate as the `streaming_replica` user. If not defined, ClientCASecret must provide also `ca.key`, and a new secret will be created using the provided CA.                                                                                                                                                                                                                               | string  
`clientCASecret      ` | The secret containing the Client CA certificate. If not defined, a new secret will be created with a self-signed CA and will be used to generate all the client certificates.<br /> <br /> Contains:<br /> <br /> - `ca.crt`: CA that should be used to validate the client certificates, used as `ssl_ca_file` of all the instances.<br /> - `ca.key`: key used to generate client certificates, if ReplicationTLSSecret is provided, this can be omitted.<br />        | string  
`serverAltDNSNames   ` | The list of the server alternative DNS names to be added to the generated server TLS certificates, when required.                                                                                                                                                                                                                                                                                                                                                        | []string

<a id='CertificatesStatus'></a>

## CertificatesStatus

CertificatesStatus contains configuration certificates and related expiration dates.

Name        | Description                            | Type             
----------- | -------------------------------------- | -----------------
`expirations` | Expiration dates for all certificates. | map[string]string

<a id='Cluster'></a>

## Cluster

Cluster is the Schema for the PostgreSQL API

Name     | Description                                                                                                                                                                                                                       | Type                                                                                                        
-------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------
`metadata` |                                                                                                                                                                                                                                   | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#objectmeta-v1-meta)
`spec    ` | Specification of the desired behavior of the cluster. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status                                                              | [ClusterSpec](#ClusterSpec)                                                                                 
`status  ` | Most recently observed status of the cluster. This data may not be up to date. Populated by the system. Read-only. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | [ClusterStatus](#ClusterStatus)                                                                             

<a id='ClusterList'></a>

## ClusterList

ClusterList contains a list of Cluster

Name     | Description                                                                                                                        | Type                                                                                                    
-------- | ---------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------
`metadata` | Standard list metadata. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#listmeta-v1-meta)
`items   ` | List of clusters                                                                                                                   - *mandatory*  | [[]Cluster](#Cluster)                                                                                   

<a id='ClusterSpec'></a>

## ClusterSpec

ClusterSpec defines the desired state of Cluster

Name                  | Description                                                                                                                                                                                                                                                                                                                                                                                                             | Type                                                                                                                            
--------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------
`description          ` | Description of this PostgreSQL cluster                                                                                                                                                                                                                                                                                                                                                                                  | string                                                                                                                          
`inheritedMetadata    ` | Metadata that will be inherited by all objects related to the Cluster                                                                                                                                                                                                                                                                                                                                                   | [*EmbeddedObjectMetadata](#EmbeddedObjectMetadata)                                                                              
`imageName            ` | Name of the container image, supporting both tags (`<image>:<tag>`) and digests for deterministic and repeatable deployments (`<image>:<tag>@sha256:<digestValue>`)                                                                                                                                                                                                                                                     | string                                                                                                                          
`imagePullPolicy      ` | Image pull policy. One of `Always`, `Never` or `IfNotPresent`. If not defined, it defaults to `IfNotPresent`. Cannot be updated. More info: https://kubernetes.io/docs/concepts/containers/images#updating-images                                                                                                                                                                                                       | corev1.PullPolicy                                                                                                               
`postgresUID          ` | The UID of the `postgres` user inside the image, defaults to `26`                                                                                                                                                                                                                                                                                                                                                       | int64                                                                                                                           
`postgresGID          ` | The GID of the `postgres` user inside the image, defaults to `26`                                                                                                                                                                                                                                                                                                                                                       | int64                                                                                                                           
`instances            ` | Number of instances required in the cluster                                                                                                                                                                                                                                                                                                                                                                             - *mandatory*  | int                                                                                                                             
`minSyncReplicas      ` | Minimum number of instances required in synchronous replication with the primary. Undefined or 0 allow writes to complete when no standby is available.                                                                                                                                                                                                                                                                 | int                                                                                                                             
`maxSyncReplicas      ` | The target value for the synchronous replication quorum, that can be decreased if the number of ready standbys is lower than this. Undefined or 0 disable synchronous replication.                                                                                                                                                                                                                                      | int                                                                                                                             
`postgresql           ` | Configuration of the PostgreSQL server                                                                                                                                                                                                                                                                                                                                                                                  | [PostgresConfiguration](#PostgresConfiguration)                                                                                 
`bootstrap            ` | Instructions to bootstrap this cluster                                                                                                                                                                                                                                                                                                                                                                                  | [*BootstrapConfiguration](#BootstrapConfiguration)                                                                              
`replica              ` | Replica cluster configuration                                                                                                                                                                                                                                                                                                                                                                                           | [*ReplicaClusterConfiguration](#ReplicaClusterConfiguration)                                                                    
`superuserSecret      ` | The secret containing the superuser password. If not defined a new secret will be created with a randomly generated password                                                                                                                                                                                                                                                                                            | [*LocalObjectReference](#LocalObjectReference)                                                                                  
`enableSuperuserAccess` | When this option is enabled, the operator will use the `SuperuserSecret` to update the `postgres` user password (if the secret is not present, the operator will automatically create one). When this option is disabled, the operator will ignore the `SuperuserSecret` content, delete it when automatically created, and then blank the password of the `postgres` user by setting it to `NULL`. Enabled by default. | *bool                                                                                                                           
`certificates         ` | The configuration for the CA and related certificates                                                                                                                                                                                                                                                                                                                                                                   | [*CertificatesConfiguration](#CertificatesConfiguration)                                                                        
`imagePullSecrets     ` | The list of pull secrets to be used to pull the images                                                                                                                                                                                                                                                                                                                                                                  | [[]LocalObjectReference](#LocalObjectReference)                                                                                 
`storage              ` | Configuration of the storage of the instances                                                                                                                                                                                                                                                                                                                                                                           | [StorageConfiguration](#StorageConfiguration)                                                                                   
`startDelay           ` | The time in seconds that is allowed for a PostgreSQL instance to successfully start up (default 30)                                                                                                                                                                                                                                                                                                                     | int32                                                                                                                           
`stopDelay            ` | The time in seconds that is allowed for a PostgreSQL instance to gracefully shutdown (default 30)                                                                                                                                                                                                                                                                                                                       | int32                                                                                                                           
`switchoverDelay      ` | The time in seconds that is allowed for a primary PostgreSQL instance to gracefully shutdown during a switchover. Default value is 40000000, greater than one year in seconds, big enough to simulate an infinite delay                                                                                                                                                                                                 | int32                                                                                                                           
`affinity             ` | Affinity/Anti-affinity rules for Pods                                                                                                                                                                                                                                                                                                                                                                                   | [AffinityConfiguration](#AffinityConfiguration)                                                                                 
`resources            ` | Resources requirements of every generated Pod. Please refer to https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ for more information.                                                                                                                                                                                                                                                     | [corev1.ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#resourcerequirements-v1-core)
`primaryUpdateStrategy` | Strategy to follow to upgrade the primary server during a rolling update procedure, after all replicas have been successfully updated: it can be automated (`unsupervised` - default) or manual (`supervised`)                                                                                                                                                                                                          | PrimaryUpdateStrategy                                                                                                           
`primaryUpdateMethod  ` | Method to follow to upgrade the primary server during a rolling update procedure, after all replicas have been successfully updated: it can be with a switchover (`switchover` - default) or in-place (`restart`)                                                                                                                                                                                                       | PrimaryUpdateMethod                                                                                                             
`backup               ` | The configuration to be used for backups                                                                                                                                                                                                                                                                                                                                                                                | [*BackupConfiguration](#BackupConfiguration)                                                                                    
`nodeMaintenanceWindow` | Define a maintenance window for the Kubernetes nodes                                                                                                                                                                                                                                                                                                                                                                    | [*NodeMaintenanceWindow](#NodeMaintenanceWindow)                                                                                
`monitoring           ` | The configuration of the monitoring infrastructure of this cluster                                                                                                                                                                                                                                                                                                                                                      | [*MonitoringConfiguration](#MonitoringConfiguration)                                                                            
`externalClusters     ` | The list of external clusters which are used in the configuration                                                                                                                                                                                                                                                                                                                                                       | [[]ExternalCluster](#ExternalCluster)                                                                                           
`logLevel             ` | The instances' log level, one of the following values: error, warning, info (default), debug, trace                                                                                                                                                                                                                                                                                                                     | string                                                                                                                          

<a id='ClusterStatus'></a>

## ClusterStatus

ClusterStatus defines the observed state of Cluster

Name                      | Description                                                                                                                                                                        | Type                                                 
------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -----------------------------------------------------
`instances                ` | Total number of instances in the cluster                                                                                                                                           | int                                                  
`readyInstances           ` | Total number of ready instances in the cluster                                                                                                                                     | int                                                  
`instancesStatus          ` | Instances status                                                                                                                                                                   | map[utils.PodStatus][]string                         
`latestGeneratedNode      ` | ID of the latest generated node (used to avoid node name clashing)                                                                                                                 | int                                                  
`currentPrimary           ` | Current primary instance                                                                                                                                                           | string                                               
`targetPrimary            ` | Target primary instance, this is different from the previous one during a switchover or a failover                                                                                 | string                                               
`pvcCount                 ` | How many PVCs have been created by this cluster                                                                                                                                    | int32                                                
`jobCount                 ` | How many Jobs have been created by this cluster                                                                                                                                    | int32                                                
`danglingPVC              ` | List of all the PVCs created by this cluster and still available which are not attached to a Pod                                                                                   | []string                                             
`resizingPVC              ` | List of all the PVCs that have ResizingPVC condition.                                                                                                                              | []string                                             
`initializingPVC          ` | List of all the PVCs that are being initialized by this cluster                                                                                                                    | []string                                             
`healthyPVC               ` | List of all the PVCs not dangling nor initializing                                                                                                                                 | []string                                             
`writeService             ` | Current write pod                                                                                                                                                                  | string                                               
`readService              ` | Current list of read pods                                                                                                                                                          | string                                               
`phase                    ` | Current phase of the cluster                                                                                                                                                       | string                                               
`phaseReason              ` | Reason for the current phase                                                                                                                                                       | string                                               
`secretsResourceVersion   ` | The list of resource versions of the secrets managed by the operator. Every change here is done in the interest of the instance manager, which will refresh the secret data        | [SecretsResourceVersion](#SecretsResourceVersion)    
`configMapResourceVersion ` | The list of resource versions of the configmaps, managed by the operator. Every change here is done in the interest of the instance manager, which will refresh the configmap data | [ConfigMapResourceVersion](#ConfigMapResourceVersion)
`certificates             ` | The configuration for the CA and related certificates, initialized with defaults.                                                                                                  | [CertificatesStatus](#CertificatesStatus)            
`firstRecoverabilityPoint ` | The first recoverability point, stored as a date in RFC3339 format                                                                                                                 | string                                               
`cloudNativePGCommitHash  ` | The commit hash number of which this operator running                                                                                                                              | string                                               
`currentPrimaryTimestamp  ` | The timestamp when the last actual promotion to primary has occurred                                                                                                               | string                                               
`targetPrimaryTimestamp   ` | The timestamp when the last request for a new primary has occurred                                                                                                                 | string                                               
`poolerIntegrations       ` | The integration needed by poolers referencing the cluster                                                                                                                          | [*PoolerIntegrations](#PoolerIntegrations)           
`cloudNativePGOperatorHash` | The hash of the binary of the operator                                                                                                                                             | string                                               
`onlineUpdateEnabled      ` | OnlineUpdateEnabled shows if the online upgrade is enabled inside the cluster                                                                                                      | bool                                                 
`azurePVCUpdateEnabled    ` | AzurePVCUpdateEnabled shows if the PVC online upgrade is enabled for this cluster                                                                                                  | bool                                                 
`conditions               ` | Conditions for cluster object                                                                                                                                                      | []metav1.Condition                                   

<a id='ConfigMapKeySelector'></a>

## ConfigMapKeySelector

ConfigMapKeySelector contains enough information to let you locate the key of a ConfigMap

Name  | Description       | Type  
--- | ----------------- | ------
`key` | The key to select - *mandatory*  | string

<a id='ConfigMapResourceVersion'></a>

## ConfigMapResourceVersion

ConfigMapResourceVersion is the resource versions of the secrets managed by the operator

Name    | Description                                                                                                                         | Type             
------- | ----------------------------------------------------------------------------------------------------------------------------------- | -----------------
`metrics` | A map with the versions of all the config maps used to pass metrics. Map keys are the config map names, map values are the versions | map[string]string

<a id='DataBackupConfiguration'></a>

## DataBackupConfiguration

DataBackupConfiguration is the configuration of the backup of the data directory

Name                | Description                                                                                                                                                                                                                                                                                                          | Type           
------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------
`compression        ` | Compress a backup file (a tar file per tablespace) while streaming it to the object store. Available options are empty string (no compression, default), `gzip`, `bzip2` or `snappy`.                                                                                                                                | CompressionType
`encryption         ` | Whenever to force the encryption of files (if the bucket is not already configured for that). Allowed options are empty string (use the bucket policy, default), `AES256` and `aws:kms`                                                                                                                              | EncryptionType 
`immediateCheckpoint` | Control whether the I/O workload for the backup initial checkpoint will be limited, according to the `checkpoint_completion_target` setting on the PostgreSQL server. If set to true, an immediate checkpoint will be used, meaning PostgreSQL will complete the checkpoint as soon as possible. `false` by default. | bool           
`jobs               ` | The number of parallel jobs to be used to upload the backup, defaults to 2                                                                                                                                                                                                                                           | *int32         

<a id='EmbeddedObjectMetadata'></a>

## EmbeddedObjectMetadata

EmbeddedObjectMetadata contains metadata to be inherited by all resources related to a Cluster

Name        | Description            | Type             
----------- | --- | -----------------
`labels     ` |  | map[string]string
`annotations` |  | map[string]string

<a id='ExternalCluster'></a>

## ExternalCluster

ExternalCluster represents the connection parameters to an external cluster which is used in the other sections of the configuration

Name                 | Description                                                                  | Type                                                                                                                       
-------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------
`name                ` | The server name, required                                                    - *mandatory*  | string                                                                                                                     
`connectionParameters` | The list of connection parameters, such as dbname, host, username, etc       | map[string]string                                                                                                          
`sslCert             ` | The reference to an SSL certificate to be used to connect to this instance   | [*corev1.SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#secretkeyselector-v1-core)
`sslKey              ` | The reference to an SSL private key to be used to connect to this instance   | [*corev1.SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#secretkeyselector-v1-core)
`sslRootCert         ` | The reference to an SSL CA public key to be used to connect to this instance | [*corev1.SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#secretkeyselector-v1-core)
`password            ` | The reference to the password to be used to connect to the server            | [*corev1.SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#secretkeyselector-v1-core)
`barmanObjectStore   ` | The configuration for the barman-cloud tool suite                            | [*BarmanObjectStoreConfiguration](#BarmanObjectStoreConfiguration)                                                         

<a id='GoogleCredentials'></a>

## GoogleCredentials

GoogleCredentials is the type for the Google Cloud Storage credentials. This needs to be specified even if we run inside a GKE environment.

Name                   | Description                                                                                | Type                                    
---------------------- | ------------------------------------------------------------------------------------------ | ----------------------------------------
`gkeEnvironment        ` | If set to true, will presume that it's running inside a GKE environment, default to false. - *mandatory*  | bool                                    
`applicationCredentials` | The secret containing the Google Cloud Storage JSON file with the credentials              | [*SecretKeySelector](#SecretKeySelector)

<a id='InstanceID'></a>

## InstanceID

InstanceID contains the information to identify an instance

Name        | Description      | Type  
----------- | ---------------- | ------
`podName    ` | The pod name     | string
`ContainerID` | The container ID | string

<a id='LDAPBindAsAuth'></a>

## LDAPBindAsAuth

LDAPBindAsAuth provides the required fields to use the bind authentication for LDAP

Name   | Description                               | Type  
------ | ----------------------------------------- | ------
`prefix` | Prefix for the bind authentication option | string
`suffix` | Suffix for the bind authentication option | string

<a id='LDAPBindSearchAuth'></a>

## LDAPBindSearchAuth

LDAPBindSearchAuth provides the required fields to use the bind+search LDAP authentication process

Name            | Description                                                    | Type                                                                                                                       
--------------- | -------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------
`baseDN         ` | Root DN to begin the user search                               | string                                                                                                                     
`bindDN         ` | DN of the user to bind to the directory                        | string                                                                                                                     
`bindPassword   ` | Secret with the password for the user to bind to the directory | [*corev1.SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#secretkeyselector-v1-core)
`searchAttribute` | Attribute to match against the username                        | string                                                                                                                     
`searchFilter   ` | Search filter to use when doing the search+bind authentication | string                                                                                                                     

<a id='LDAPConfig'></a>

## LDAPConfig

LDAPConfig contains the parameters needed for LDAP authentication

Name           | Description                                                     | Type                                      
-------------- | --------------------------------------------------------------- | ------------------------------------------
`server        ` | LDAP hostname or IP address                                     | string                                    
`port          ` | LDAP server port                                                | int                                       
`scheme        ` | LDAP schema to be used, possible options are `ldap` and `ldaps` | LDAPScheme                                
`tls           ` | Set to 1 to enable LDAP over TLS                                | bool                                      
`bindAsAuth    ` | Bind as authentication configuration                            | [*LDAPBindAsAuth](#LDAPBindAsAuth)        
`bindSearchAuth` | Bind+Search authentication configuration                        | [*LDAPBindSearchAuth](#LDAPBindSearchAuth)

<a id='LocalObjectReference'></a>

## LocalObjectReference

LocalObjectReference contains enough information to let you locate a local object with a known type inside the same namespace

Name | Description           | Type  
---- | --------------------- | ------
`name` | Name of the referent. - *mandatory*  | string

<a id='MonitoringConfiguration'></a>

## MonitoringConfiguration

MonitoringConfiguration is the type containing all the monitoring configuration for a certain cluster

Name                   | Description                                                                                                                                    | Type                                           
---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- | -----------------------------------------------
`disableDefaultQueries ` | Whether the default queries should be injected. Set it to `true` if you don't want to inject default queries into the cluster. Default: false. | *bool                                          
`customQueriesConfigMap` | The list of config maps containing the custom queries                                                                                          | [[]ConfigMapKeySelector](#ConfigMapKeySelector)
`customQueriesSecret   ` | The list of secrets containing the custom queries                                                                                              | [[]SecretKeySelector](#SecretKeySelector)      
`enablePodMonitor      ` | Enable or disable the `PodMonitor`                                                                                                             | bool                                           

<a id='NodeMaintenanceWindow'></a>

## NodeMaintenanceWindow

NodeMaintenanceWindow contains information that the operator will use while upgrading the underlying node.

This option is only useful when the chosen storage prevents the Pods from being freely moved across nodes.

Name       | Description                                                                                                      | Type 
---------- | ---------------------------------------------------------------------------------------------------------------- | -----
`inProgress` | Is there a node maintenance activity in progress?                                                                - *mandatory*  | bool 
`reusePVC  ` | Reuse the existing PVC (wait for the node to come up again) or not (recreate it elsewhere - when `instances` >1) - *mandatory*  | *bool

<a id='PgBouncerIntegrationStatus'></a>

## PgBouncerIntegrationStatus

PgBouncerIntegrationStatus encapsulates the needed integration for the pgbouncer poolers referencing the cluster

Name    | Description            | Type    
------- | --- | --------
`secrets` |  | []string

<a id='PgBouncerSecrets'></a>

## PgBouncerSecrets

PgBouncerSecrets contains the versions of the secrets used by pgbouncer

Name      | Description                   | Type                           
--------- | ----------------------------- | -------------------------------
`authQuery` | The auth query secret version | [SecretVersion](#SecretVersion)

<a id='PgBouncerSpec'></a>

## PgBouncerSpec

PgBouncerSpec defines how to configure PgBouncer

Name            | Description                                                                                                                                                                                                                                                                       | Type                                          
--------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------
`poolMode       ` | The pool mode                                                                                                                                                                                                                                                                     - *mandatory*  | PgBouncerPoolMode                             
`authQuerySecret` | The credentials of the user that need to be used for the authentication query. In case it is specified, also an AuthQuery (e.g. "SELECT usename, passwd FROM pg_shadow WHERE usename=$1") has to be specified and no automatic CNPG Cluster integration will be triggered.        | [*LocalObjectReference](#LocalObjectReference)
`authQuery      ` | The query that will be used to download the hash of the password of a certain user. Default: "SELECT usename, passwd FROM user_search($1)". In case it is specified, also an AuthQuerySecret has to be specified and no automatic CNPG Cluster integration will be triggered.     | string                                        
`parameters     ` | Additional parameters to be passed to PgBouncer - please check the CNPG documentation for a list of options you can configure                                                                                                                                                     | map[string]string                             
`paused         ` | When set to `true`, PgBouncer will disconnect from the PostgreSQL server, first waiting for all queries to complete, and pause all new client connections until this value is set to `false` (default). Internally, the operator calls PgBouncer's `PAUSE` and `RESUME` commands. | *bool                                         

<a id='PodMeta'></a>

## PodMeta

PodMeta is a structure similar to the metav1.ObjectMeta, but still parseable by controller-gen to create a suitable CRD for the user. The comment of PodTemplateSpec has an explanation of why we are not using the core data types.

Name        | Description                                                                                                                                                                                                                                                                        | Type             
----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -----------------
`labels     ` | Map of string keys and values that can be used to organize and categorize (scope and select) objects. May match selectors of replication controllers and services. More info: http://kubernetes.io/docs/user-guide/labels                                                          | map[string]string
`annotations` | Annotations is an unstructured key value map stored with a resource that may be set by external tools to store and retrieve arbitrary metadata. They are not queryable and should be preserved when modifying objects. More info: http://kubernetes.io/docs/user-guide/annotations | map[string]string

<a id='PodTemplateSpec'></a>

## PodTemplateSpec

PodTemplateSpec is a structure allowing the user to set a template for Pod generation.

Unfortunately we can't use the corev1.PodTemplateSpec type because the generated CRD won't have the field for the metadata section.

References: https://github.com/kubernetes-sigs/controller-tools/issues/385 https://github.com/kubernetes-sigs/controller-tools/issues/448 https://github.com/prometheus-operator/prometheus-operator/issues/3041

Name     | Description                                                                                                                                                      | Type               
-------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------
`metadata` | Standard object's metadata. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata                              | [PodMeta](#PodMeta)
`spec    ` | Specification of the desired behavior of the pod. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | corev1.PodSpec     

<a id='Pooler'></a>

## Pooler

Pooler is the Schema for the poolers API

Name     | Description            | Type                                                                                                        
-------- | --- | ------------------------------------------------------------------------------------------------------------
`metadata` |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#objectmeta-v1-meta)
`spec    ` |  | [PoolerSpec](#PoolerSpec)                                                                                   
`status  ` |  | [PoolerStatus](#PoolerStatus)                                                                               

<a id='PoolerIntegrations'></a>

## PoolerIntegrations

PoolerIntegrations encapsulates the needed integration for the poolers referencing the cluster

Name                 | Description            | Type                                                     
-------------------- | --- | ---------------------------------------------------------
`pgBouncerIntegration` |  | [PgBouncerIntegrationStatus](#PgBouncerIntegrationStatus)

<a id='PoolerList'></a>

## PoolerList

PoolerList contains a list of Pooler

Name     | Description            | Type                                                                                                    
-------- | --- | --------------------------------------------------------------------------------------------------------
`metadata` |  | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#listmeta-v1-meta)
`items   ` |  - *mandatory*  | [[]Pooler](#Pooler)                                                                                     

<a id='PoolerSecrets'></a>

## PoolerSecrets

PoolerSecrets contains the versions of all the secrets used

Name             | Description                                  | Type                                  
---------------- | -------------------------------------------- | --------------------------------------
`serverTLS       ` | The server TLS secret version                | [SecretVersion](#SecretVersion)       
`serverCA        ` | The server CA secret version                 | [SecretVersion](#SecretVersion)       
`clientCA        ` | The client CA secret version                 | [SecretVersion](#SecretVersion)       
`pgBouncerSecrets` | The version of the secrets used by PgBouncer | [*PgBouncerSecrets](#PgBouncerSecrets)

<a id='PoolerSpec'></a>

## PoolerSpec

PoolerSpec defines the desired state of Pooler

Name      | Description                                                                                                                                  | Type                                         
--------- | -------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------
`cluster  ` | This is the cluster reference on which the Pooler will work. Pooler name should never match with any cluster name within the same namespace. - *mandatory*  | [LocalObjectReference](#LocalObjectReference)
`type     ` | Which instances we must forward traffic to?                                                                                                  - *mandatory*  | PoolerType                                   
`instances` | The number of replicas we want                                                                                                               - *mandatory*  | int32                                        
`template ` | The template of the Pod to be created                                                                                                        | [*PodTemplateSpec](#PodTemplateSpec)         
`pgbouncer` | The PgBouncer configuration                                                                                                                  - *mandatory*  | [*PgBouncerSpec](#PgBouncerSpec)             

<a id='PoolerStatus'></a>

## PoolerStatus

PoolerStatus defines the observed state of Pooler

Name      | Description                               | Type                            
--------- | ----------------------------------------- | --------------------------------
`secrets  ` | The resource version of the config object | [*PoolerSecrets](#PoolerSecrets)
`instances` | The number of pods trying to be scheduled | int32                           

<a id='PostgresConfiguration'></a>

## PostgresConfiguration

PostgresConfiguration defines the PostgreSQL configuration

Name                     | Description                                                                                                                                                                                    | Type                      
------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------
`parameters              ` | PostgreSQL configuration options (postgresql.conf)                                                                                                                                             | map[string]string         
`pg_hba                  ` | PostgreSQL Host Based Authentication rules (lines to be appended to the pg_hba.conf file)                                                                                                      | []string                  
`promotionTimeout        ` | Specifies the maximum number of seconds to wait when promoting an instance to primary. Default value is 40000000, greater than one year in seconds, big enough to simulate an infinite timeout | int32                     
`shared_preload_libraries` | Lists of shared preload libraries to add to the default ones                                                                                                                                   | []string                  
`ldap                    ` | Options to specify LDAP configuration                                                                                                                                                          | [*LDAPConfig](#LDAPConfig)

<a id='RecoveryTarget'></a>

## RecoveryTarget

RecoveryTarget allows to configure the moment where the recovery process will stop. All the target options except TargetTLI are mutually exclusive.

Name            | Description                                                               | Type  
--------------- | ------------------------------------------------------------------------- | ------
`targetTLI      ` | The target timeline ("latest" or a positive integer)                      | string
`targetXID      ` | The target transaction ID                                                 | string
`targetName     ` | The target name (to be previously created with `pg_create_restore_point`) | string
`targetLSN      ` | The target LSN (Log Sequence Number)                                      | string
`targetTime     ` | The target time, in any unambiguous representation allowed by PostgreSQL  | string
`targetImmediate` | End recovery as soon as a consistent state is reached                     | *bool 
`exclusive      ` | Set the target to be exclusive (defaults to true)                         | *bool 

<a id='ReplicaClusterConfiguration'></a>

## ReplicaClusterConfiguration

ReplicaClusterConfiguration encapsulates the configuration of a replica cluster

Name    | Description                                                                                                                                                                                                                                                     | Type  
------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------
`enabled` | If replica mode is enabled, this cluster will be a replica of an existing cluster. Replica cluster can be created from a recovery object store or via streaming through pg_basebackup. Refer to the Replication page of the documentation for more information. - *mandatory*  | bool  
`source ` | The name of the external cluster which is the replication origin                                                                                                                                                                                                - *mandatory*  | string

<a id='RollingUpdateStatus'></a>

## RollingUpdateStatus

RollingUpdateStatus contains the information about an instance which is being updated

Name      | Description                         | Type                                                                                            
--------- | ----------------------------------- | ------------------------------------------------------------------------------------------------
`imageName` | The image which we put into the Pod - *mandatory*  | string                                                                                          
`startedAt` | When the update has been started    | [metav1.Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#time-v1-meta)

<a id='S3Credentials'></a>

## S3Credentials

S3Credentials is the type for the credentials to be used to upload files to S3. It can be provided in two alternative ways:

- explicitly passing accessKeyId and secretAccessKey

- inheriting the role from the pod environment by setting inheritFromIAMRole to true

Name               | Description                                                              | Type                                    
------------------ | ------------------------------------------------------------------------ | ----------------------------------------
`accessKeyId       ` | The reference to the access key id                                       | [*SecretKeySelector](#SecretKeySelector)
`secretAccessKey   ` | The reference to the secret access key                                   | [*SecretKeySelector](#SecretKeySelector)
`sessionToken      ` | The references to the session key                                        | [*SecretKeySelector](#SecretKeySelector)
`inheritFromIAMRole` | Use the role based authentication without providing explicitly the keys. - *mandatory*  | bool                                    

<a id='ScheduledBackup'></a>

## ScheduledBackup

ScheduledBackup is the Schema for the scheduledbackups API

Name     | Description                                                                                                                                                                                                                               | Type                                                                                                        
-------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------
`metadata` |                                                                                                                                                                                                                                           | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#objectmeta-v1-meta)
`spec    ` | Specification of the desired behavior of the ScheduledBackup. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status                                                              | [ScheduledBackupSpec](#ScheduledBackupSpec)                                                                 
`status  ` | Most recently observed status of the ScheduledBackup. This data may not be up to date. Populated by the system. Read-only. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | [ScheduledBackupStatus](#ScheduledBackupStatus)                                                             

<a id='ScheduledBackupList'></a>

## ScheduledBackupList

ScheduledBackupList contains a list of ScheduledBackup

Name     | Description                                                                                                                        | Type                                                                                                    
-------- | ---------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------
`metadata` | Standard list metadata. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#listmeta-v1-meta)
`items   ` | List of clusters                                                                                                                   - *mandatory*  | [[]ScheduledBackup](#ScheduledBackup)                                                                   

<a id='ScheduledBackupSpec'></a>

## ScheduledBackupSpec

ScheduledBackupSpec defines the desired state of ScheduledBackup

Name                 | Description                                                                                                                                                                                                                                                                                                                          | Type                                         
-------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------
`suspend             ` | If this backup is suspended or not                                                                                                                                                                                                                                                                                                   | *bool                                        
`immediate           ` | If the first backup has to be immediately start after creation or not                                                                                                                                                                                                                                                                | *bool                                        
`schedule            ` | The schedule follows the same format used in Kubernetes CronJobs, see https://pkg.go.dev/github.com/robfig/cron#hdr-CRON_Expression_Format                                                                                                                                                                                           - *mandatory*  | string                                       
`cluster             ` | The cluster to backup                                                                                                                                                                                                                                                                                                                | [LocalObjectReference](#LocalObjectReference)
`backupOwnerReference` | Indicates which ownerReference should be put inside the created backup resources.<br /> - none: no owner reference for created backup objects (same behavior as before the field was introduced)<br /> - self: sets the Scheduled backup object as owner of the backup<br /> - cluster: set the cluster as owner of the backup<br /> | string                                       

<a id='ScheduledBackupStatus'></a>

## ScheduledBackupStatus

ScheduledBackupStatus defines the observed state of ScheduledBackup

Name             | Description                                                                | Type                                                                                             
---------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------
`lastCheckTime   ` | The latest time the schedule                                               | [*metav1.Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#time-v1-meta)
`lastScheduleTime` | Information when was the last time that backup was successfully scheduled. | [*metav1.Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#time-v1-meta)
`nextScheduleTime` | Next time we will run a backup                                             | [*metav1.Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#time-v1-meta)

<a id='SecretKeySelector'></a>

## SecretKeySelector

SecretKeySelector contains enough information to let you locate the key of a Secret

Name  | Description       | Type  
--- | ----------------- | ------
`key` | The key to select - *mandatory*  | string

<a id='SecretVersion'></a>

## SecretVersion

SecretVersion contains a secret name and its ResourceVersion

Name    | Description                       | Type  
------- | --------------------------------- | ------
`name   ` | The name of the secret            | string
`version` | The ResourceVersion of the secret | string

<a id='SecretsResourceVersion'></a>

## SecretsResourceVersion

SecretsResourceVersion is the resource versions of the secrets managed by the operator

Name                     | Description                                                                                                                 | Type             
------------------------ | --------------------------------------------------------------------------------------------------------------------------- | -----------------
`superuserSecretVersion  ` | The resource version of the "postgres" user secret                                                                          | string           
`replicationSecretVersion` | The resource version of the "streaming_replica" user secret                                                                 | string           
`applicationSecretVersion` | The resource version of the "app" user secret                                                                               | string           
`caSecretVersion         ` | Unused. Retained for compatibility with old versions.                                                                       | string           
`clientCaSecretVersion   ` | The resource version of the PostgreSQL client-side CA secret version                                                        | string           
`serverCaSecretVersion   ` | The resource version of the PostgreSQL server-side CA secret version                                                        | string           
`serverSecretVersion     ` | The resource version of the PostgreSQL server-side secret version                                                           | string           
`barmanEndpointCA        ` | The resource version of the Barman Endpoint CA if provided                                                                  | string           
`metrics                 ` | A map with the versions of all the secrets used to pass metrics. Map keys are the secret names, map values are the versions | map[string]string

<a id='StorageConfiguration'></a>

## StorageConfiguration

StorageConfiguration is the configuration of the storage of the PostgreSQL instances

Name               | Description                                                                                                                                                                                | Type                                                                                                                                   
------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------
`storageClass      ` | StorageClass to use for database data (`PGDATA`). Applied after evaluating the PVC template, if available. If not specified, generated PVCs will be satisfied by the default storage class | *string                                                                                                                                
`size              ` | Size of the storage. Required if not already specified in the PVC template. Changes to this field are automatically reapplied to the created PVCs. Size cannot be decreased.               - *mandatory*  | string                                                                                                                                 
`resizeInUseVolumes` | Resize existent PVCs, defaults to true                                                                                                                                                     | *bool                                                                                                                                  
`pvcTemplate       ` | Template to be used to generate the Persistent Volume Claim                                                                                                                                | [*corev1.PersistentVolumeClaimSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#persistentvolumeclaim-v1-core)

<a id='WalBackupConfiguration'></a>

## WalBackupConfiguration

WalBackupConfiguration is the configuration of the backup of the WAL stream

Name        | Description                                                                                                                                                                                                                                                                                                                                                                         | Type           
----------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------
`compression` | Compress a WAL file before sending it to the object store. Available options are empty string (no compression, default), `gzip`, `bzip2` or `snappy`.                                                                                                                                                                                                                               | CompressionType
`encryption ` | Whenever to force the encryption of files (if the bucket is not already configured for that). Allowed options are empty string (use the bucket policy, default), `AES256` and `aws:kms`                                                                                                                                                                                             | EncryptionType 
`maxParallel` | Number of WAL files to be either archived in parallel (when the PostgreSQL instance is archiving to a backup object store) or restored in parallel (when a PostgreSQL standby is fetching WAL files from a recovery object store). If not specified, WAL files will be processed one at a time. It accepts a positive integer as a value - with 1 being the minimum accepted value. | int            

