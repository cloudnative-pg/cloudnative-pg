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
func (src *Backup) ConvertTo(dstRaw conversion.Hub) error { //nolint:golint
	dst := dstRaw.(*v1.Backup)

	// objectmeta
	dst.ObjectMeta = src.ObjectMeta

	// spec
	dst.Spec.Cluster = src.Spec.Cluster

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

	// status.s3Credentials
	dst.Status.S3Credentials.AccessKeyIDReference = src.Status.S3Credentials.AccessKeyIDReference
	dst.Status.S3Credentials.SecretAccessKeyReference = src.Status.S3Credentials.SecretAccessKeyReference

	return nil
}

// ConvertFrom converts from the Hub version (v1) to this version.
func (dst *Backup) ConvertFrom(srcRaw conversion.Hub) error { //nolint:golint
	src := srcRaw.(*v1.Backup)

	// objectmeta
	dst.ObjectMeta = src.ObjectMeta

	// spec
	dst.Spec.Cluster = src.Spec.Cluster

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

	// status.s3Credentials
	dst.Status.S3Credentials.AccessKeyIDReference = src.Status.S3Credentials.AccessKeyIDReference
	dst.Status.S3Credentials.SecretAccessKeyReference = src.Status.S3Credentials.SecretAccessKeyReference

	return nil
}
