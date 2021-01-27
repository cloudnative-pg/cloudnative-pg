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

		// spec.bootstrap.initdb
		if src.Spec.Bootstrap.InitDB != nil {
			srcInitDB := src.Spec.Bootstrap.InitDB

			dst.Spec.Bootstrap.InitDB = &v1.BootstrapInitDB{}
			dst.Spec.Bootstrap.InitDB.Database = srcInitDB.Database
			dst.Spec.Bootstrap.InitDB.Owner = srcInitDB.Owner
			dst.Spec.Bootstrap.InitDB.Secret = srcInitDB.Secret
			dst.Spec.Bootstrap.InitDB.Options = srcInitDB.Options
		}

		// spec.bootstrap.recovery
		if src.Spec.Bootstrap.Recovery != nil {
			dst.Spec.Bootstrap.Recovery = &v1.BootstrapRecovery{}
			dst.Spec.Bootstrap.Recovery.Backup = src.Spec.Bootstrap.Recovery.Backup

			if src.Spec.Bootstrap.Recovery.RecoveryTarget != nil {
				srcRecoveryTarget := src.Spec.Bootstrap.Recovery.RecoveryTarget

				dst.Spec.Bootstrap.Recovery.RecoveryTarget = &v1.RecoveryTarget{}
				dst.Spec.Bootstrap.Recovery.RecoveryTarget.TargetTLI = srcRecoveryTarget.TargetTLI
				dst.Spec.Bootstrap.Recovery.RecoveryTarget.TargetXID = srcRecoveryTarget.TargetXID
				dst.Spec.Bootstrap.Recovery.RecoveryTarget.TargetName = srcRecoveryTarget.TargetName
				dst.Spec.Bootstrap.Recovery.RecoveryTarget.TargetLSN = srcRecoveryTarget.TargetLSN
				dst.Spec.Bootstrap.Recovery.RecoveryTarget.TargetTime = srcRecoveryTarget.TargetTime
				dst.Spec.Bootstrap.Recovery.RecoveryTarget.TargetImmediate = srcRecoveryTarget.TargetImmediate
				dst.Spec.Bootstrap.Recovery.RecoveryTarget.Exclusive = srcRecoveryTarget.Exclusive
			}
		}
	}

	dst.Spec.SuperuserSecret = src.Spec.SuperuserSecret
	dst.Spec.ImagePullSecrets = src.Spec.ImagePullSecrets

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
			dst.Spec.Backup.BarmanObjectStore.S3Credentials.AccessKeyIDReference = s3Credentials.AccessKeyIDReference
			dst.Spec.Backup.BarmanObjectStore.S3Credentials.SecretAccessKeyReference = s3Credentials.SecretAccessKeyReference
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

	return nil
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

		// spec.bootstrap.initdb
		if src.Spec.Bootstrap.InitDB != nil {
			srcInitDB := src.Spec.Bootstrap.InitDB

			dst.Spec.Bootstrap.InitDB = &BootstrapInitDB{}
			dst.Spec.Bootstrap.InitDB.Database = srcInitDB.Database
			dst.Spec.Bootstrap.InitDB.Owner = srcInitDB.Owner
			dst.Spec.Bootstrap.InitDB.Secret = srcInitDB.Secret
			dst.Spec.Bootstrap.InitDB.Options = srcInitDB.Options
		}

		// spec.bootstrap.recovery
		if src.Spec.Bootstrap.Recovery != nil {
			dst.Spec.Bootstrap.Recovery = &BootstrapRecovery{}
			dst.Spec.Bootstrap.Recovery.Backup = src.Spec.Bootstrap.Recovery.Backup

			if src.Spec.Bootstrap.Recovery.RecoveryTarget != nil {
				srcRecoveryTarget := src.Spec.Bootstrap.Recovery.RecoveryTarget

				dst.Spec.Bootstrap.Recovery.RecoveryTarget = &RecoveryTarget{}
				dst.Spec.Bootstrap.Recovery.RecoveryTarget.TargetTLI = srcRecoveryTarget.TargetTLI
				dst.Spec.Bootstrap.Recovery.RecoveryTarget.TargetXID = srcRecoveryTarget.TargetXID
				dst.Spec.Bootstrap.Recovery.RecoveryTarget.TargetName = srcRecoveryTarget.TargetName
				dst.Spec.Bootstrap.Recovery.RecoveryTarget.TargetLSN = srcRecoveryTarget.TargetLSN
				dst.Spec.Bootstrap.Recovery.RecoveryTarget.TargetTime = srcRecoveryTarget.TargetTime
				dst.Spec.Bootstrap.Recovery.RecoveryTarget.TargetImmediate = srcRecoveryTarget.TargetImmediate
				dst.Spec.Bootstrap.Recovery.RecoveryTarget.Exclusive = srcRecoveryTarget.Exclusive
			}
		}
	}

	dst.Spec.SuperuserSecret = src.Spec.SuperuserSecret
	dst.Spec.ImagePullSecrets = src.Spec.ImagePullSecrets

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
			dst.Spec.Backup.BarmanObjectStore.S3Credentials.AccessKeyIDReference = s3Credentials.AccessKeyIDReference
			dst.Spec.Backup.BarmanObjectStore.S3Credentials.SecretAccessKeyReference = s3Credentials.SecretAccessKeyReference
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

	return nil
}
