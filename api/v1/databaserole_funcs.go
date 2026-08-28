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

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
)

// SetAsFailed sets the role as failed with the given error
func (r *DatabaseRole) SetAsFailed(err error) {
	r.Status.Applied = ptr.To(false)
	r.Status.Message = err.Error()
}

// SetAsReady sets the role as working correctly
func (r *DatabaseRole) SetAsReady() {
	r.Status.Message = ""
	r.Status.Applied = ptr.To(true)
	r.Status.ObservedGeneration = r.Generation
}

// SetAsUnknown sets the role's applied state as unknown with the given error
func (r *DatabaseRole) SetAsUnknown(err error) {
	r.Status.Applied = nil
	r.Status.Message = err.Error()
}

// HasReconciliations returns true if the role has been reconciled at least once
func (r *DatabaseRole) HasReconciliations() bool {
	return r.Status.ObservedGeneration > 0
}

// MustHaveManagedResourceExclusivity detects conflicting roles
func (roleList *DatabaseRoleList) MustHaveManagedResourceExclusivity(role *DatabaseRole) error {
	pointers := toSliceWithPointers(roleList.Items)
	return ensureManagedResourceExclusivity(role, pointers)
}

// GetClusterRef returns the cluster reference of the role
func (r *DatabaseRole) GetClusterRef() corev1.LocalObjectReference {
	return r.Spec.ClusterRef
}

// GetManagedObjectName returns the name of the managed role object
func (r *DatabaseRole) GetManagedObjectName() string {
	return r.Spec.Name
}

// GetStatusMessage returns the status message of the role
func (r *DatabaseRole) GetStatusMessage() string {
	return r.Status.Message
}

// SetStatusObservedGeneration sets the observed generation of the role
func (r *DatabaseRole) SetStatusObservedGeneration(obsGeneration int64) {
	r.Status.ObservedGeneration = obsGeneration
}

// GetClientCertSecretName returns the name of the Secret that holds the
// generated TLS client certificate for this role.
func (r *DatabaseRole) GetClientCertSecretName() string {
	return r.Name + clientCertSecretSuffix
}

// IsClientCertificateEnabled returns true if the operator should manage a TLS
// client certificate for this role. The clientCertificate block defaults
// enabled to true when present, so an unset enabled field means enabled.
func (r *DatabaseRole) IsClientCertificateEnabled() bool {
	if r.Spec.ClientCertificate == nil {
		return false
	}
	return ptr.Deref(r.Spec.ClientCertificate.Enabled, true)
}

// IsPasswordGenerationEnabled returns true if the operator should generate the
// password of this role.
func (r *DatabaseRole) IsPasswordGenerationEnabled() bool {
	if r.Spec.Password == nil {
		return false
	}
	// Asked for positively, so a mode added later must opt into generation
	// rather than fall into it by not being listed here.
	return r.Spec.Password.Mode == PasswordModeGenerate
}

// IsPasswordSetToNull returns true if the password block asks the operator to
// set the password of this role to NULL in PostgreSQL, rather than to generate
// one or leave it untouched.
func (r *DatabaseRole) IsPasswordSetToNull() bool {
	return r.Spec.Password != nil && r.Spec.Password.Mode == PasswordModeSetNull
}

// IsPasswordRevocationPending returns true when the operator generated the
// password this role still has in PostgreSQL, deleted the Secret holding it,
// and the role asks for the password not to be managed any more: nothing can
// read that password now, so it has to be set to NULL rather than left behind.
// Both the specification and the status must say so: a role that moved on to
// another mode has a password to apply, not one to revoke.
func (r *DatabaseRole) IsPasswordRevocationPending() bool {
	return r.Spec.Password != nil && r.Spec.Password.Mode == PasswordModeExternal &&
		r.Status.Password != nil && r.Status.Password.PendingRevocation
}

// GetGeneratedPasswordSecretName returns the name of the Secret where the
// operator stores the generated password of this role. The Secret it generated
// into before, which may carry another name, is recorded in
// `status.password.secretName` instead.
func (r *DatabaseRole) GetGeneratedPasswordSecretName() string {
	if r.Spec.Password != nil && r.Spec.Password.Secret != "" {
		return r.Spec.Password.Secret
	}
	return r.Name + passwordSecretSuffix
}

// GetPasswordSecretName returns the name of the Secret holding the password of
// this role: the one generated by the operator, the one named by `password`
// when its mode is `secret`, or the one supplied through `passwordSecret`. It
// is empty when the role has no password Secret.
func (r *DatabaseRole) GetPasswordSecretName() string {
	if r.IsPasswordGenerationEnabled() {
		return r.GetGeneratedPasswordSecretName()
	}
	if r.Spec.Password != nil && r.Spec.Password.Mode == PasswordModeSecret {
		return r.Spec.Password.Secret
	}
	return r.Spec.GetRoleSecretName()
}

// GetPasswordMessage returns the explanation the operator recorded about the
// password of the role, if it recorded one.
func (r *DatabaseRole) GetPasswordMessage() string {
	if r.Status.Password == nil {
		return ""
	}
	return r.Status.Password.Message
}

// SetPasswordMessage explains in the status why the password is not being
// generated, without dropping the Secret name and issue/expiration times
// already recorded: losing those while rotation is stalled would lift the
// VALID UNTIL PostgreSQL enforces.
func (r *DatabaseRole) SetPasswordMessage(message string) {
	var state GeneratedPasswordState
	if r.Status.Password != nil {
		state = *r.Status.Password
	}

	// The revocation is the one thing not kept: it belongs to a role that
	// stopped generating its password, and this one is still asking for it.
	state.PendingRevocation = false
	state.Message = message
	r.Status.Password = &state
}

// SetGeneratedPasswordState records what the operator knows about the
// generated password, field by field rather than by whole-struct assignment,
// so the applied-expiration field the instance manager writes into the same
// struct isn't dropped. A nil state means the role generates no password.
func (r *DatabaseRole) SetGeneratedPasswordState(state *GeneratedPasswordState) {
	if state == nil {
		r.Status.Password = nil
		return
	}

	if r.Status.Password == nil {
		r.Status.Password = &GeneratedPasswordState{}
	}
	r.Status.Password.SecretName = state.SecretName
	r.Status.Password.IssuedAt = state.IssuedAt
	r.Status.Password.Expiration = state.Expiration
	r.Status.Password.Message = state.Message
	r.Status.Password.PendingRevocation = state.PendingRevocation
}

// IsPasswordRotationEnabled returns true when the generated password has a
// lifetime, and is therefore rotated by the operator.
func (r *DatabaseRole) IsPasswordRotationEnabled() bool {
	return r.IsPasswordGenerationEnabled() && r.Spec.Password.Duration != nil
}

// GetPasswordRenewalDue returns when the current password is due for renewal:
// issue time plus duration, minus renewBefore. It always recomputes from the
// current spec rather than a stored deadline, so a config change takes effect
// immediately. Meaningful only when password rotation is enabled.
// fallbackIssuedAt stands in for the issue time when rotation was enabled
// after the password was already generated.
func (r *DatabaseRole) GetPasswordRenewalDue(fallbackIssuedAt time.Time) (time.Time, error) {
	issuedAt, err := r.GetPasswordIssuedAt(fallbackIssuedAt)
	if err != nil {
		return time.Time{}, err
	}

	return issuedAt.Add(r.Spec.Password.Duration.Duration).Add(-r.GetPasswordRenewBefore()), nil
}

// GetPasswordIssuedAt returns the recorded issue time of the current password,
// parsed from RFC3339, falling back to the given time when it's absent (which
// happens when rotation was enabled after the password was already generated).
func (r *DatabaseRole) GetPasswordIssuedAt(fallback time.Time) (time.Time, error) {
	if r.Status.Password == nil || r.Status.Password.IssuedAt == "" {
		return fallback, nil
	}
	return time.Parse(time.RFC3339, r.Status.Password.IssuedAt)
}

// GetPasswordRenewBefore returns how long before its expiration the password
// of the role is rotated. Meaningful only when password rotation is enabled.
func (r *DatabaseRole) GetPasswordRenewBefore() time.Duration {
	duration := r.Spec.Password.Duration.Duration

	if r.Spec.Password.RenewBefore != nil {
		return r.Spec.Password.RenewBefore.Duration
	}

	// A non-positive threshold would rotate the password only once it is already
	// expired, so fall back to the built-in default as the certificates do.
	threshold := configuration.Current.ExpiringCheckThreshold
	if threshold <= 0 {
		threshold = configuration.ExpiringCheckThreshold
	}

	// The operator-wide default can be longer than a short lifetime, which would
	// leave the password due for rotation the moment it is generated: cap it at
	// half of the requested lifetime.
	renewBefore := time.Duration(threshold) * 24 * time.Hour
	if renewBefore > duration/2 {
		renewBefore = duration / 2
	}
	return renewBefore
}
