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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// eventBufferSize sizes the channel of the fake recorder these tests give the
// reconciler: record.FakeRecorder blocks on a full channel, so it has to hold
// more events than any single spec emits.
const eventBufferSize = 100

// recordedEvents returns the events the reconciler emitted so far, draining
// them so that a spec can tell a new event from one it has already seen.
func recordedEvents(r *DatabaseRoleReconciler) []string {
	recorder := r.Recorder.(*record.FakeRecorder)
	var events []string
	for {
		select {
		case event := <-recorder.Events:
			events = append(events, event)
		default:
			return events
		}
	}
}

var _ = Describe("DatabaseRole password generation", func() {
	ctx := context.Background()

	const (
		namespace   = "default"
		clusterName = "cluster-example"
		roleName    = "dante"
	)

	newCluster := func() *apiv1.Cluster {
		return &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace}}
	}

	newRoleWithPassword := func(config *apiv1.PasswordConfiguration) *apiv1.DatabaseRole {
		return &apiv1.DatabaseRole{
			ObjectMeta: metav1.ObjectMeta{Name: "role-dante", Namespace: namespace},
			Spec: apiv1.DatabaseRoleSpec{
				RoleConfiguration: apiv1.RoleConfiguration{Name: roleName, Login: true},
				ClusterRef:        corev1.LocalObjectReference{Name: clusterName},
				Password:          config,
			},
		}
	}

	buildReconciler := func(objs ...client.Object) (*DatabaseRoleReconciler, client.Client) {
		scheme := schemeBuilder.BuildWithAllKnownScheme()
		cli := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&apiv1.DatabaseRole{}).
			WithObjects(objs...).
			Build()
		return &DatabaseRoleReconciler{Client: cli, Scheme: scheme, Recorder: record.NewFakeRecorder(eventBufferSize)}, cli
	}

	requestFor := func(role *apiv1.DatabaseRole) ctrl.Request {
		return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: role.Namespace, Name: role.Name}}
	}

	getSecret := func(cli client.Client, name string) *corev1.Secret {
		var secret corev1.Secret
		Expect(cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &secret)).To(Succeed())
		return &secret
	}

	expectNoSecret := func(cli client.Client, name string) {
		err := cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &corev1.Secret{})
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
	}

	getRole := func(cli client.Client, role *apiv1.DatabaseRole) *apiv1.DatabaseRole {
		var got apiv1.DatabaseRole
		Expect(cli.Get(ctx, client.ObjectKeyFromObject(role), &got)).To(Succeed())
		return &got
	}

	// recordSecretChange stands in for an earlier loop of the operator, which
	// recorded the current version of the password Secret in the role.
	recordSecretChange := func(cli client.Client, role *apiv1.DatabaseRole) {
		stored := getRole(cli, role)
		meta.SetStatusCondition(&stored.Status.Conditions, metav1.Condition{
			Type:    string(apiv1.ConditionPasswordSecretChange),
			Status:  metav1.ConditionTrue,
			Reason:  "ChangeDetected",
			Message: getSecret(cli, stored.GetGeneratedPasswordSecretName()).ResourceVersion,
		})
		Expect(cli.Status().Update(ctx, stored)).To(Succeed())
	}

	It("does not rotate the password when no lifetime is requested", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
		r, cli := buildReconciler(role, newCluster())

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())
		first := getSecret(cli, "role-dante-password")

		_, err = r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())
		second := getSecret(cli, "role-dante-password")

		Expect(second.Data).To(Equal(first.Data))
		Expect(second.ResourceVersion).To(Equal(first.ResourceVersion))
	})

	It("honors the requested name and criteria", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{
			Mode:   apiv1.PasswordModeGenerate,
			Secret: "dante-credentials",
			Criteria: &apiv1.PasswordCriteria{
				Length:           20,
				Digits:           ptr.To(4),
				Symbols:          ptr.To(3),
				SymbolCharacters: ptr.To("#$%"),
				NoUpper:          true,
			},
		})
		r, cli := buildReconciler(role, newCluster())

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		generated := string(getSecret(cli, "dante-credentials").Data[corev1.BasicAuthPasswordKey])
		Expect(generated).To(HaveLen(20))
		Expect(generated).To(MatchRegexp(`^[^#$%]*([#$%][^#$%]*){3}$`))
		Expect(generated).To(MatchRegexp(`^[^0-9]*([0-9][^0-9]*){4}$`))
		Expect(generated).To(Equal(strings.ToLower(generated)))
	})

	It("keeps the previous secret until it can generate into the new one", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
		cluster := newCluster()
		r, cli := buildReconciler(role, cluster)

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())
		generated := string(getSecret(cli, "role-dante-password").Data[corev1.BasicAuthPasswordKey])

		// The cluster is gone for the moment, so no password can be generated:
		// deleting the Secret that holds the current one would leave the role with
		// no credential at all.
		Expect(cli.Delete(ctx, cluster)).To(Succeed())
		stored := getRole(cli, role)
		stored.Spec.Password.Secret = "dante-credentials"
		Expect(cli.Update(ctx, stored)).To(Succeed())

		_, err = r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(getSecret(cli, "role-dante-password").Data[corev1.BasicAuthPasswordKey])).
			To(Equal(generated))

		Expect(cli.Create(ctx, newCluster())).To(Succeed())
		_, err = r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		expectNoSecret(cli, "role-dante-password")
		Expect(getSecret(cli, "dante-credentials").Data[corev1.BasicAuthPasswordKey]).NotTo(BeEmpty())
	})

	It("reports a symbol listed twice, instead of looking for the second one forever", func(_ SpecContext) {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{
			Mode: apiv1.PasswordModeGenerate,
			Criteria: &apiv1.PasswordCriteria{
				// The generator measures the symbols it can draw from by the length
				// of the set, so a repeated one makes it ask for two distinct symbols
				// out of a set that only has one.
				Length:           20,
				Symbols:          ptr.To(2),
				SymbolCharacters: ptr.To("##"),
			},
		})

		_, err := generatePassword(role)
		Expect(err).To(MatchError(errInvalidPasswordCriteria))
	}, SpecTimeout(30*time.Second))

	It("reports criteria no password can satisfy, instead of retrying forever", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{
			Mode: apiv1.PasswordModeGenerate,
			Criteria: &apiv1.PasswordCriteria{
				// The generator draws symbols from a single character and is not
				// allowed to repeat it.
				Length:           20,
				Symbols:          ptr.To(3),
				SymbolCharacters: ptr.To("#"),
			},
		})
		r, cli := buildReconciler(role, newCluster())

		result, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		expectNoSecret(cli, "role-dante-password")
		Expect(getRole(cli, role).Status.Password.Message).To(
			ContainSubstring("cannot generate a password matching the requested criteria"))
	})

	When("a lifetime is requested", func() {
		It("rotates immediately once a shortened duration is already exceeded, "+
			"instead of honoring the deadline computed under the previous one", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{
				Mode:     apiv1.PasswordModeGenerate,
				Duration: &metav1.Duration{Duration: 90 * 24 * time.Hour},
			})
			r, cli := buildReconciler(role, newCluster())

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			first := getSecret(cli, "role-dante-password")

			// Back-date the issue time to two days ago, well within the original
			// 90-day duration, then shorten the duration below to make it overdue.
			stored := getRole(cli, role)
			issuedAt := time.Now().Add(-48 * time.Hour)
			stored.Status.Password.IssuedAt = issuedAt.UTC().Format(time.RFC3339)
			Expect(cli.Status().Update(ctx, stored)).To(Succeed())

			// Shorten the duration to one hour: the two-day-old password is now
			// well past its shortened deadline.
			stored = getRole(cli, role)
			stored.Spec.Password.Duration = &metav1.Duration{Duration: time.Hour}
			Expect(cli.Update(ctx, stored)).To(Succeed())

			_, err = r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			rotated := getSecret(cli, "role-dante-password")
			Expect(rotated.Data[corev1.BasicAuthPasswordKey]).NotTo(
				Equal(first.Data[corev1.BasicAuthPasswordKey]))
		})

		It("rotates a password older than the requested lifetime", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{
				Mode:     apiv1.PasswordModeGenerate,
				Duration: &metav1.Duration{Duration: 90 * 24 * time.Hour},
			})
			// A password generated before a lifetime was requested carries no
			// expiration: its age is counted from the creation of its Secret.
			stale := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "role-dante-password",
					Namespace:         namespace,
					CreationTimestamp: metav1.NewTime(time.Now().Add(-200 * 24 * time.Hour)),
					OwnerReferences: []metav1.OwnerReference{
						*metav1.NewControllerRef(role, apiv1.SchemeGroupVersion.WithKind("DatabaseRole")),
					},
				},
				Type: corev1.SecretTypeBasicAuth,
				Data: map[string][]byte{
					corev1.BasicAuthUsernameKey: []byte(roleName),
					corev1.BasicAuthPasswordKey: []byte("ancient-password"),
				},
			}
			r, cli := buildReconciler(role, newCluster(), stale)
			recordSecretChange(cli, role)

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			rotated := getSecret(cli, "role-dante-password")
			Expect(string(rotated.Data[corev1.BasicAuthPasswordKey])).NotTo(Equal("ancient-password"))
			Expect(getRole(cli, role).Status.Password.Expiration).NotTo(BeEmpty())
		})

		It("rotates the password when the recorded issue time cannot be read", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{
				Mode:     apiv1.PasswordModeGenerate,
				Duration: &metav1.Duration{Duration: 90 * 24 * time.Hour},
			})
			r, cli := buildReconciler(role, newCluster())

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			first := getSecret(cli, "role-dante-password")

			stored := getRole(cli, role)
			stored.Status.Password.IssuedAt = "not-a-timestamp"
			Expect(cli.Status().Update(ctx, stored)).To(Succeed())

			_, err = r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			rotated := getSecret(cli, "role-dante-password")
			Expect(rotated.Data[corev1.BasicAuthPasswordKey]).NotTo(
				Equal(first.Data[corev1.BasicAuthPasswordKey]))
			_, err = time.Parse(time.RFC3339, getRole(cli, role).Status.Password.IssuedAt)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	It("refuses the secret reserved for the client certificate of the role", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{
			Mode:   apiv1.PasswordModeGenerate,
			Secret: "role-dante-client-cert",
		})
		r, cli := buildReconciler(role, newCluster())

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		// Both reconcilers own that Secret, so writing the password into it would
		// have them overwrite each other's data on every loop.
		expectNoSecret(cli, "role-dante-client-cert")
		Expect(getRole(cli, role).Status.Password.Message).To(ContainSubstring("client certificate"))
	})

	It("keeps the applied expiration while it explains why generation is blocked", func() {
		// SetPasswordMessage rebuilds the password state to add its message,
		// and runs on every loop for as long as the cause lasts.
		existing := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "role-dante-password", Namespace: namespace},
			Type:       corev1.SecretTypeBasicAuth,
			Data:       map[string][]byte{corev1.BasicAuthPasswordKey: []byte("user-managed")},
		}
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{
			Mode:     apiv1.PasswordModeGenerate,
			Duration: &metav1.Duration{Duration: 1008 * time.Hour},
		})
		role.Status.Password = &apiv1.GeneratedPasswordState{
			SecretName:        "role-dante-password",
			Expiration:        "2026-11-16T09:12:44Z",
			AppliedExpiration: "2026-11-16T09:12:44Z",
		}
		r, cli := buildReconciler(role, newCluster(), existing)

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		Expect(getSecret(cli, "role-dante-password").Data).To(Equal(existing.Data))
		stored := getRole(cli, role)
		Expect(stored.Status.Password.Message).To(ContainSubstring("not owned"))
		Expect(stored.Status.Password.AppliedExpiration).To(Equal("2026-11-16T09:12:44Z"))
	})

	It("keeps rotating ahead of the deadline when the expiry check threshold is disabled", func() {
		configuration.Current = configuration.NewConfiguration()
		configuration.Current.ExpiringCheckThreshold = 0
		DeferCleanup(func() { configuration.Current = configuration.NewConfiguration() })

		role := newRoleWithPassword(&apiv1.PasswordConfiguration{
			Mode:     apiv1.PasswordModeGenerate,
			Duration: &metav1.Duration{Duration: 30 * 24 * time.Hour},
		})
		Expect(role.GetPasswordRenewBefore()).To(Equal(7 * 24 * time.Hour))
	})

	It("records the password it generated even when the client certificate fails", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
		role.Spec.ClientCertificate = &apiv1.ClientCertificateConfiguration{}
		cluster := newCluster()

		scheme := schemeBuilder.BuildWithAllKnownScheme()
		cli := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&apiv1.DatabaseRole{}).
			WithObjects(role, cluster).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(
					ctx context.Context, cl client.WithWatch, key client.ObjectKey,
					obj client.Object, opts ...client.GetOption,
				) error {
					if key.Name == cluster.GetClientCASecretName() {
						return errors.New("the CA secret cannot be read")
					}
					return cl.Get(ctx, key, obj, opts...)
				},
			}).
			Build()
		r := &DatabaseRoleReconciler{Client: cli, Scheme: scheme, Recorder: record.NewFakeRecorder(eventBufferSize)}

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).To(HaveOccurred())

		// The condition is how the instance manager learns of a new password:
		// losing it would leave the password in its Secret and never applied.
		stored := getRole(cli, role)
		Expect(meta.FindStatusCondition(
			stored.Status.Conditions,
			string(apiv1.ConditionPasswordSecretChange),
		)).NotTo(BeNil())
		Expect(stored.Status.Password).NotTo(BeNil())
	})

	When("generation is turned off", func() {
		It("deletes the secret it generated when the password is set to NULL", func() {
			// Same Secret lifecycle as turning generation off; `external` vs
			// `setNull` only matters to the instance manager applying it.
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
			r, cli := buildReconciler(role, newCluster())

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			Expect(getSecret(cli, "role-dante-password")).NotTo(BeNil())

			stored := getRole(cli, role)
			stored.Spec.Password.Mode = apiv1.PasswordModeSetNull
			Expect(cli.Update(ctx, stored)).To(Succeed())

			_, err = r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			expectNoSecret(cli, "role-dante-password")
			Expect(getRole(cli, role).Status.Password).To(BeNil())
		})

		It("keeps track of its secret across a state it cannot generate in", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{
				Mode:   apiv1.PasswordModeGenerate,
				Secret: "dante-credentials",
			})
			cluster := newCluster()
			r, cli := buildReconciler(role, cluster)

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			// A demotion stops generation and explains itself in the status, which
			// must not cost the operator the name of the Secret it generated.
			cluster.Spec.ReplicaCluster = &apiv1.ReplicaClusterConfiguration{Source: "origin", Enabled: ptr.To(true)}
			Expect(cli.Update(ctx, cluster)).To(Succeed())

			_, err = r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			status := getRole(cli, role).Status.Password
			Expect(status.Message).To(ContainSubstring("replica cluster"))
			Expect(status.SecretName).To(Equal("dante-credentials"))

			stored := getRole(cli, role)
			stored.Spec.Password.Mode = apiv1.PasswordModeExternal
			Expect(cli.Update(ctx, stored)).To(Succeed())

			_, err = r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			expectNoSecret(cli, "dante-credentials")
		})

		It("keeps the revocation recorded until the instance manager acknowledges it", func() {
			// Only the instance manager's acknowledgement retires a revocation,
			// not the generation it was recorded against.
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeExternal})
			role.Status.ObservedGeneration = role.Generation
			role.Status.Applied = ptr.To(true)
			role.Status.Password = &apiv1.GeneratedPasswordState{PendingRevocation: true}
			r, cli := buildReconciler(role, newCluster())

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			Expect(getRole(cli, role).Status.Password.PendingRevocation).To(BeTrue())
		})

		It("leaves a secret it does not own in place", func() {
			// The Secret the status points at lost its owner reference, so the
			// operator has no claim on it any more.
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeExternal})
			role.Status.Password = &apiv1.GeneratedPasswordState{SecretName: "role-dante-password"}
			foreign := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "role-dante-password", Namespace: namespace},
				Data:       map[string][]byte{corev1.BasicAuthPasswordKey: []byte("user-managed")},
			}
			r, cli := buildReconciler(role, newCluster(), foreign)

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			Expect(string(getSecret(cli, "role-dante-password").Data[corev1.BasicAuthPasswordKey])).
				To(Equal("user-managed"))
			status := getRole(cli, role).Status.Password
			Expect(status.Message).To(ContainSubstring("not owned"))
			// The password in PostgreSQL came from that Secret, which is still
			// there to be read: revoking it would break a credential the
			// operator never issued.
			Expect(status.PendingRevocation).To(BeFalse())
		})
	})

	When("the password is read from an existing Secret (mode: secret)", func() {
		It("ignores a manual rotation request, since there is nothing to rotate", func() {
			existing := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "byo-secret", Namespace: namespace},
				Type:       corev1.SecretTypeBasicAuth,
				Data: map[string][]byte{
					corev1.BasicAuthUsernameKey: []byte(roleName),
					corev1.BasicAuthPasswordKey: []byte("user-managed"),
				},
			}
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{
				Mode:   apiv1.PasswordModeSecret,
				Secret: "byo-secret",
			})
			role.Annotations = map[string]string{utils.RotatePasswordAnnotationName: "requested"}
			r, cli := buildReconciler(role, newCluster(), existing)

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			Expect(string(getSecret(cli, "byo-secret").Data[corev1.BasicAuthPasswordKey])).
				To(Equal("user-managed"))
			Expect(getRole(cli, role).Annotations).NotTo(HaveKey(utils.RotatePasswordAnnotationName))
		})
	})

	When("rotation is manually requested", func() {
		It("rotates a password that would otherwise not be due yet, and clears the request", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{
				Mode:     apiv1.PasswordModeGenerate,
				Duration: &metav1.Duration{Duration: 90 * 24 * time.Hour},
			})
			r, cli := buildReconciler(role, newCluster())

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			first := getSecret(cli, "role-dante-password")
			Expect(recordedEvents(r)).To(ContainElement(ContainSubstring("Normal PasswordGenerated")))

			stored := getRole(cli, role)
			stored.Annotations = map[string]string{utils.RotatePasswordAnnotationName: "requested"}
			Expect(cli.Update(ctx, stored)).To(Succeed())

			_, err = r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			rotated := getSecret(cli, "role-dante-password")
			Expect(rotated.Data[corev1.BasicAuthPasswordKey]).NotTo(
				Equal(first.Data[corev1.BasicAuthPasswordKey]))
			// A rotation invalidates the password every consumer still holds,
			// so it has to leave a trace outside the operator's own log.
			Expect(recordedEvents(r)).To(ContainElement(SatisfyAll(
				ContainSubstring("Normal PasswordRotated"),
				ContainSubstring("role-dante-password"),
			)))

			// The request is one-shot: acted on, then removed.
			stored = getRole(cli, role)
			Expect(stored.Annotations).NotTo(HaveKey(utils.RotatePasswordAnnotationName))

			// Consuming the annotation must not cost the status written in the
			// same loop: the condition is how the instance manager learns of
			// the new password, and losing it would leave the rotated password
			// in its Secret and never applied. The issue time is what the next
			// deadline is computed from, so it must survive too.
			Expect(stored.Status.Password).NotTo(BeNil())
			Expect(stored.Status.Password.SecretName).To(Equal("role-dante-password"))
			Expect(stored.Status.Password.IssuedAt).NotTo(BeEmpty())
			cond := meta.FindStatusCondition(
				stored.Status.Conditions,
				string(apiv1.ConditionPasswordSecretChange),
			)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Message).To(Equal(rotated.ResourceVersion))
		})
	})
})

var _ = Describe("DatabaseRole password rotation from a lagging cache", func() {
	ctx := context.Background()

	const secretName = "role-dante-password"

	// cachedView stands in for an informer cache that has not caught up yet:
	// while set, it is what reading the role, or its password Secret, returns.
	type cachedView struct {
		role   *apiv1.DatabaseRole
		secret *corev1.Secret
		// secretMissing has the cache not see the password Secret at all.
		secretMissing bool
		// roleReads, when positive, limits how many reads of the role return
		// the stale one, after which the cache has caught up.
		roleReads int
	}

	var (
		r     *DatabaseRoleReconciler
		cli   client.Client
		cache *cachedView
		req   ctrl.Request
	)

	BeforeEach(func() {
		role := &apiv1.DatabaseRole{
			ObjectMeta: metav1.ObjectMeta{Name: "role-dante", Namespace: "default"},
			Spec: apiv1.DatabaseRoleSpec{
				RoleConfiguration: apiv1.RoleConfiguration{Name: "dante", Login: true},
				ClusterRef:        corev1.LocalObjectReference{Name: "cluster-example"},
				Password: &apiv1.PasswordConfiguration{
					Mode:        apiv1.PasswordModeGenerate,
					Duration:    &metav1.Duration{Duration: 5 * time.Minute},
					RenewBefore: &metav1.Duration{Duration: 2 * time.Minute},
				},
			},
		}
		cluster := &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-example", Namespace: "default"}}
		req = ctrl.Request{NamespacedName: client.ObjectKeyFromObject(role)}

		cache = &cachedView{}
		scheme := schemeBuilder.BuildWithAllKnownScheme()
		cli = fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&apiv1.DatabaseRole{}).
			WithObjects(role, cluster).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(
					ctx context.Context, c client.WithWatch, key client.ObjectKey,
					obj client.Object, opts ...client.GetOption,
				) error {
					switch typed := obj.(type) {
					case *apiv1.DatabaseRole:
						if cache.role != nil {
							cache.role.DeepCopyInto(typed)
							if cache.roleReads--; cache.roleReads == 0 {
								cache.role = nil
							}
							return nil
						}
					case *corev1.Secret:
						if key.Name != secretName {
							break
						}
						if cache.secretMissing {
							return apierrs.NewNotFound(corev1.Resource("secrets"), key.Name)
						}
						if cache.secret != nil {
							cache.secret.DeepCopyInto(typed)
							return nil
						}
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}).
			Build()
		r = &DatabaseRoleReconciler{Client: cli, Scheme: scheme, Recorder: record.NewFakeRecorder(eventBufferSize)}

		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
	})

	getRole := func() *apiv1.DatabaseRole {
		var role apiv1.DatabaseRole
		Expect(cli.Get(ctx, req.NamespacedName, &role)).To(Succeed())
		return &role
	}
	getSecret := func() *corev1.Secret {
		var secret corev1.Secret
		Expect(cli.Get(ctx, client.ObjectKey{Namespace: "default", Name: secretName}, &secret)).To(Succeed())
		return &secret
	}

	// pastDeadline moves the issue time of the password back into its renewal
	// window.
	pastDeadline := func() {
		role := getRole()
		role.Status.Password.IssuedAt = time.Now().Add(-4 * time.Minute).UTC().Format(time.RFC3339)
		Expect(cli.Status().Update(ctx, role)).To(Succeed())
	}

	It("neither rotates nor records anything from a role read before the rotation", func() {
		pastDeadline()
		before := getRole()

		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		rotated := getSecret().Data
		recorded := getRole().Status.Password

		// The rotation wakes the controller up again through the Secret it owns,
		// before the cache has seen the role it was recorded in. The lifetime
		// changed meanwhile: an expiration computed from that role would pair the
		// old issue time with it. The cache catches up before the status patch.
		before.Spec.Password.Duration = &metav1.Duration{Duration: 4 * time.Minute}
		cache.role, cache.roleReads = before, 1
		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		Expect(getSecret().Data).To(Equal(rotated))
		Expect(getRole().Status.Password).To(Equal(recorded))
	})

	It("requeues, rather than failing, while the cache keeps the role behind its status patch", func() {
		before := getRole()
		pastDeadline()
		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		cache.role = before
		result, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(time.Second))
	})

	It("records the issue time of a password it regenerates after an edit it had not seen", func() {
		pastDeadline()
		emptied := getSecret()
		emptied.Data[corev1.BasicAuthPasswordKey] = nil
		Expect(cli.Update(ctx, emptied)).To(Succeed())

		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		Expect(getSecret().Data[corev1.BasicAuthPasswordKey]).NotTo(BeEmpty())
		issuedAt, err := time.Parse(time.RFC3339, getRole().Status.Password.IssuedAt)
		Expect(err).NotTo(HaveOccurred())
		Expect(issuedAt).To(BeTemporally("~", time.Now(), 2*time.Second))
	})

	It("does not rotate a password the cache has not seen rotated yet", func() {
		pastDeadline()
		before := getRole()
		beforeSecret := getSecret()

		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		rotated := getSecret()
		Expect(rotated.Data).NotTo(Equal(beforeSecret.Data))

		// Both the role and the Secret are read as they were before the rotation.
		cache.role, cache.secret = before, beforeSecret
		result, err := r.Reconcile(ctx, req)
		cache.role, cache.secret = nil, nil

		Expect(getSecret().Data).To(Equal(rotated.Data))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(time.Second))
	})

	It("requeues, rather than failing, when the cache has not seen the Secret it generated", func() {
		generated := getSecret()

		// The role and the Secret are read as they were before the Secret was
		// generated.
		cache.role = getRole()
		cache.role.Status = apiv1.DatabaseRoleStatus{}
		cache.secretMissing = true
		result, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(time.Second))
		cache.role, cache.secretMissing = nil, false

		Expect(getSecret().Data).To(Equal(generated.Data))
	})

	It("keeps the password state of a renamed Secret when the role read predates the rename", func() {
		// The fake client numbers the versions of each object from one, while
		// the API server never gives two objects the same one: move the old
		// Secret past the first version the new one gets.
		old := getSecret()
		old.Labels = map[string]string{"touched": "true"}
		Expect(cli.Update(ctx, old)).To(Succeed())
		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		role := getRole()
		role.Spec.Password.Secret = "dante-renamed"
		Expect(cli.Update(ctx, role)).To(Succeed())
		before := getRole()

		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		recorded := getRole().Status.Password
		Expect(recorded.SecretName).To(Equal("dante-renamed"))

		// Clearing the state along with the old Secret would leave the role
		// with no expiration, and lift its VALID UNTIL, until the next loop.
		// The cache catches up before the status is patched.
		cache.role, cache.roleReads = before, 1
		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		Expect(getRole().Status.Password).To(Equal(recorded))
	})

	It("records the version of the Secret it wrote, not the one the cache still holds", func() {
		pastDeadline()
		beforeSecret := getSecret()

		cache.secret = beforeSecret
		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		cache.secret = nil

		rotated := getSecret()
		Expect(rotated.ResourceVersion).NotTo(Equal(beforeSecret.ResourceVersion))
		condition := meta.FindStatusCondition(getRole().Status.Conditions,
			string(apiv1.ConditionPasswordSecretChange))
		Expect(condition).NotTo(BeNil())
		Expect(condition.Message).To(Equal(rotated.ResourceVersion))
	})
})

var _ = Describe("pgpassLine", func() {
	It("escapes the field separator and the escape character in the password", func() {
		// The symbols a generated password may draw from include both, and an
		// unescaped one would shift every field after it, handing PostgreSQL a
		// different password, or a different role.
		Expect(string(pgpassLine("dante", `a:b\c`))).To(Equal(`*:5432:*:dante:a\:b\\c` + "\n"))
	})
})
