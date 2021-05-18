/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1alpha1

// LocalObjectReference contains enough information to let you locate a
// local object with a known type inside the same namespace
type LocalObjectReference struct {
	// Name of the referent.
	Name string `json:"name"`
}

// SecretKeySelector contains enough information to let you locate
// key of a Secret
type SecretKeySelector struct {
	// The name of the secret in the pod's namespace to select from.
	LocalObjectReference `json:",inline"`
	// The key to select
	Key string `json:"key"`
}

// ConfigMapKeySelector contains enough information to let you locate
// key of a ConfigMap
type ConfigMapKeySelector struct {
	// The name of the secret in the pod's namespace to select from.
	LocalObjectReference `json:",inline"`
	// The key to select
	Key string `json:"key"`
}
