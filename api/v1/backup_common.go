/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:object:generate=false

// BackupCommon is implemented by all types of PostgreSQL backups
type BackupCommon interface {
	// GetStatus returns a pointer to the backup status that the caller may update
	GetStatus() *BackupStatus

	// GetMetadata returns a pointer to the object metadata that the caller may update
	GetMetadata() *metav1.ObjectMeta

	// GetName gets the backup name
	GetName() string

	// GetNamespace gets the backup namespace
	GetNamespace() string

	// GetKubernetesObject gets the kubernetes object
	GetKubernetesObject() client.Object
}
