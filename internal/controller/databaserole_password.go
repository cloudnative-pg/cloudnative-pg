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
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	// defaultPasswordLength is the length of a generated password when the role
	// does not constrain it, and matches the default of the external-secrets
	// password generator this stanza is modeled on.
	defaultPasswordLength = 24

	// passwordSecretPgpassKey is the key of the generated password Secret
	// carrying a ready-made `.pgpass` line for the role.
	passwordSecretPgpassKey = "pgpass"

	// maxGeneratedDigits caps how many digits are asked for by default: the
	// generator draws from ten of them, and refuses to repeat a character unless
	// explicitly allowed to.
	maxGeneratedDigits = 10

	// passwordSecretNotOwnedMessage says what a role asking for a generated
	// password gets when the name is taken by a Secret the operator does not
	// own: the password that Secret holds, and no generation, which is what
	// `mode: secret` does. Spelling it out matters because the role keeps
	// working, so nothing else would show that the generation, and the
	// rotation with it, is not happening.
	passwordSecretNotOwnedMessage = "Secret %q already exists and is not owned by this DatabaseRole: " +
		"the password it holds is applied to the role, and no password is generated or rotated for it. " +
		"Point password.secret at a name of its own, or use mode: secret to read from this Secret"
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
		// There is no generated password to rotate: the request is consumed
		// without effect, rather than left pending forever.
		if _, requested := role.Annotations[utils.RotatePasswordAnnotationName]; requested {
			log.FromContext(ctx).Warning(
				"password rotation requested, but this DatabaseRole is not generating a password",
				"role", role.Spec.Name)
			delete(role.Annotations, utils.RotatePasswordAnnotationName)
		}

		return r.stopGeneratingPassword(ctx, role, generatedSecretName)
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

// stopGeneratingPassword cleans up after a role that does not generate its
// password any more. The Secret the operator generated goes, and the record of
// it with it, unless the password it held is still set on the role in
// PostgreSQL with nothing left to read it: that one is recorded as a revocation
// for the instance manager to apply.
func (r *DatabaseRoleReconciler) stopGeneratingPassword(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	generatedSecretName string,
) error {
	if generatedSecretName == "" {
		// The revocation stays recorded until the instance manager
		// acknowledges it, by clearing the flag once it has applied it:
		// forgetting it any earlier would leave the role with a password
		// nobody can read.
		if role.IsPasswordRevocationPending() {
			return nil
		}
		role.Status.Password = nil
		return nil
	}

	// A Secret the operator generated into is a Secret the operator deletes,
	// whatever the role points at now: keeping it alive but unmanaged would
	// leave a credential nobody maintains, still carrying the owner reference
	// that has it garbage collected with the role.
	if err := r.deleteOwnedPasswordSecret(ctx, role, client.ObjectKey{
		Namespace: role.Namespace,
		Name:      generatedSecretName,
	}); err != nil {
		return err
	}

	// A cleared status is how deleteOwnedPasswordSecret reports the Secret
	// really was the operator's to delete, and the password it held is still
	// the one set on the role in PostgreSQL. Only `external` reaches this with
	// that password left to deal with: `setNull` sets it to NULL anyway, and
	// `secret` replaces it with the one it reads.
	if role.Status.Password == nil &&
		role.Spec.Password != nil &&
		role.Spec.Password.Mode == apiv1.PasswordModeExternal {
		role.Status.Password = &apiv1.GeneratedPasswordState{PendingRevocation: true}
	}
	return nil
}

// setPasswordMessage explains in the status why the password of the role is not
// being generated, keeping what the operator already recorded about the
// password it generated last: the name of its Secret, which is the only record
// it has to clean that Secret up later, and when the password was issued and
// expires. Those two are what the role's VALID UNTIL follows, so forgetting
// them while the password cannot be rotated would lift the expiration
// PostgreSQL enforces, turning a stalled rotation into a credential that works
// forever, and would take the deadline out of the status an operator watches
// exactly when it is needed.
func setPasswordMessage(role *apiv1.DatabaseRole, message string) {
	state := apiv1.GeneratedPasswordState{Message: message}
	if role.Status.Password != nil {
		state.SecretName = role.Status.Password.SecretName
		state.IssuedAt = role.Status.Password.IssuedAt
		state.Expiration = role.Status.Password.Expiration
	}
	role.Status.Password = &state
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
	var issuedAt time.Time
	err := r.Get(ctx, secretKey, &secret)
	switch {
	case err == nil:
		// Never touch a same-named Secret the operator does not own: it may
		// belong to the user or to another component. Report the conflict in the
		// status rather than overwriting a password somebody else manages.
		if !metav1.IsControlledBy(&secret, role) {
			contextLogger.Warning("password secret exists but is not owned by this DatabaseRole, skipping generation",
				"secret", secretKey.Name)
			setPasswordMessage(role, fmt.Sprintf(passwordSecretNotOwnedMessage, secretKey.Name))
			return nil
		}

		issuedAt, err = r.ensureOwnedPasswordSecretUpToDate(ctx, role, &secret)
		if err != nil {
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
		issuedAt = time.Now()
		// A brand new password satisfies any pending manual rotation request.
		delete(role.Annotations, utils.RotatePasswordAnnotationName)

	default:
		return fmt.Errorf("while getting password secret %q: %w", secretKey.Name, err)
	}

	var issuedAtString, expiration string
	if !issuedAt.IsZero() {
		issuedAtString = issuedAt.UTC().Format(time.RFC3339)
		// Rotation may still be disabled for this role even though a password
		// was just issued: there is nothing to expire in that case.
		if role.Spec.Password.Duration != nil {
			expiration = issuedAt.Add(role.Spec.Password.Duration.Duration).UTC().Format(time.RFC3339)
		}
	}

	role.Status.Password = &apiv1.GeneratedPasswordState{
		SecretName: secretKey.Name,
		IssuedAt:   issuedAtString,
		Expiration: expiration,
	}
	return nil
}

// ensureOwnedPasswordSecretUpToDate generates a new password when the current
// one is gone or due for rotation, and keeps the username in sync with the
// role. It returns the time the password currently held by the Secret was
// issued, which the caller needs to record in the role's status; it is the
// zero time when rotation is not enabled for the role.
func (r *DatabaseRoleReconciler) ensureOwnedPasswordSecretUpToDate(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	secret *corev1.Secret,
) (time.Time, error) {
	origSecret := secret.DeepCopy()

	var issuedAt time.Time
	switch {
	case passwordNeedsRotation(ctx, role, secret):
		generated, err := generatePassword(role)
		if err != nil {
			return time.Time{}, err
		}
		secret.Data = passwordSecretData(role, generated)
		issuedAt = time.Now()
		// The rotation this reconciliation just performed satisfies any
		// pending manual request: it is a one-shot ask, not a standing one.
		delete(role.Annotations, utils.RotatePasswordAnnotationName)

	case !role.IsPasswordRotationEnabled():
		// Nothing to expire: the status must carry an empty IssuedAt and
		// Expiration, matching what clearing the lifetime annotations used to do.
		issuedAt = time.Time{}

	default:
		// The password was not rotated this loop and rotation is enabled: carry
		// its recorded issue time forward, so the deadline is not recomputed
		// from nothing on every reconciliation.
		var err error
		issuedAt, err = passwordIssuedAt(role, secret.CreationTimestamp.Time)
		if err != nil {
			issuedAt = secret.CreationTimestamp.Time
		}
	}

	// The instance manager refuses to apply a password whose Secret names a role
	// other than the one it belongs to, so the username follows the role name
	// whether or not the password itself is rotated.
	secret.Data[corev1.BasicAuthUsernameKey] = []byte(role.Spec.Name)

	// The pgpass line is derived from what the Secret already holds, so it is
	// rebuilt here rather than only when a password is generated: a Secret that
	// predates this key, or lost it, gets it back without waiting for a
	// rotation it may never be due for.
	secret.Data[passwordSecretPgpassKey] = pgpassLine(
		role.Spec.Name, string(secret.Data[corev1.BasicAuthPasswordKey]))

	// Patch only on a real change: an unconditional patch would bump the
	// resourceVersion of the Secret on every loop, and that is exactly the signal
	// that makes the instance manager re-apply the password.
	if reflect.DeepEqual(origSecret.Data, secret.Data) {
		return issuedAt, nil
	}

	if err := r.Patch(ctx, secret, client.MergeFrom(origSecret)); err != nil {
		return time.Time{}, fmt.Errorf("while patching password secret %q: %w", secret.Name, err)
	}
	return issuedAt, nil
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
// either because the Secret no longer carries one, because the current one
// reached its renewal window, or because rotation was explicitly requested
// through the RotatePasswordAnnotationName annotation. The renewal window is
// always computed fresh from the recorded issue time and the role's current
// duration/renewBefore, rather than trusting a previously recorded deadline,
// so that a change to either takes effect on this reconciliation instead of
// being overridden by a deadline computed under settings that no longer
// apply.
func passwordNeedsRotation(ctx context.Context, role *apiv1.DatabaseRole, secret *corev1.Secret) bool {
	if len(secret.Data[corev1.BasicAuthPasswordKey]) == 0 {
		return true
	}

	// A manual request overrides even a role that never rotates on its own.
	if _, requested := role.Annotations[utils.RotatePasswordAnnotationName]; requested {
		return true
	}

	if !role.IsPasswordRotationEnabled() {
		return false
	}

	issuedAt, err := passwordIssuedAt(role, secret.CreationTimestamp.Time)
	if err != nil {
		rawIssuedAt := ""
		if role.Status.Password != nil {
			rawIssuedAt = role.Status.Password.IssuedAt
		}
		log.FromContext(ctx).Warning("unreadable password issue time, rotating the password",
			"secret", secret.Name, "issuedAt", rawIssuedAt, "err", err.Error())
		return true
	}

	renewalDue := issuedAt.Add(role.Spec.Password.Duration.Duration).Add(-passwordRenewBefore(role))
	return !time.Now().Before(renewalDue)
}

// passwordIssuedAt returns the time recorded in the role's status as when its
// current generated password was issued, parsed from RFC3339. When it is
// absent or empty, indicating rotation was enabled only after the password
// had already been generated, it falls back to the given time, so that the
// lifetime the password already had is counted from the creation of its
// Secret.
func passwordIssuedAt(role *apiv1.DatabaseRole, fallback time.Time) (time.Time, error) {
	if role.Status.Password == nil || role.Status.Password.IssuedAt == "" {
		return fallback, nil
	}
	return time.Parse(time.RFC3339, role.Status.Password.IssuedAt)
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

	return secret, nil
}

// passwordSecretData returns the content of the Secret holding the password of
// a role. The username is the one the instance manager checks the Secret
// against before applying the password.
func passwordSecretData(role *apiv1.DatabaseRole, generated string) map[string][]byte {
	return map[string][]byte{
		corev1.BasicAuthUsernameKey: []byte(role.Spec.Name),
		corev1.BasicAuthPasswordKey: []byte(generated),
		passwordSecretPgpassKey:     pgpassLine(role.Spec.Name, generated),
	}
}

// pgpassFieldEscaper escapes what a `.pgpass` line cannot carry literally: the
// field separator, and the escape character itself. A generated password can
// contain both, since the symbols it may draw from include `:` and `\`.
// Ref: https://www.postgresql.org/docs/current/libpq-pgpass.html
var pgpassFieldEscaper = strings.NewReplacer(`\`, `\\`, `:`, `\:`)

// pgpassLine builds the `.pgpass` line for the password of a role, ready to be
// appended to a `~/.pgpass` file. The host and the database are wildcards: the
// credential belongs to the role, and says nothing about which endpoint of the
// cluster it connects to, or which database it connects to. The trailing
// newline makes the value usable as a file on its own.
func pgpassLine(username, password string) []byte {
	return []byte(fmt.Sprintf("*:%d:*:%s:%s\n",
		postgres.ServerPort,
		pgpassFieldEscaper.Replace(username),
		pgpassFieldEscaper.Replace(password),
	))
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
