/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package v1

import corev1 "k8s.io/api/core/v1"

// IsPaused returns whether all database should be paused or not.
func (in PgBouncerSpec) IsPaused() bool {
	return in.Paused != nil && *in.Paused
}

// GetAuthQuerySecretName returns the specified AuthQuerySecret name for PgBouncer
// if provided or the default name otherwise.
func (in *Pooler) GetAuthQuerySecretName() string {
	if in.Spec.PgBouncer != nil && in.Spec.PgBouncer.AuthQuerySecret != nil {
		return in.Spec.PgBouncer.AuthQuerySecret.Name
	}

	return in.Spec.Cluster.Name + DefaultPgBouncerPoolerSecretSuffix
}

// GetServerTLSSecretName returns the specified server TLS secret name
// for PgBouncer if provided or the default name otherwise.
func (in *Pooler) GetServerTLSSecretName() string {
	if in.Spec.PgBouncer != nil && in.Spec.PgBouncer.ServerTLSSecret != nil {
		return in.Spec.PgBouncer.ServerTLSSecret.Name
	}

	return ""
}

// GetServerCASecretNameOrDefault returns the specified server CA secret name
// for PgBouncer if provided or the default name otherwise.
func (in *Pooler) GetServerCASecretNameOrDefault(cluster *Cluster) string {
	if in.Spec.PgBouncer != nil && in.Spec.PgBouncer.ServerCASecret != nil {
		return in.Spec.PgBouncer.ServerCASecret.Name
	}

	return cluster.GetServerCASecretName()
}

// GetClientCASecretNameOrDefault returns the specified client CA secret name
// for PgBouncer if provided or the default name otherwise.
func (in *Pooler) GetClientCASecretNameOrDefault(cluster *Cluster) string {
	if in.Spec.PgBouncer != nil && in.Spec.PgBouncer.ClientCASecret != nil {
		return in.Spec.PgBouncer.ClientCASecret.Name
	}

	return cluster.GetClientCASecretName()
}

// GetClientTLSSecretNameOrDefault returns the specified client TLS secret name
// for PgBouncer if provided or the default name otherwise.
func (in *Pooler) GetClientTLSSecretNameOrDefault(cluster *Cluster) string {
	if in.Spec.PgBouncer != nil && in.Spec.PgBouncer.ClientTLSSecret != nil {
		return in.Spec.PgBouncer.ClientTLSSecret.Name
	}

	return cluster.GetServerTLSSecretName()
}

// GetAuthQuery returns the specified AuthQuery name for PgBouncer
// if provided or the default name otherwise.
func (in *Pooler) GetAuthQuery() string {
	if in.Spec.PgBouncer.AuthQuery != "" {
		return in.Spec.PgBouncer.AuthQuery
	}

	return DefaultPgBouncerPoolerAuthQuery
}

// IsAutomatedIntegration returns whether the Pooler integration with the
// Cluster is automated or not.
func (in *Pooler) IsAutomatedIntegration() bool {
	if in.Spec.PgBouncer == nil {
		return true
	}
	// If the user specified an AuthQuerySecret or an AuthQuery, the integration
	// is not going to be handled by the operator.
	if in.Spec.PgBouncer.AuthQuery != "" ||
		(in.Spec.PgBouncer.AuthQuerySecret != nil && in.Spec.PgBouncer.AuthQuerySecret.Name != "") ||
		(in.Spec.PgBouncer.ServerTLSSecret != nil && in.Spec.PgBouncer.ServerTLSSecret.Name != "") {
		return false
	}
	return true
}

// GetResourcesRequirements returns the resource requirements for the Pooler
func (in *Pooler) GetResourcesRequirements() corev1.ResourceRequirements {
	if in.Spec.Template == nil {
		return corev1.ResourceRequirements{}
	}

	if in.Spec.Template.Spec.Resources == nil {
		return corev1.ResourceRequirements{}
	}

	return *in.Spec.Template.Spec.Resources
}

// GetServiceAccountName returns the name of the ServiceAccount to use for this Pooler.
// If ServiceAccountName is specified in the spec, it returns that value.
// Otherwise, it returns the Pooler name.
func (in *Pooler) GetServiceAccountName() string {
	if in.Spec.ServiceAccountName != nil {
		return *in.Spec.ServiceAccountName
	}
	return in.ObjectMeta.Name
}
