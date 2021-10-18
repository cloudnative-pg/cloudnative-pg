/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1alpha1

import (
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	v1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

// ConvertTo converts this Cluster to the Hub version (v1).
func (src *Cluster) ConvertTo(dstRaw conversion.Hub) error { //nolint:revive,gocognit
	dst := dstRaw.(*v1.Cluster)

	// objectmeta
	dst.ObjectMeta = src.ObjectMeta

	// spec
	dst.Spec.Description = src.Spec.Description
	dst.Spec.ImageName = src.Spec.ImageName
	dst.Spec.ImagePullPolicy = src.Spec.ImagePullPolicy
	dst.Spec.PostgresUID = src.Spec.PostgresUID
	dst.Spec.PostgresGID = src.Spec.PostgresGID
	dst.Spec.Instances = src.Spec.Instances
	dst.Spec.MinSyncReplicas = src.Spec.MinSyncReplicas
	dst.Spec.MaxSyncReplicas = src.Spec.MaxSyncReplicas
	dst.Spec.EnableSuperuserAccess = src.Spec.EnableSuperuserAccess
	dst.Spec.LogLevel = src.Spec.LogLevel

	// spec.postgresql
	dst.Spec.PostgresConfiguration.Parameters = src.Spec.PostgresConfiguration.Parameters
	dst.Spec.PostgresConfiguration.PgHBA = src.Spec.PostgresConfiguration.PgHBA
	dst.Spec.PostgresConfiguration.AdditionalLibraries = src.Spec.PostgresConfiguration.AdditionalLibraries

	// spec.bootstrap
	if src.Spec.Bootstrap != nil {
		dst.Spec.Bootstrap = &v1.BootstrapConfiguration{}
		src.Spec.Bootstrap.ConvertTo(dst.Spec.Bootstrap)
	}

	// spec.certificates
	if src.Spec.Certificates != nil {
		dst.Spec.Certificates = &v1.CertificatesConfiguration{}
		dst.Spec.Certificates.ServerTLSSecret = src.Spec.Certificates.ServerTLSSecret
		dst.Spec.Certificates.ServerCASecret = src.Spec.Certificates.ServerCASecret
		dst.Spec.Certificates.ServerAltDNSNames = src.Spec.Certificates.ServerAltDNSNames
		dst.Spec.Certificates.ClientCASecret = src.Spec.Certificates.ClientCASecret
		dst.Spec.Certificates.ReplicationTLSSecret = src.Spec.Certificates.ReplicationTLSSecret
	}

	// src.spec.superuserSecret
	if src.Spec.SuperuserSecret != nil {
		dst.Spec.SuperuserSecret = &v1.LocalObjectReference{}
		dst.Spec.SuperuserSecret.Name = src.Spec.SuperuserSecret.Name
	}

	// src.spec.imagePullSecrets
	if src.Spec.ImagePullSecrets != nil {
		dst.Spec.ImagePullSecrets = make([]v1.LocalObjectReference, len(src.Spec.ImagePullSecrets))
		for idx := range src.Spec.ImagePullSecrets {
			dst.Spec.ImagePullSecrets[idx].Name = src.Spec.ImagePullSecrets[idx].Name
		}
	}

	// spec.storage
	srcStorageConf := src.Spec.StorageConfiguration
	dst.Spec.StorageConfiguration.StorageClass = srcStorageConf.StorageClass
	dst.Spec.StorageConfiguration.Size = srcStorageConf.Size
	dst.Spec.StorageConfiguration.ResizeInUseVolumes = srcStorageConf.ResizeInUseVolumes
	dst.Spec.StorageConfiguration.PersistentVolumeClaimTemplate = srcStorageConf.PersistentVolumeClaimTemplate

	dst.Spec.MaxStartDelay = src.Spec.MaxStartDelay
	dst.Spec.MaxStopDelay = src.Spec.MaxStopDelay

	// spec.affinity
	dst.Spec.Affinity.EnablePodAntiAffinity = src.Spec.Affinity.EnablePodAntiAffinity
	dst.Spec.Affinity.TopologyKey = src.Spec.Affinity.TopologyKey
	dst.Spec.Affinity.NodeSelector = src.Spec.Affinity.NodeSelector
	dst.Spec.Affinity.Tolerations = src.Spec.Affinity.Tolerations
	dst.Spec.Affinity.PodAntiAffinityType = src.Spec.Affinity.PodAntiAffinityType
	dst.Spec.Affinity.AdditionalPodAntiAffinity = src.Spec.Affinity.AdditionalPodAntiAffinity
	dst.Spec.Affinity.AdditionalPodAffinity = src.Spec.Affinity.AdditionalPodAffinity

	dst.Spec.Resources = src.Spec.Resources
	dst.Spec.PrimaryUpdateStrategy = v1.PrimaryUpdateStrategy(src.Spec.PrimaryUpdateStrategy)

	// spec.backup
	if src.Spec.Backup != nil { // nolint:nestif
		dst.Spec.Backup = &v1.BackupConfiguration{}
		if src.Spec.Backup.BarmanObjectStore != nil {
			dst.Spec.Backup.BarmanObjectStore = &v1.BarmanObjectStoreConfiguration{}
			src.Spec.Backup.BarmanObjectStore.ConvertTo(dst.Spec.Backup.BarmanObjectStore)
		}
		dst.Spec.Backup.RetentionPolicy = src.Spec.Backup.RetentionPolicy
	}

	// spec.nodeMaintenanceWindow
	if src.Spec.NodeMaintenanceWindow != nil {
		dst.Spec.NodeMaintenanceWindow = &v1.NodeMaintenanceWindow{}
		dst.Spec.NodeMaintenanceWindow.InProgress = src.Spec.NodeMaintenanceWindow.InProgress
		dst.Spec.NodeMaintenanceWindow.ReusePVC = src.Spec.NodeMaintenanceWindow.ReusePVC
	}

	// spec.monitoring
	if src.Spec.Monitoring != nil {
		dst.Spec.Monitoring = &v1.MonitoringConfiguration{}
		if src.Spec.Monitoring.CustomQueriesConfigMap != nil {
			dst.Spec.Monitoring.CustomQueriesConfigMap = make(
				[]v1.ConfigMapKeySelector,
				len(src.Spec.Monitoring.CustomQueriesConfigMap))
			for idx := range src.Spec.Monitoring.CustomQueriesConfigMap {
				dst.Spec.Monitoring.CustomQueriesConfigMap[idx].Key = src.Spec.Monitoring.CustomQueriesConfigMap[idx].Key
				dst.Spec.Monitoring.CustomQueriesConfigMap[idx].Name = src.Spec.Monitoring.CustomQueriesConfigMap[idx].Name
			}
		}

		if src.Spec.Monitoring.CustomQueriesSecret != nil {
			dst.Spec.Monitoring.CustomQueriesSecret = make(
				[]v1.SecretKeySelector,
				len(src.Spec.Monitoring.CustomQueriesSecret))
			for idx := range src.Spec.Monitoring.CustomQueriesSecret {
				dst.Spec.Monitoring.CustomQueriesSecret[idx].Key = src.Spec.Monitoring.CustomQueriesSecret[idx].Key
				dst.Spec.Monitoring.CustomQueriesSecret[idx].Name = src.Spec.Monitoring.CustomQueriesSecret[idx].Name
			}
		}
	}

	// spec.externalClusters
	if src.Spec.ExternalClusters != nil {
		dst.Spec.ExternalClusters = make([]v1.ExternalCluster, len(src.Spec.ExternalClusters))
		for idx, entry := range src.Spec.ExternalClusters {
			dst.Spec.ExternalClusters[idx].Name = entry.Name
			dst.Spec.ExternalClusters[idx].ConnectionParameters = entry.ConnectionParameters
			dst.Spec.ExternalClusters[idx].SSLCert = entry.SSLCert
			dst.Spec.ExternalClusters[idx].SSLKey = entry.SSLKey
			dst.Spec.ExternalClusters[idx].SSLRootCert = entry.SSLRootCert
			dst.Spec.ExternalClusters[idx].Password = entry.Password
			if entry.BarmanObjectStore != nil {
				dst.Spec.ExternalClusters[idx].BarmanObjectStore = &v1.BarmanObjectStoreConfiguration{}
				entry.BarmanObjectStore.ConvertTo(dst.Spec.ExternalClusters[idx].BarmanObjectStore)
			}
		}
	}

	// spec.replicaCluster
	if src.Spec.ReplicaCluster != nil {
		dst.Spec.ReplicaCluster = &v1.ReplicaClusterConfiguration{}
		dst.Spec.ReplicaCluster.Enabled = src.Spec.ReplicaCluster.Enabled
		dst.Spec.ReplicaCluster.Source = src.Spec.ReplicaCluster.Source
	}

	// status
	dst.Status.Instances = src.Status.Instances
	dst.Status.ReadyInstances = src.Status.ReadyInstances
	dst.Status.InstancesStatus = src.Status.InstancesStatus
	dst.Status.LatestGeneratedNode = src.Status.LatestGeneratedNode
	dst.Status.CurrentPrimary = src.Status.CurrentPrimary
	dst.Status.TargetPrimary = src.Status.TargetPrimary
	dst.Status.PVCCount = src.Status.PVCCount
	dst.Status.JobCount = src.Status.JobCount
	dst.Status.DanglingPVC = src.Status.DanglingPVC
	dst.Status.InitializingPVC = src.Status.InitializingPVC
	dst.Status.HealthyPVC = src.Status.HealthyPVC
	dst.Status.WriteService = src.Status.WriteService
	dst.Status.ReadService = src.Status.ReadService
	dst.Status.Phase = src.Status.Phase
	dst.Status.PhaseReason = src.Status.PhaseReason
	dst.Status.CommitHash = src.Status.CommitHash
	dst.Status.CurrentPrimaryTimestamp = src.Status.CurrentPrimaryTimestamp
	dst.Status.TargetPrimaryTimestamp = src.Status.TargetPrimaryTimestamp
	if src.Status.PoolerIntegrations != nil {
		dst.Status.PoolerIntegrations = &v1.PoolerIntegrations{PgBouncerIntegration: v1.PgbouncerIntegrationStatus{
			Secrets: src.Status.PoolerIntegrations.PgBouncerIntegration.Secrets,
		}}
	}
	dst.Status.SecretsResourceVersion.SuperuserSecretVersion =
		src.Status.SecretsResourceVersion.SuperuserSecretVersion
	dst.Status.SecretsResourceVersion.ReplicationSecretVersion =
		src.Status.SecretsResourceVersion.ReplicationSecretVersion
	dst.Status.SecretsResourceVersion.ApplicationSecretVersion =
		src.Status.SecretsResourceVersion.ApplicationSecretVersion
	dst.Status.SecretsResourceVersion.CASecretVersion = src.Status.SecretsResourceVersion.CASecretVersion
	dst.Status.SecretsResourceVersion.ClientCASecretVersion = src.Status.SecretsResourceVersion.ClientCASecretVersion
	dst.Status.SecretsResourceVersion.ServerCASecretVersion = src.Status.SecretsResourceVersion.ServerCASecretVersion
	dst.Status.SecretsResourceVersion.ServerSecretVersion = src.Status.SecretsResourceVersion.ServerSecretVersion
	dst.Status.SecretsResourceVersion.Metrics = src.Status.SecretsResourceVersion.Metrics
	dst.Status.SecretsResourceVersion.BarmanEndpointCA = src.Status.SecretsResourceVersion.BarmanEndpointCA
	dst.Status.ConfigMapResourceVersion.Metrics = src.Status.ConfigMapResourceVersion.Metrics
	dst.Status.Certificates.ServerTLSSecret = src.Status.Certificates.ServerTLSSecret
	dst.Status.Certificates.ServerCASecret = src.Status.Certificates.ServerCASecret
	dst.Status.Certificates.ClientCASecret = src.Status.Certificates.ClientCASecret
	dst.Status.Certificates.ReplicationTLSSecret = src.Status.Certificates.ReplicationTLSSecret
	dst.Status.Certificates.ServerAltDNSNames = src.Status.Certificates.ServerAltDNSNames
	dst.Status.Certificates.Expirations = src.Status.Certificates.Expirations

	return nil
}

// ConvertTo converts this specification to the relative Hub version (v1)
func (src *BootstrapConfiguration) ConvertTo(dstSpec *v1.BootstrapConfiguration) {
	// spec.bootstrap.initdb
	if src.InitDB != nil {
		srcInitDB := src.InitDB

		dstSpec.InitDB = &v1.BootstrapInitDB{}
		dstSpec.InitDB.Database = srcInitDB.Database
		dstSpec.InitDB.Owner = srcInitDB.Owner
		dstSpec.InitDB.Options = srcInitDB.Options
		dstSpec.InitDB.PostInitSQL = srcInitDB.PostInitSQL
	}

	// spec.bootstrap.initdb.secret
	if src.InitDB != nil && src.InitDB.Secret != nil {
		dstSpec.InitDB.Secret = &v1.LocalObjectReference{}
		dstSpec.InitDB.Secret.Name = src.InitDB.Secret.Name
	}

	// spec.bootstrap.recovery
	if src.Recovery != nil {
		dstSpec.Recovery = &v1.BootstrapRecovery{}
		dstSpec.Recovery.Source = src.Recovery.Source
	}

	// spec.bootstrap.recovery.backup
	if src.Recovery != nil && src.Recovery.Backup != nil {
		dstSpec.Recovery.Backup = &v1.LocalObjectReference{}
		dstSpec.Recovery.Backup.Name = src.Recovery.Backup.Name
	}

	// spec.bootstrap.recovery.recoveryTarget
	if src.Recovery != nil && src.Recovery.RecoveryTarget != nil {
		srcRecoveryTarget := src.Recovery.RecoveryTarget

		dstSpec.Recovery.RecoveryTarget = &v1.RecoveryTarget{}
		dstSpec.Recovery.RecoveryTarget.TargetTLI = srcRecoveryTarget.TargetTLI
		dstSpec.Recovery.RecoveryTarget.TargetXID = srcRecoveryTarget.TargetXID
		dstSpec.Recovery.RecoveryTarget.TargetName = srcRecoveryTarget.TargetName
		dstSpec.Recovery.RecoveryTarget.TargetLSN = srcRecoveryTarget.TargetLSN
		dstSpec.Recovery.RecoveryTarget.TargetTime = srcRecoveryTarget.TargetTime
		dstSpec.Recovery.RecoveryTarget.TargetImmediate = srcRecoveryTarget.TargetImmediate
		dstSpec.Recovery.RecoveryTarget.Exclusive = srcRecoveryTarget.Exclusive
	}

	// spec.bootstrap.pg_basebackup
	if src != nil && src.PgBaseBackup != nil {
		dstSpec.PgBaseBackup = &v1.BootstrapPgBaseBackup{}
		dstSpec.PgBaseBackup.Source = src.PgBaseBackup.Source
	}
}

// ConvertTo convert this backup configuration to the relative Hub version (v1)
func (src *BarmanObjectStoreConfiguration) ConvertTo(dst *v1.BarmanObjectStoreConfiguration) { //nolint:revive
	// spec.backup.barmanObjectStore
	s3Credentials := src.S3Credentials
	azureCredentials := src.AzureCredentials

	if s3Credentials != nil {
		dst.S3Credentials = &v1.S3Credentials{}
		dst.S3Credentials.AccessKeyIDReference.Key =
			s3Credentials.AccessKeyIDReference.Key
		dst.S3Credentials.AccessKeyIDReference.Name =
			s3Credentials.AccessKeyIDReference.Name
		dst.S3Credentials.SecretAccessKeyReference.Key =
			s3Credentials.SecretAccessKeyReference.Key
		dst.S3Credentials.SecretAccessKeyReference.Name =
			s3Credentials.SecretAccessKeyReference.Name
	}

	if azureCredentials != nil {
		dst.AzureCredentials = &v1.AzureCredentials{}

		if azureCredentials.StorageAccount != nil {
			dst.AzureCredentials.StorageAccount = &v1.SecretKeySelector{}
			dst.AzureCredentials.StorageAccount.Name =
				azureCredentials.StorageAccount.Name
			dst.AzureCredentials.StorageAccount.Key =
				azureCredentials.StorageAccount.Key
		}

		if azureCredentials.ConnectionString != nil {
			dst.AzureCredentials.ConnectionString = &v1.SecretKeySelector{}
			dst.AzureCredentials.ConnectionString.Name =
				azureCredentials.ConnectionString.Name
			dst.AzureCredentials.ConnectionString.Key =
				azureCredentials.ConnectionString.Key
		}

		if azureCredentials.StorageKey != nil {
			dst.AzureCredentials.StorageKey = &v1.SecretKeySelector{}
			dst.AzureCredentials.StorageKey.Name =
				azureCredentials.StorageKey.Name
			dst.AzureCredentials.StorageKey.Key =
				azureCredentials.StorageKey.Key
		}

		if azureCredentials.StorageSasToken != nil {
			dst.AzureCredentials.StorageSasToken = &v1.SecretKeySelector{}
			dst.AzureCredentials.StorageSasToken.Name =
				azureCredentials.StorageSasToken.Name
			dst.AzureCredentials.StorageSasToken.Key =
				azureCredentials.StorageSasToken.Key
		}
	}

	dst.EndpointURL =
		src.EndpointURL
	dst.DestinationPath =
		src.DestinationPath
	dst.ServerName =
		src.ServerName

	// spec.backup.barmanObjectStore.wal
	if src.Wal != nil {
		wal := src.Wal
		dst.Wal = &v1.WalBackupConfiguration{}
		dst.Wal.Compression = v1.CompressionType(
			wal.Compression)
		dst.Wal.Encryption = v1.EncryptionType(
			wal.Encryption)
	}

	// spec.backup.barmanObjectStore.data
	if src.Data != nil {
		data := src.Data
		dst.Data = &v1.DataBackupConfiguration{}
		dst.Data.Compression = v1.CompressionType(
			data.Compression)
		dst.Data.Encryption = v1.EncryptionType(
			data.Encryption)
		dst.Data.ImmediateCheckpoint = data.ImmediateCheckpoint
		dst.Data.Jobs = data.Jobs
	}

	// spec.backup.barmanObjectStore.endpointCA
	if src.EndpointCA != nil {
		dst.EndpointCA = &v1.SecretKeySelector{}
		dst.EndpointCA.LocalObjectReference.Name =
			src.EndpointCA.LocalObjectReference.Name
		dst.EndpointCA.Key =
			src.EndpointCA.Key
	}
}

// ConvertFrom converts from the Hub version (v1) to this version.
func (dst *Cluster) ConvertFrom(srcRaw conversion.Hub) error { //nolint:revive
	src := srcRaw.(*v1.Cluster)

	// objectmeta
	dst.ObjectMeta = src.ObjectMeta

	// spec
	dst.Spec.Description = src.Spec.Description
	dst.Spec.ImageName = src.Spec.ImageName
	dst.Spec.ImagePullPolicy = src.Spec.ImagePullPolicy
	dst.Spec.PostgresUID = src.Spec.PostgresUID
	dst.Spec.PostgresGID = src.Spec.PostgresGID
	dst.Spec.Instances = src.Spec.Instances
	dst.Spec.MinSyncReplicas = src.Spec.MinSyncReplicas
	dst.Spec.MaxSyncReplicas = src.Spec.MaxSyncReplicas
	dst.Spec.EnableSuperuserAccess = src.Spec.EnableSuperuserAccess
	dst.Spec.LogLevel = src.Spec.LogLevel

	// spec.postgresql
	dst.Spec.PostgresConfiguration.Parameters = src.Spec.PostgresConfiguration.Parameters
	dst.Spec.PostgresConfiguration.PgHBA = src.Spec.PostgresConfiguration.PgHBA
	dst.Spec.PostgresConfiguration.AdditionalLibraries = src.Spec.PostgresConfiguration.AdditionalLibraries

	// spec.bootstrap
	if src.Spec.Bootstrap != nil {
		dst.Spec.Bootstrap = &BootstrapConfiguration{}
		dst.Spec.Bootstrap.ConvertFrom(src.Spec.Bootstrap)
	}

	// spec.superuserSecret
	if src.Spec.SuperuserSecret != nil {
		dst.Spec.SuperuserSecret = &LocalObjectReference{}
		dst.Spec.SuperuserSecret.Name = src.Spec.SuperuserSecret.Name
	}

	if src.Spec.ImagePullSecrets != nil {
		dst.Spec.ImagePullSecrets = make([]LocalObjectReference, len(src.Spec.ImagePullSecrets))
		for idx := range src.Spec.ImagePullSecrets {
			dst.Spec.ImagePullSecrets[idx].Name = src.Spec.ImagePullSecrets[idx].Name
		}
	}

	// spec.storage
	srcStorageConf := src.Spec.StorageConfiguration
	dst.Spec.StorageConfiguration.StorageClass = srcStorageConf.StorageClass
	dst.Spec.StorageConfiguration.Size = srcStorageConf.Size
	dst.Spec.StorageConfiguration.ResizeInUseVolumes = srcStorageConf.ResizeInUseVolumes
	dst.Spec.StorageConfiguration.PersistentVolumeClaimTemplate = srcStorageConf.PersistentVolumeClaimTemplate

	dst.Spec.MaxStartDelay = src.Spec.MaxStartDelay
	dst.Spec.MaxStopDelay = src.Spec.MaxStopDelay

	// spec.affinity
	dst.Spec.Affinity.EnablePodAntiAffinity = src.Spec.Affinity.EnablePodAntiAffinity
	dst.Spec.Affinity.TopologyKey = src.Spec.Affinity.TopologyKey
	dst.Spec.Affinity.NodeSelector = src.Spec.Affinity.NodeSelector
	dst.Spec.Affinity.Tolerations = src.Spec.Affinity.Tolerations
	dst.Spec.Affinity.PodAntiAffinityType = src.Spec.Affinity.PodAntiAffinityType
	dst.Spec.Affinity.AdditionalPodAntiAffinity = src.Spec.Affinity.AdditionalPodAntiAffinity
	dst.Spec.Affinity.AdditionalPodAffinity = src.Spec.Affinity.AdditionalPodAffinity

	dst.Spec.Resources = src.Spec.Resources
	dst.Spec.PrimaryUpdateStrategy = PrimaryUpdateStrategy(src.Spec.PrimaryUpdateStrategy)

	// spec.certificates
	if src.Spec.Certificates != nil {
		dst.Spec.Certificates = &CertificatesConfiguration{}
		dst.Spec.Certificates.ServerTLSSecret = src.Spec.Certificates.ServerTLSSecret
		dst.Spec.Certificates.ServerCASecret = src.Spec.Certificates.ServerCASecret
		dst.Spec.Certificates.ServerAltDNSNames = src.Spec.Certificates.ServerAltDNSNames
		dst.Spec.Certificates.ClientCASecret = src.Spec.Certificates.ClientCASecret
		dst.Spec.Certificates.ReplicationTLSSecret = src.Spec.Certificates.ReplicationTLSSecret
	}

	// spec.backup
	if src.Spec.Backup != nil { // nolint:nestif
		dst.Spec.Backup = &BackupConfiguration{}

		// spec.backup.barmanObjectStore
		if src.Spec.Backup.BarmanObjectStore != nil {
			dst.Spec.Backup.BarmanObjectStore = &BarmanObjectStoreConfiguration{}
			dst.Spec.Backup.BarmanObjectStore.ConvertFrom(src.Spec.Backup.BarmanObjectStore)
		}
		dst.Spec.Backup.RetentionPolicy = src.Spec.Backup.RetentionPolicy
	}

	// spec.nodeMaintenanceWindow
	if src.Spec.NodeMaintenanceWindow != nil {
		dst.Spec.NodeMaintenanceWindow = &NodeMaintenanceWindow{}
		dst.Spec.NodeMaintenanceWindow.InProgress = src.Spec.NodeMaintenanceWindow.InProgress
		dst.Spec.NodeMaintenanceWindow.ReusePVC = src.Spec.NodeMaintenanceWindow.ReusePVC
	}

	// spec.replicaCluster
	if src.Spec.ReplicaCluster != nil {
		dst.Spec.ReplicaCluster = &ReplicaClusterConfiguration{}
		dst.Spec.ReplicaCluster.Enabled = src.Spec.ReplicaCluster.Enabled
		dst.Spec.ReplicaCluster.Source = src.Spec.ReplicaCluster.Source
	}

	if src.Spec.ExternalClusters != nil {
		dst.Spec.ExternalClusters = make([]ExternalCluster, len(src.Spec.ExternalClusters))
		for idx, entry := range src.Spec.ExternalClusters {
			dst.Spec.ExternalClusters[idx].Name = entry.Name
			dst.Spec.ExternalClusters[idx].ConnectionParameters = entry.ConnectionParameters
			dst.Spec.ExternalClusters[idx].SSLCert = entry.SSLCert
			dst.Spec.ExternalClusters[idx].SSLKey = entry.SSLKey
			dst.Spec.ExternalClusters[idx].SSLRootCert = entry.SSLRootCert
			dst.Spec.ExternalClusters[idx].Password = entry.Password
			if entry.BarmanObjectStore != nil {
				dst.Spec.ExternalClusters[idx].BarmanObjectStore = &BarmanObjectStoreConfiguration{}
				dst.Spec.ExternalClusters[idx].BarmanObjectStore.ConvertFrom(entry.BarmanObjectStore)
			}
		}
	}

	// status
	dst.Status.Instances = src.Status.Instances
	dst.Status.ReadyInstances = src.Status.ReadyInstances
	dst.Status.InstancesStatus = src.Status.InstancesStatus
	dst.Status.LatestGeneratedNode = src.Status.LatestGeneratedNode
	dst.Status.CurrentPrimary = src.Status.CurrentPrimary
	dst.Status.TargetPrimary = src.Status.TargetPrimary
	dst.Status.PVCCount = src.Status.PVCCount
	dst.Status.JobCount = src.Status.JobCount
	dst.Status.DanglingPVC = src.Status.DanglingPVC
	dst.Status.InitializingPVC = src.Status.InitializingPVC
	dst.Status.HealthyPVC = src.Status.HealthyPVC
	dst.Status.WriteService = src.Status.WriteService
	dst.Status.ReadService = src.Status.ReadService
	dst.Status.Phase = src.Status.Phase
	dst.Status.PhaseReason = src.Status.PhaseReason
	dst.Status.CommitHash = src.Status.CommitHash
	dst.Status.CurrentPrimaryTimestamp = src.Status.CurrentPrimaryTimestamp
	dst.Status.TargetPrimaryTimestamp = src.Status.TargetPrimaryTimestamp
	if src.Status.PoolerIntegrations != nil {
		dst.Status.PoolerIntegrations = &PoolerIntegrations{PgBouncerIntegration: PgbouncerIntegrationStatus{
			Secrets: src.Status.PoolerIntegrations.PgBouncerIntegration.Secrets,
		}}
	}
	dst.Status.SecretsResourceVersion.SuperuserSecretVersion =
		src.Status.SecretsResourceVersion.SuperuserSecretVersion
	dst.Status.SecretsResourceVersion.ReplicationSecretVersion =
		src.Status.SecretsResourceVersion.ReplicationSecretVersion
	dst.Status.SecretsResourceVersion.ApplicationSecretVersion =
		src.Status.SecretsResourceVersion.ApplicationSecretVersion
	dst.Status.SecretsResourceVersion.CASecretVersion = src.Status.SecretsResourceVersion.CASecretVersion
	dst.Status.SecretsResourceVersion.ClientCASecretVersion = src.Status.SecretsResourceVersion.ClientCASecretVersion
	dst.Status.SecretsResourceVersion.ServerCASecretVersion = src.Status.SecretsResourceVersion.ServerCASecretVersion
	dst.Status.SecretsResourceVersion.ServerSecretVersion = src.Status.SecretsResourceVersion.ServerSecretVersion
	dst.Status.SecretsResourceVersion.Metrics = src.Status.SecretsResourceVersion.Metrics
	dst.Status.SecretsResourceVersion.BarmanEndpointCA = src.Status.SecretsResourceVersion.BarmanEndpointCA
	dst.Status.ConfigMapResourceVersion.Metrics = src.Status.ConfigMapResourceVersion.Metrics
	dst.Status.Certificates.ServerTLSSecret = src.Status.Certificates.ServerTLSSecret
	dst.Status.Certificates.ServerCASecret = src.Status.Certificates.ServerCASecret
	dst.Status.Certificates.ClientCASecret = src.Status.Certificates.ClientCASecret
	dst.Status.Certificates.ReplicationTLSSecret = src.Status.Certificates.ReplicationTLSSecret
	dst.Status.Certificates.ServerAltDNSNames = src.Status.Certificates.ServerAltDNSNames
	dst.Status.Certificates.Expirations = src.Status.Certificates.Expirations

	return nil
}

// ConvertFrom convert a BarmanObjectStoreConfiguration from the v1 API to the v1alpha1
func (dst *BarmanObjectStoreConfiguration) ConvertFrom(src *v1.BarmanObjectStoreConfiguration) { //nolint:revive
	s3Credentials := src.S3Credentials
	azureCredentials := src.AzureCredentials

	if s3Credentials != nil {
		dst.S3Credentials = &S3Credentials{}
		dst.S3Credentials.AccessKeyIDReference.Key = s3Credentials.AccessKeyIDReference.Key
		dst.S3Credentials.AccessKeyIDReference.Name = s3Credentials.AccessKeyIDReference.Name
		dst.S3Credentials.SecretAccessKeyReference.Key =
			s3Credentials.SecretAccessKeyReference.Key
		dst.S3Credentials.SecretAccessKeyReference.Name =
			s3Credentials.SecretAccessKeyReference.Name
	}

	if azureCredentials != nil {
		dst.AzureCredentials = &AzureCredentials{}
	}

	if azureCredentials != nil && azureCredentials.ConnectionString != nil {
		dst.AzureCredentials.ConnectionString = &SecretKeySelector{}
		dst.AzureCredentials.ConnectionString.Name = azureCredentials.ConnectionString.Name
		dst.AzureCredentials.ConnectionString.Key = azureCredentials.ConnectionString.Key
	}

	if azureCredentials != nil && azureCredentials.StorageAccount != nil {
		dst.AzureCredentials.StorageAccount = &SecretKeySelector{}
		dst.AzureCredentials.StorageAccount.Name = azureCredentials.StorageAccount.Name
		dst.AzureCredentials.StorageAccount.Key = azureCredentials.StorageAccount.Key
	}

	if azureCredentials != nil && azureCredentials.StorageKey != nil {
		dst.AzureCredentials.StorageKey = &SecretKeySelector{}
		dst.AzureCredentials.StorageKey.Name = azureCredentials.StorageKey.Name
		dst.AzureCredentials.StorageKey.Key = azureCredentials.StorageKey.Key
	}

	if azureCredentials != nil && azureCredentials.StorageSasToken != nil {
		dst.AzureCredentials.StorageSasToken = &SecretKeySelector{}
		dst.AzureCredentials.StorageSasToken.Name = azureCredentials.StorageSasToken.Name
		dst.AzureCredentials.StorageSasToken.Key = azureCredentials.StorageSasToken.Key
	}

	dst.EndpointURL = src.EndpointURL
	dst.DestinationPath = src.DestinationPath
	dst.ServerName = src.ServerName

	// spec.backup.barmanObjectStore.wal
	if src.Wal != nil {
		wal := src.Wal
		dst.Wal = &WalBackupConfiguration{}
		dst.Wal.Compression = CompressionType(
			wal.Compression)
		dst.Wal.Encryption = EncryptionType(
			wal.Encryption)
	}

	// spec.backup.barmanObjectStore.data
	if src.Data != nil {
		data := src.Data
		dst.Data = &DataBackupConfiguration{}
		dst.Data.Compression = CompressionType(
			data.Compression)
		dst.Data.Encryption = EncryptionType(
			data.Encryption)
		dst.Data.ImmediateCheckpoint = data.ImmediateCheckpoint
		dst.Data.Jobs = data.Jobs
	}

	// spec.backup.barmanObjectStore.endpointCA
	if src.EndpointCA != nil {
		dst.EndpointCA = &SecretKeySelector{}
		dst.EndpointCA.LocalObjectReference.Name =
			src.EndpointCA.LocalObjectReference.Name
		dst.EndpointCA.Key =
			src.EndpointCA.Key
	}
}

// ConvertFrom converts from the Hub version (v1) to this version.
func (dst *BootstrapConfiguration) ConvertFrom(srcSpec *v1.BootstrapConfiguration) { //nolint:revive
	// spec.bootstrap.initdb
	if srcSpec.InitDB != nil {
		srcInitDB := srcSpec.InitDB

		dst.InitDB = &BootstrapInitDB{}
		dst.InitDB.Database = srcInitDB.Database
		dst.InitDB.Owner = srcInitDB.Owner
		dst.InitDB.Options = srcInitDB.Options
		dst.InitDB.PostInitSQL = srcInitDB.PostInitSQL
	}

	// spec.bootstrap.initdb.secret
	if srcSpec.InitDB != nil && srcSpec.InitDB.Secret != nil {
		srcInitDB := srcSpec.InitDB

		dst.InitDB.Secret = &LocalObjectReference{}
		dst.InitDB.Secret.Name = srcInitDB.Secret.Name
	}

	// spec.bootstrap.recovery
	if srcSpec.Recovery != nil {
		dst.Recovery = &BootstrapRecovery{}
		dst.Recovery.Source = srcSpec.Recovery.Source
	}

	// spec.bootstrap.recovery.backup
	if srcSpec.Recovery != nil && srcSpec.Recovery.Backup != nil {
		dst.Recovery.Backup = &LocalObjectReference{}
		dst.Recovery.Backup.Name = srcSpec.Recovery.Backup.Name
	}

	// spec.bootstrap.recovery.recoveryTarget
	if srcSpec.Recovery != nil && srcSpec.Recovery.RecoveryTarget != nil {
		srcRecoveryTarget := srcSpec.Recovery.RecoveryTarget

		dst.Recovery.RecoveryTarget = &RecoveryTarget{}
		dst.Recovery.RecoveryTarget.TargetTLI = srcRecoveryTarget.TargetTLI
		dst.Recovery.RecoveryTarget.TargetXID = srcRecoveryTarget.TargetXID
		dst.Recovery.RecoveryTarget.TargetName = srcRecoveryTarget.TargetName
		dst.Recovery.RecoveryTarget.TargetLSN = srcRecoveryTarget.TargetLSN
		dst.Recovery.RecoveryTarget.TargetTime = srcRecoveryTarget.TargetTime
		dst.Recovery.RecoveryTarget.TargetImmediate = srcRecoveryTarget.TargetImmediate
		dst.Recovery.RecoveryTarget.Exclusive = srcRecoveryTarget.Exclusive
	}

	// spec.bootstrap.pg_basebackup
	if srcSpec != nil && srcSpec.PgBaseBackup != nil {
		dst.PgBaseBackup = &BootstrapPgBaseBackup{}
		dst.PgBaseBackup.Source = srcSpec.PgBaseBackup.Source
	}
}
