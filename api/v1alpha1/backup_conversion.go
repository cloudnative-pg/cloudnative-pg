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
func (src *Backup) ConvertTo(dstRaw conversion.Hub) error { //nolint:revive
	dst := dstRaw.(*v1.Backup)

	// objectmeta
	dst.ObjectMeta = src.ObjectMeta

	// spec
	dst.Spec.Cluster.Name = src.Spec.Cluster.Name

	// status
	dst.Status.EndpointURL = src.Status.EndpointURL
	dst.Status.DestinationPath = src.Status.DestinationPath
	dst.Status.ServerName = src.Status.ServerName
	dst.Status.Encryption = src.Status.Encryption
	dst.Status.BackupID = src.Status.BackupID
	dst.Status.Phase = v1.BackupPhase(src.Status.Phase)
	dst.Status.StartedAt = src.Status.StartedAt
	dst.Status.StoppedAt = src.Status.StoppedAt
	dst.Status.Error = src.Status.Error
	dst.Status.CommandOutput = src.Status.CommandOutput
	dst.Status.CommandError = src.Status.CommandError
	dst.Status.BeginWal = src.Status.BeginWal
	dst.Status.EndWal = src.Status.EndWal
	dst.Status.BeginLSN = src.Status.BeginLSN
	dst.Status.EndLSN = src.Status.EndLSN
	if src.Status.InstanceID != nil {
		dst.Status.InstanceID = &v1.InstanceID{
			PodName:     src.Status.InstanceID.PodName,
			ContainerID: src.Status.InstanceID.ContainerID,
		}
	}

	// status.s3Credentials
	if src.Status.S3Credentials != nil {
		dst.Status.S3Credentials = &v1.S3Credentials{}
		dst.Status.S3Credentials.AccessKeyIDReference.Key = src.Status.S3Credentials.AccessKeyIDReference.Key
		dst.Status.S3Credentials.AccessKeyIDReference.LocalObjectReference.Name =
			src.Status.S3Credentials.AccessKeyIDReference.LocalObjectReference.Name
		dst.Status.S3Credentials.SecretAccessKeyReference.Key = src.Status.S3Credentials.SecretAccessKeyReference.Key
		dst.Status.S3Credentials.SecretAccessKeyReference.LocalObjectReference.Name =
			src.Status.S3Credentials.SecretAccessKeyReference.Name
	}

	// status.azureCredentials
	if src.Status.AzureCredentials != nil {
		dst.Status.AzureCredentials = &v1.AzureCredentials{}

		if dst.Status.AzureCredentials.StorageAccount != nil {
			dst.Status.AzureCredentials.StorageAccount = &v1.SecretKeySelector{}
			dst.Status.AzureCredentials.StorageAccount.Name =
				src.Status.AzureCredentials.StorageAccount.Name
			dst.Status.AzureCredentials.StorageAccount.Key =
				src.Status.AzureCredentials.StorageAccount.Key
		}

		if src.Status.AzureCredentials.StorageKey != nil {
			dst.Status.AzureCredentials.StorageKey = &v1.SecretKeySelector{}
			dst.Status.AzureCredentials.StorageKey.Name =
				src.Status.AzureCredentials.StorageKey.Name
			dst.Status.AzureCredentials.StorageKey.Key =
				src.Status.AzureCredentials.StorageKey.Key
		}

		if src.Status.AzureCredentials.StorageSasToken != nil {
			dst.Status.AzureCredentials.StorageSasToken = &v1.SecretKeySelector{}
			dst.Status.AzureCredentials.StorageSasToken.Name =
				src.Status.AzureCredentials.StorageSasToken.Name
			dst.Status.AzureCredentials.StorageSasToken.Key =
				src.Status.AzureCredentials.StorageKey.Key
		}
	}

	return nil
}

// ConvertFrom converts from the Hub version (v1) to this version.
func (dst *Backup) ConvertFrom(srcRaw conversion.Hub) error { //nolint:revive
	src := srcRaw.(*v1.Backup)

	// objectmeta
	dst.ObjectMeta = src.ObjectMeta

	// spec
	dst.Spec.Cluster.Name = src.Spec.Cluster.Name

	// status
	dst.Status.EndpointURL = src.Status.EndpointURL
	dst.Status.DestinationPath = src.Status.DestinationPath
	dst.Status.ServerName = src.Status.ServerName
	dst.Status.Encryption = src.Status.Encryption
	dst.Status.BackupID = src.Status.BackupID
	dst.Status.Phase = BackupPhase(src.Status.Phase)
	dst.Status.StartedAt = src.Status.StartedAt
	dst.Status.StoppedAt = src.Status.StoppedAt
	dst.Status.Error = src.Status.Error
	dst.Status.CommandOutput = src.Status.CommandOutput
	dst.Status.CommandError = src.Status.CommandError
	dst.Status.BeginWal = src.Status.BeginWal
	dst.Status.EndWal = src.Status.EndWal
	dst.Status.BeginLSN = src.Status.BeginLSN
	dst.Status.EndLSN = src.Status.EndLSN
	if src.Status.InstanceID != nil {
		dst.Status.InstanceID = &InstanceID{
			PodName:     src.Status.InstanceID.PodName,
			ContainerID: src.Status.InstanceID.ContainerID,
		}
	}

	// status.s3Credentials
	if src.Status.S3Credentials != nil {
		dst.Status.S3Credentials = &S3Credentials{}
		dst.Status.S3Credentials.AccessKeyIDReference.Key = src.Status.S3Credentials.AccessKeyIDReference.Key
		dst.Status.S3Credentials.AccessKeyIDReference.LocalObjectReference.Name =
			src.Status.S3Credentials.AccessKeyIDReference.LocalObjectReference.Name
		dst.Status.S3Credentials.SecretAccessKeyReference.Key = src.Status.S3Credentials.SecretAccessKeyReference.Key
		dst.Status.S3Credentials.SecretAccessKeyReference.LocalObjectReference.Name =
			src.Status.S3Credentials.SecretAccessKeyReference.LocalObjectReference.Name
	}

	// status.azureCredentials
	if src.Status.AzureCredentials != nil {
		dst.Status.AzureCredentials = &AzureCredentials{}
		dst.Status.AzureCredentials.StorageAccount.Name =
			src.Status.AzureCredentials.StorageAccount.Name

		if src.Status.AzureCredentials.StorageKey != nil {
			dst.Status.AzureCredentials.StorageKey = &SecretKeySelector{}
			dst.Status.AzureCredentials.StorageKey.Name =
				src.Status.AzureCredentials.StorageKey.Name
			dst.Status.AzureCredentials.StorageKey.Key =
				src.Status.AzureCredentials.StorageKey.Key
		}

		if src.Status.AzureCredentials.StorageSasToken != nil {
			dst.Status.AzureCredentials.StorageSasToken = &SecretKeySelector{}
			dst.Status.AzureCredentials.StorageSasToken.Name =
				src.Status.AzureCredentials.StorageSasToken.Name
			dst.Status.AzureCredentials.StorageSasToken.Key =
				src.Status.AzureCredentials.StorageKey.Key
		}
	}

	return nil
}
