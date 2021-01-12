/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package v1alpha1

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:object:generate=false

// BackupCommon is implemented by all types of PostgreSQL backups
type BackupCommon interface {
	// GetStatus returns a pointer to the backup status that the caller may update
	GetStatus() *BackupStatus

	// GetName gets the backup name
	GetName() string

	// GetNamespace gets the backup namespace
	GetNamespace() string

	// GetKubernetesObject gets the kubernetes object
	GetKubernetesObject() client.Object
}
