/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

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
	if (in.Spec.PgBouncer.AuthQuerySecret != nil && in.Spec.PgBouncer.AuthQuerySecret.Name != "") ||
		in.Spec.PgBouncer.AuthQuery != "" {
		return false
	}
	return true
}
