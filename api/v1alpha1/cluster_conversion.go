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
func (src *Cluster) ConvertTo(dstRaw conversion.Hub) error { //nolint:golint
	dst := dstRaw.(*v1.Cluster)

	// objectmeta
	dst.ObjectMeta = src.ObjectMeta

	// spec
	dst.Spec.Description = src.Spec.Description
	dst.Spec.ImageName = src.Spec.ImageName
	dst.Spec.PostgresUID = src.Spec.PostgresUID
	dst.Spec.PostgresGID = src.Spec.PostgresGID
	dst.Spec.Instances = src.Spec.Instances
	dst.Spec.MinSyncReplicas = src.Spec.MinSyncReplicas
	dst.Spec.MaxSyncReplicas = src.Spec.MaxSyncReplicas

	// spec.postgresql
	dst.Spec.PostgresConfiguration.Parameters = src.Spec.PostgresConfiguration.Parameters
	dst.Spec.PostgresConfiguration.PgHBA = src.Spec.PostgresConfiguration.PgHBA

	// spec.bootstrap
	if src.Spec.Bootstrap != nil {
		dst.Spec.Bootstrap = &v1.BootstrapConfiguration{}
		src.Spec.Bootstrap.ConvertTo(dst.Spec.Bootstrap)
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

	dst.Spec.Resources = src.Spec.Resources
	dst.Spec.PrimaryUpdateStrategy = v1.PrimaryUpdateStrategy(src.Spec.PrimaryUpdateStrategy)

	// spec.backup
	if src.Spec.Backup != nil {
		dst.Spec.Backup = &v1.BackupConfiguration{}

		// spec.backup.barmanObjectStore
		if src.Spec.Backup.BarmanObjectStore != nil {
			s3Credentials := src.Spec.Backup.BarmanObjectStore.S3Credentials
			dst.Spec.Backup.BarmanObjectStore = &v1.BarmanObjectStoreConfiguration{}
			dst.Spec.Backup.BarmanObjectStore.S3Credentials.AccessKeyIDReference.Key =
				s3Credentials.AccessKeyIDReference.Key
			dst.Spec.Backup.BarmanObjectStore.S3Credentials.AccessKeyIDReference.Name =
				s3Credentials.AccessKeyIDReference.Name
			dst.Spec.Backup.BarmanObjectStore.S3Credentials.SecretAccessKeyReference.Key =
				s3Credentials.SecretAccessKeyReference.Key
			dst.Spec.Backup.BarmanObjectStore.S3Credentials.SecretAccessKeyReference.Name =
				s3Credentials.SecretAccessKeyReference.Name
		}

		dst.Spec.Backup.BarmanObjectStore.EndpointURL = src.Spec.Backup.BarmanObjectStore.EndpointURL
		dst.Spec.Backup.BarmanObjectStore.DestinationPath = src.Spec.Backup.BarmanObjectStore.DestinationPath
		dst.Spec.Backup.BarmanObjectStore.ServerName = src.Spec.Backup.BarmanObjectStore.ServerName

		// spec.backup.barmanObjectStore.wal
		if src.Spec.Backup.BarmanObjectStore.Wal != nil {
			wal := src.Spec.Backup.BarmanObjectStore.Wal
			dst.Spec.Backup.BarmanObjectStore.Wal = &v1.WalBackupConfiguration{}
			dst.Spec.Backup.BarmanObjectStore.Wal.Compression = v1.CompressionType(
				wal.Compression)
			dst.Spec.Backup.BarmanObjectStore.Wal.Encryption = v1.EncryptionType(
				wal.Encryption)
		}

		// spec.backup.barmanObjectStore.data
		if src.Spec.Backup.BarmanObjectStore.Data != nil {
			data := src.Spec.Backup.BarmanObjectStore.Data
			dst.Spec.Backup.BarmanObjectStore.Data = &v1.DataBackupConfiguration{}
			dst.Spec.Backup.BarmanObjectStore.Data.Compression = v1.CompressionType(
				data.Compression)
			dst.Spec.Backup.BarmanObjectStore.Data.Encryption = v1.EncryptionType(
				data.Encryption)
			dst.Spec.Backup.BarmanObjectStore.Data.ImmediateCheckpoint = data.ImmediateCheckpoint
			dst.Spec.Backup.BarmanObjectStore.Data.Jobs = data.Jobs
		}
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

	// spec.externalServers
	if src.Spec.ExternalClusters != nil {
		dst.Spec.ExternalClusters = make([]v1.ExternalCluster, len(src.Spec.ExternalClusters))
		for idx, entry := range src.Spec.ExternalClusters {
			dst.Spec.ExternalClusters[idx].Name = entry.Name
			dst.Spec.ExternalClusters[idx].ConnectionParameters = entry.ConnectionParameters
			dst.Spec.ExternalClusters[idx].SSLCert = entry.SSLCert
			dst.Spec.ExternalClusters[idx].SSLKey = entry.SSLKey
			dst.Spec.ExternalClusters[idx].SSLRootCert = entry.SSLRootCert
			dst.Spec.ExternalClusters[idx].Password = entry.Password
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
	dst.Status.WriteService = src.Status.WriteService
	dst.Status.ReadService = src.Status.ReadService
	dst.Status.Phase = src.Status.Phase
	dst.Status.PhaseReason = src.Status.PhaseReason
	dst.Status.SecretsResourceVersion.SuperuserSecretVersion =
		src.Status.SecretsResourceVersion.SuperuserSecretVersion
	dst.Status.SecretsResourceVersion.ReplicationSecretVersion =
		src.Status.SecretsResourceVersion.ReplicationSecretVersion
	dst.Status.SecretsResourceVersion.ReplicationSecretVersion =
		src.Status.SecretsResourceVersion.ReplicationSecretVersion
	dst.Status.SecretsResourceVersion.CASecretVersion = src.Status.SecretsResourceVersion.CASecretVersion
	dst.Status.SecretsResourceVersion.ServerSecretVersion = src.Status.SecretsResourceVersion.ServerSecretVersion

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
	}

	// spec.bootstrap.initdb.secret
	if src.InitDB != nil && src.InitDB.Secret != nil {
		dstSpec.InitDB.Secret = &v1.LocalObjectReference{}
		dstSpec.InitDB.Secret.Name = src.InitDB.Secret.Name
	}

	// spec.bootstrap.recovery
	if src.Recovery != nil {
		dstSpec.Recovery = &v1.BootstrapRecovery{}
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

// ConvertFrom converts from the Hub version (v1) to this version.
func (dst *Cluster) ConvertFrom(srcRaw conversion.Hub) error { //nolint:golint
	src := srcRaw.(*v1.Cluster)

	// objectmeta
	dst.ObjectMeta = src.ObjectMeta

	// spec
	dst.Spec.Description = src.Spec.Description
	dst.Spec.ImageName = src.Spec.ImageName
	dst.Spec.PostgresUID = src.Spec.PostgresUID
	dst.Spec.PostgresGID = src.Spec.PostgresGID
	dst.Spec.Instances = src.Spec.Instances
	dst.Spec.MinSyncReplicas = src.Spec.MinSyncReplicas
	dst.Spec.MaxSyncReplicas = src.Spec.MaxSyncReplicas

	// spec.postgresql
	dst.Spec.PostgresConfiguration.Parameters = src.Spec.PostgresConfiguration.Parameters
	dst.Spec.PostgresConfiguration.PgHBA = src.Spec.PostgresConfiguration.PgHBA

	// spec.bootstrap
	if src.Spec.Bootstrap != nil {
		dst.Spec.Bootstrap = &BootstrapConfiguration{}
		dst.Spec.Bootstrap.ConvertFrom(src.Spec.Bootstrap)
	}

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

	dst.Spec.Resources = src.Spec.Resources
	dst.Spec.PrimaryUpdateStrategy = PrimaryUpdateStrategy(src.Spec.PrimaryUpdateStrategy)

	// spec.backup
	if src.Spec.Backup != nil {
		dst.Spec.Backup = &BackupConfiguration{}

		// spec.backup.barmanObjectStore
		if src.Spec.Backup.BarmanObjectStore != nil {
			s3Credentials := src.Spec.Backup.BarmanObjectStore.S3Credentials
			dst.Spec.Backup.BarmanObjectStore = &BarmanObjectStoreConfiguration{}
			dst.Spec.Backup.BarmanObjectStore.S3Credentials.AccessKeyIDReference.Key = s3Credentials.AccessKeyIDReference.Key
			dst.Spec.Backup.BarmanObjectStore.S3Credentials.AccessKeyIDReference.Name = s3Credentials.AccessKeyIDReference.Name
			dst.Spec.Backup.BarmanObjectStore.S3Credentials.SecretAccessKeyReference.Key =
				s3Credentials.SecretAccessKeyReference.Key
			dst.Spec.Backup.BarmanObjectStore.S3Credentials.SecretAccessKeyReference.Name =
				s3Credentials.SecretAccessKeyReference.Name
		}

		dst.Spec.Backup.BarmanObjectStore.EndpointURL = src.Spec.Backup.BarmanObjectStore.EndpointURL
		dst.Spec.Backup.BarmanObjectStore.DestinationPath = src.Spec.Backup.BarmanObjectStore.DestinationPath
		dst.Spec.Backup.BarmanObjectStore.ServerName = src.Spec.Backup.BarmanObjectStore.ServerName

		// spec.backup.barmanObjectStore.wal
		if src.Spec.Backup.BarmanObjectStore.Wal != nil {
			wal := src.Spec.Backup.BarmanObjectStore.Wal
			dst.Spec.Backup.BarmanObjectStore.Wal = &WalBackupConfiguration{}
			dst.Spec.Backup.BarmanObjectStore.Wal.Compression = CompressionType(
				wal.Compression)
			dst.Spec.Backup.BarmanObjectStore.Wal.Encryption = EncryptionType(
				wal.Encryption)
		}

		// spec.backup.barmanObjectStore.data
		if src.Spec.Backup.BarmanObjectStore.Data != nil {
			data := src.Spec.Backup.BarmanObjectStore.Data
			dst.Spec.Backup.BarmanObjectStore.Data = &DataBackupConfiguration{}
			dst.Spec.Backup.BarmanObjectStore.Data.Compression = CompressionType(
				data.Compression)
			dst.Spec.Backup.BarmanObjectStore.Data.Encryption = EncryptionType(
				data.Encryption)
			dst.Spec.Backup.BarmanObjectStore.Data.ImmediateCheckpoint = data.ImmediateCheckpoint
			dst.Spec.Backup.BarmanObjectStore.Data.Jobs = data.Jobs
		}
	}

	// spec.nodeMaintenanceWindow
	if src.Spec.NodeMaintenanceWindow != nil {
		dst.Spec.NodeMaintenanceWindow = &NodeMaintenanceWindow{}
		dst.Spec.NodeMaintenanceWindow.InProgress = src.Spec.NodeMaintenanceWindow.InProgress
		dst.Spec.NodeMaintenanceWindow.ReusePVC = src.Spec.NodeMaintenanceWindow.ReusePVC
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
	dst.Status.WriteService = src.Status.WriteService
	dst.Status.ReadService = src.Status.ReadService
	dst.Status.Phase = src.Status.Phase
	dst.Status.PhaseReason = src.Status.PhaseReason
	dst.Status.SecretsResourceVersion.SuperuserSecretVersion =
		src.Status.SecretsResourceVersion.SuperuserSecretVersion
	dst.Status.SecretsResourceVersion.ReplicationSecretVersion =
		src.Status.SecretsResourceVersion.ReplicationSecretVersion
	dst.Status.SecretsResourceVersion.ReplicationSecretVersion =
		src.Status.SecretsResourceVersion.ReplicationSecretVersion
	dst.Status.SecretsResourceVersion.CASecretVersion = src.Status.SecretsResourceVersion.CASecretVersion
	dst.Status.SecretsResourceVersion.ServerSecretVersion = src.Status.SecretsResourceVersion.ServerSecretVersion

	return nil
}

// ConvertFrom converts from the Hub version (v1) to this version.
func (dst *BootstrapConfiguration) ConvertFrom(srcSpec *v1.BootstrapConfiguration) { //nolint:golint
	// spec.bootstrap.initdb
	if srcSpec.InitDB != nil {
		srcInitDB := srcSpec.InitDB

		dst.InitDB = &BootstrapInitDB{}
		dst.InitDB.Database = srcInitDB.Database
		dst.InitDB.Owner = srcInitDB.Owner
		dst.InitDB.Options = srcInitDB.Options
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
