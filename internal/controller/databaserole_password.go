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

package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/sethvargo/go-password/password"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	// defaultPasswordLength is the length of a generated password when the role
	// does not constrain it, and matches the default of the external-secrets
	// password generator this stanza is modeled on.
	defaultPasswordLength = 24

	// maxGeneratedDigits caps how many digits are asked for by default: the
	// generator draws from ten of them, and refuses to repeat a character unless
	// explicitly allowed to.
	maxGeneratedDigits = 10
)

// errInvalidPasswordCriteria is returned when no password can satisfy the
// criteria of the role: retrying cannot help until the specification changes.
var errInvalidPasswordCriteria = errors.New("cannot generate a password matching the requested criteria")

// reconcilePassword is the top-level entry point for the lifecycle of a
// generated password. It either generates and rotates it, or deletes the Secret
// holding it, depending on whether password generation is enabled.
func (r *DatabaseRoleReconciler) reconcilePassword(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	cluster *apiv1.Cluster,
) error {
	// The status is the only record of where the password was generated: the name
	// may have changed, or have disappeared from the specification altogether.
	generatedSecretName := ""
	if role.Status.Password != nil {
		generatedSecretName = role.Status.Password.SecretName
	}

	// When generation is disabled we only need to clean up the Secret we
	// generated; the cluster is not required for that.
	if !role.IsPasswordGenerationEnabled() {
		if generatedSecretName == "" {
			role.Status.Password = nil
			return nil
		}
		return r.deleteOwnedPasswordSecret(ctx, role, client.ObjectKey{
			Namespace: role.Namespace,
			Name:      generatedSecretName,
		})
	}

	secretKey := client.ObjectKey{
		Namespace: role.Namespace,
		Name:      role.GetGeneratedPasswordSecretName(),
	}

	// The client certificate of the role lives in a Secret this same controller
	// owns: writing the password into it would have the two reconcilers
	// overwrite each other's data on every loop.
	if secretKey.Name == role.GetClientCertSecretName() {
		setPasswordMessage(role, fmt.Sprintf(
			"Secret %q is reserved for the client certificate of this DatabaseRole and cannot hold its password",
			secretKey.Name))
		return nil
	}

	if cluster == nil {
		return nil
	}

	// On a replica cluster the role, and therefore its password, is owned by the
	// primary cluster and replicated from it: a password generated here would
	// never be applied, and publishing it would hand out a credential that does
	// not work. An already generated Secret is left in place, since it still
	// holds the password the role had when this cluster was a primary.
	if cluster.IsReplica() {
		setPasswordMessage(role, fmt.Sprintf(
			"cluster %q is a replica cluster: the password of this role is owned by the primary cluster, "+
				"generation will start once this cluster is promoted",
			cluster.Name))
		return nil
	}

	// The password is generated somewhere else now: the Secret it used to be
	// generated into keeps a credential the operator no longer maintains, and
	// nothing else is going to remove it while the role lives on. This waits
	// until a password can actually be generated again, so that a cluster that
	// is missing, or not a primary, does not cost the role the only Secret it
	// has.
	if generatedSecretName != "" && generatedSecretName != secretKey.Name {
		if err := r.deleteOwnedPasswordSecret(ctx, role, client.ObjectKey{
			Namespace: role.Namespace,
			Name:      generatedSecretName,
		}); err != nil {
			return err
		}
	}

	if err := r.ensurePasswordSecret(ctx, role, secretKey); err != nil {
		if !errors.Is(err, errInvalidPasswordCriteria) {
			return err
		}
		// The criteria cannot be satisfied: say so and wait for the role to be
		// fixed, instead of retrying a generation that will never succeed.
		log.FromContext(ctx).Warning("cannot generate the password of this DatabaseRole",
			"role", role.Spec.Name, "err", err.Error())
		setPasswordMessage(role, err.Error())
	}

	return nil
}

// setPasswordMessage explains in the status why the password of the role is not
// being generated, keeping the name of the Secret it was last generated into:
// that name is the only record the operator has to clean the Secret up later.
func setPasswordMessage(role *apiv1.DatabaseRole, message string) {
	generatedSecretName := ""
	if role.Status.Password != nil {
		generatedSecretName = role.Status.Password.SecretName
	}
	role.Status.Password = &apiv1.GeneratedPasswordState{
		SecretName: generatedSecretName,
		Message:    message,
	}
}

// ensurePasswordSecret makes sure the Secret holding the generated password
// exists and is up to date. It modifies role.Status.Password in memory; the
// caller is responsible for persisting the status.
func (r *DatabaseRoleReconciler) ensurePasswordSecret(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	secretKey client.ObjectKey,
) error {
	contextLogger := log.FromContext(ctx)

	var secret corev1.Secret
	err := r.Get(ctx, secretKey, &secret)
	switch {
	case err == nil:
		// Never touch a same-named Secret the operator does not own: it may
		// belong to the user or to another component. Report the conflict in the
		// status rather than overwriting a password somebody else manages.
		if !metav1.IsControlledBy(&secret, role) {
			contextLogger.Warning("password secret exists but is not owned by this DatabaseRole, skipping generation",
				"secret", secretKey.Name)
			setPasswordMessage(role, fmt.Sprintf(secretNotOwnedMessage, secretKey.Name))
			return nil
		}

		if err := r.ensureOwnedPasswordSecretUpToDate(ctx, role, &secret); err != nil {
			return err
		}

	case apierrs.IsNotFound(err):
		newSecret, err := generatePasswordSecret(role, secretKey)
		if err != nil {
			return err
		}
		if err := ctrl.SetControllerReference(role, newSecret, r.Scheme); err != nil {
			return fmt.Errorf("while setting owner reference on password secret %q: %w", secretKey.Name, err)
		}
		if err := r.Create(ctx, newSecret); err != nil {
			return fmt.Errorf("while creating password secret %q: %w", secretKey.Name, err)
		}

		secret = *newSecret

	default:
		return fmt.Errorf("while getting password secret %q: %w", secretKey.Name, err)
	}

	role.Status.Password = &apiv1.GeneratedPasswordState{
		SecretName: secretKey.Name,
		Expiration: secret.Annotations[utils.PasswordExpirationAnnotationName],
	}
	return nil
}

// ensureOwnedPasswordSecretUpToDate generates a new password when the current
// one is gone or due for rotation, and keeps the expiration annotation and the
// username in sync with the role.
func (r *DatabaseRoleReconciler) ensureOwnedPasswordSecretUpToDate(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	secret *corev1.Secret,
) error {
	origSecret := secret.DeepCopy()

	switch {
	case passwordNeedsRotation(ctx, role, secret):
		generated, err := generatePassword(role)
		if err != nil {
			return err
		}
		secret.Data = passwordSecretData(role, generated)
		setPasswordExpiration(role, secret, time.Now())

	case !role.IsPasswordRotationEnabled():
		// Drop a deadline left over from a lifetime that is no longer requested,
		// so that the status stops advertising one the operator does not honor.
		delete(secret.Annotations, utils.PasswordExpirationAnnotationName)

	case secret.Annotations[utils.PasswordExpirationAnnotationName] == "":
		// Record the deadline of a password that was generated before rotation
		// was enabled, counting the lifetime it already had.
		setPasswordExpiration(role, secret, secret.CreationTimestamp.Time)
	}

	// The instance manager refuses to apply a password whose Secret names a role
	// other than the one it belongs to, so the username follows the role name
	// whether or not the password itself is rotated.
	secret.Data[corev1.BasicAuthUsernameKey] = []byte(role.Spec.Name)

	// Patch only on a real change: an unconditional patch would bump the
	// resourceVersion of the Secret on every loop, and that is exactly the signal
	// that makes the instance manager re-apply the password.
	if reflect.DeepEqual(origSecret.Data, secret.Data) &&
		reflect.DeepEqual(origSecret.Annotations, secret.Annotations) {
		return nil
	}

	if err := r.Patch(ctx, secret, client.MergeFrom(origSecret)); err != nil {
		return fmt.Errorf("while patching password secret %q: %w", secret.Name, err)
	}
	return nil
}

// deleteOwnedPasswordSecret deletes the password Secret if it exists and is
// owned by the given role. Unowned Secrets with the same name are left
// untouched.
func (r *DatabaseRoleReconciler) deleteOwnedPasswordSecret(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	secretKey client.ObjectKey,
) error {
	deleted, err := r.deleteOwnedSecret(ctx, role, secretKey)
	if err != nil {
		return err
	}
	if !deleted {
		setPasswordMessage(role, fmt.Sprintf(secretNotDeletableMessage, secretKey.Name))
		return nil
	}

	role.Status.Password = nil
	return nil
}

// passwordNeedsRotation reports whether a new password must be generated,
// either because the Secret no longer carries one, or because the current one
// reached its renewal window.
func passwordNeedsRotation(ctx context.Context, role *apiv1.DatabaseRole, secret *corev1.Secret) bool {
	if len(secret.Data[corev1.BasicAuthPasswordKey]) == 0 {
		return true
	}

	if !role.IsPasswordRotationEnabled() {
		return false
	}

	// A password generated before rotation was enabled has no deadline recorded:
	// account for the lifetime it already had, so that one older than the
	// requested duration is rotated right away.
	expiration := secret.CreationTimestamp.Add(role.Spec.Password.Duration.Duration)
	if value, ok := secret.Annotations[utils.PasswordExpirationAnnotationName]; ok {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			log.FromContext(ctx).Warning("unreadable password expiration, rotating the password",
				"secret", secret.Name, "expiration", value, "err", err.Error())
			return true
		}
		expiration = parsed
	}

	return !time.Now().Before(expiration.Add(-passwordRenewBefore(role)))
}

// passwordRenewBefore returns how long before its expiration the password of
// the role is rotated.
func passwordRenewBefore(role *apiv1.DatabaseRole) time.Duration {
	duration := role.Spec.Password.Duration.Duration

	if role.Spec.Password.RenewBefore != nil {
		return role.Spec.Password.RenewBefore.Duration
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

// setPasswordExpiration records when the password generated at the given time
// expires, and removes the record when the role does not rotate its password.
func setPasswordExpiration(role *apiv1.DatabaseRole, secret *corev1.Secret, issuedAt time.Time) {
	if !role.IsPasswordRotationEnabled() {
		delete(secret.Annotations, utils.PasswordExpirationAnnotationName)
		return
	}

	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	expiration := issuedAt.Add(role.Spec.Password.Duration.Duration)
	secret.Annotations[utils.PasswordExpirationAnnotationName] = expiration.UTC().Format(time.RFC3339)
}

// generatePasswordSecret builds the basic-auth Secret holding a freshly
// generated password for the role.
func generatePasswordSecret(role *apiv1.DatabaseRole, secretKey client.ObjectKey) (*corev1.Secret, error) {
	generated, err := generatePassword(role)
	if err != nil {
		return nil, err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretKey.Name,
			Namespace: secretKey.Namespace,
			Labels: map[string]string{
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			},
		},
		Type: corev1.SecretTypeBasicAuth,
		Data: passwordSecretData(role, generated),
	}
	setPasswordExpiration(role, secret, time.Now())

	return secret, nil
}

// passwordSecretData returns the content of the Secret holding the password of
// a role. The username is the one the instance manager checks the Secret
// against before applying the password.
func passwordSecretData(role *apiv1.DatabaseRole, generated string) map[string][]byte {
	return map[string][]byte{
		corev1.BasicAuthUsernameKey: []byte(role.Spec.Name),
		corev1.BasicAuthPasswordKey: []byte(generated),
	}
}

// generatePassword generates a password matching the criteria of the role.
func generatePassword(role *apiv1.DatabaseRole) (string, error) {
	var criteria apiv1.PasswordCriteria
	if role.Spec.Password.Criteria != nil {
		criteria = *role.Spec.Password.Criteria
	}

	length := defaultPasswordLength
	if criteria.Length > 0 {
		length = criteria.Length
	}

	// The generator measures the symbols it can draw from by the length of the
	// set, and spins forever looking for an unused one when a symbol is listed
	// more than once: hand it a set with no repetitions, so that asking for more
	// symbols than there are is reported as an unsatisfiable criteria instead.
	// An empty set makes the generator fall back to its own.
	generator, err := password.NewGenerator(&password.GeneratorInput{
		Symbols: dedupeCharacters(ptr.Deref(criteria.SymbolCharacters, "")),
	})
	if err != nil {
		return "", fmt.Errorf("%w for role %q: %w", errInvalidPasswordCriteria, role.Spec.Name, err)
	}

	// Following the external-secrets generator, a quarter of the password is
	// made of digits unless stated otherwise. Symbols instead default to none,
	// as they do for the passwords the operator generates for the superuser and
	// the application user.
	generated, err := generator.Generate(
		length,
		ptr.Deref(criteria.Digits, min(length/4, maxGeneratedDigits)),
		ptr.Deref(criteria.Symbols, 0),
		criteria.NoUpper,
		criteria.AllowRepeat,
	)
	if err != nil {
		return "", fmt.Errorf("%w for role %q: %w", errInvalidPasswordCriteria, role.Spec.Name, err)
	}
	return generated, nil
}

// dedupeCharacters returns the given set of characters with every repetition
// removed, preserving the order of the first occurrences.
func dedupeCharacters(set string) string {
	var unique string
	for _, character := range set {
		if !strings.ContainsRune(unique, character) {
			unique += string(character)
		}
	}
	return unique
}
