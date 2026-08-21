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

var _ = Describe("DatabaseRole password generation", func() {
	ctx := context.Background()

	const (
		namespace   = "default"
		clusterName = "cluster-example"
		roleName    = "dante"
	)

	newCluster := func(replica bool) *apiv1.Cluster {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
		}
		if replica {
			cluster.Spec.ReplicaCluster = &apiv1.ReplicaClusterConfiguration{
				Source:  "origin",
				Enabled: ptr.To(true),
			}
		}
		return cluster
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
		return &DatabaseRoleReconciler{Client: cli, Scheme: scheme}, cli
	}

	requestFor := func(role *apiv1.DatabaseRole) ctrl.Request {
		return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: role.Namespace, Name: role.Name}}
	}

	getSecret := func(cli client.Client, name string) *corev1.Secret {
		var secret corev1.Secret
		Expect(cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &secret)).To(Succeed())
		return &secret
	}

	getRole := func(cli client.Client, role *apiv1.DatabaseRole) *apiv1.DatabaseRole {
		var got apiv1.DatabaseRole
		Expect(cli.Get(ctx, client.ObjectKeyFromObject(role), &got)).To(Succeed())
		return &got
	}

	It("generates a basic-auth secret the instance manager can consume", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
		r, cli := buildReconciler(role, newCluster(false))

		result, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())
		// Without a lifetime there is nothing to renew, so no periodic requeue.
		Expect(result.RequeueAfter).To(BeZero())

		secret := getSecret(cli, "role-dante-password")
		Expect(secret.Type).To(Equal(corev1.SecretTypeBasicAuth))
		// The instance manager refuses a Secret whose username does not match the
		// PostgreSQL role name, not the DatabaseRole name.
		Expect(string(secret.Data[corev1.BasicAuthUsernameKey])).To(Equal(roleName))
		Expect(secret.Data[corev1.BasicAuthPasswordKey]).To(HaveLen(defaultPasswordLength))
		Expect(metav1.IsControlledBy(secret, role)).To(BeTrue())

		status := getRole(cli, role).Status.Password
		Expect(status).NotTo(BeNil())
		Expect(status.Expiration).To(BeEmpty())
		Expect(status.Message).To(BeEmpty())
	})

	It("tracks the generated secret in the PasswordSecretChange condition", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
		r, cli := buildReconciler(role, newCluster(false))

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		secret := getSecret(cli, "role-dante-password")
		cond := meta.FindStatusCondition(
			getRole(cli, role).Status.Conditions,
			string(apiv1.ConditionPasswordSecretChange),
		)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Message).To(Equal(secret.ResourceVersion))
	})

	It("does not rotate the password when no lifetime is requested", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
		r, cli := buildReconciler(role, newCluster(false))

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
		r, cli := buildReconciler(role, newCluster(false))

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		generated := string(getSecret(cli, "dante-credentials").Data[corev1.BasicAuthPasswordKey])
		Expect(generated).To(HaveLen(20))
		Expect(strings.Count(generated, "#") + strings.Count(generated, "$") +
			strings.Count(generated, "%")).To(Equal(3))
		Expect(generated).To(Equal(strings.ToLower(generated)))
		Expect(strings.Count(generated, "0") + strings.Count(generated, "1") + strings.Count(generated, "2") +
			strings.Count(generated, "3") + strings.Count(generated, "4") + strings.Count(generated, "5") +
			strings.Count(generated, "6") + strings.Count(generated, "7") + strings.Count(generated, "8") +
			strings.Count(generated, "9")).To(Equal(4))
	})

	It("deletes the secret it generated into before, once the name changes", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
		r, cli := buildReconciler(role, newCluster(false))

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())
		generated := string(getSecret(cli, "role-dante-password").Data[corev1.BasicAuthPasswordKey])

		stored := getRole(cli, role)
		stored.Spec.Password.Secret = "dante-credentials"
		Expect(cli.Update(ctx, stored)).To(Succeed())

		_, err = r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		// What the old Secret holds is not the password of the role any more, and
		// nothing but the deletion of the role itself would ever remove it.
		var secret corev1.Secret
		err = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "role-dante-password"}, &secret)
		Expect(apierrs.IsNotFound(err)).To(BeTrue())

		moved := getSecret(cli, "dante-credentials")
		Expect(string(moved.Data[corev1.BasicAuthPasswordKey])).NotTo(Equal(generated))
		Expect(getRole(cli, role).Status.Password.SecretName).To(Equal("dante-credentials"))
	})

	It("keeps the previous secret until it can generate into the new one", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
		cluster := newCluster(false)
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

		Expect(cli.Create(ctx, newCluster(false))).To(Succeed())
		_, err = r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		var secret corev1.Secret
		err = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "role-dante-password"}, &secret)
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
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
		r, cli := buildReconciler(role, newCluster(false))

		result, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		var secret corev1.Secret
		err = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "role-dante-password"}, &secret)
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
		Expect(getRole(cli, role).Status.Password.Message).To(
			ContainSubstring("cannot generate a password matching the requested criteria"))
	})

	When("a lifetime is requested", func() {
		It("records the issue time, expiration and renewal deadline, and asks to be requeued", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{
				Mode:        apiv1.PasswordModeGenerate,
				Duration:    &metav1.Duration{Duration: 90 * 24 * time.Hour},
				RenewBefore: &metav1.Duration{Duration: 7 * 24 * time.Hour},
			})
			r, cli := buildReconciler(role, newCluster(false))

			result, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			// Requeued at the renewal deadline (83 days away), not the flat
			// hourly interval, which would leave a short-lived password
			// overdue for most of its life.
			Expect(result.RequeueAfter).To(BeNumerically("~", 83*24*time.Hour, time.Minute))

			status := getRole(cli, role).Status.Password
			issuedAt, err := time.Parse(time.RFC3339, status.IssuedAt)
			Expect(err).NotTo(HaveOccurred())
			Expect(issuedAt).To(BeTemporally("~", time.Now(), time.Minute))

			expiration, err := time.Parse(time.RFC3339, status.Expiration)
			Expect(err).NotTo(HaveOccurred())
			Expect(expiration).To(BeTemporally("~", time.Now().Add(90*24*time.Hour), time.Minute))
		})

		It("rotates immediately once a shortened duration is already exceeded, "+
			"instead of honoring the deadline computed under the previous one", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{
				Mode:     apiv1.PasswordModeGenerate,
				Duration: &metav1.Duration{Duration: 90 * 24 * time.Hour},
			})
			r, cli := buildReconciler(role, newCluster(false))

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			first := getSecret(cli, "role-dante-password")

			// Back-date the issue time to simulate a password that has already
			// lived two days: under the original 90-day duration it is nowhere
			// near due, which is the point of the scenario below. The status
			// still carries the expiration computed for that 90-day duration,
			// which the fix must not trust once the duration changes.
			stored := getRole(cli, role)
			issuedAt := time.Now().Add(-48 * time.Hour)
			stored.Status.Password.IssuedAt = issuedAt.UTC().Format(time.RFC3339)
			Expect(cli.Status().Update(ctx, stored)).To(Succeed())

			// Shorten the duration to one hour, as an operator would when
			// rotating a leaked credential quickly: the two-day-old password is
			// now well past its shortened deadline.
			stored = getRole(cli, role)
			stored.Spec.Password.Duration = &metav1.Duration{Duration: time.Hour}
			Expect(cli.Update(ctx, stored)).To(Succeed())

			_, err = r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			rotated := getSecret(cli, "role-dante-password")
			Expect(rotated.Data[corev1.BasicAuthPasswordKey]).NotTo(
				Equal(first.Data[corev1.BasicAuthPasswordKey]))
		})

		It("rotates the password once inside the renewal window", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{
				Mode:        apiv1.PasswordModeGenerate,
				Duration:    &metav1.Duration{Duration: 90 * 24 * time.Hour},
				RenewBefore: &metav1.Duration{Duration: 7 * 24 * time.Hour},
			})
			r, cli := buildReconciler(role, newCluster(false))

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			first := getSecret(cli, "role-dante-password")

			// Move the recorded issue time back far enough that the renewal
			// window (duration minus renewBefore) has already been entered.
			// The deadline is always recomputed from the issue time and the
			// role's current duration/renewBefore, so backdating the
			// expiration directly would no longer have any effect.
			stored := getRole(cli, role)
			issuedAt := time.Now().Add(-84 * 24 * time.Hour)
			stored.Status.Password.IssuedAt = issuedAt.UTC().Format(time.RFC3339)
			Expect(cli.Status().Update(ctx, stored)).To(Succeed())

			_, err = r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			rotated := getSecret(cli, "role-dante-password")
			Expect(rotated.Data[corev1.BasicAuthPasswordKey]).NotTo(
				Equal(first.Data[corev1.BasicAuthPasswordKey]))
			expiration, err := time.Parse(time.RFC3339, getRole(cli, role).Status.Password.Expiration)
			Expect(err).NotTo(HaveOccurred())
			Expect(expiration).To(BeTemporally(">", time.Now().Add(80*24*time.Hour)))
		})

		It("does not rotate on every loop when the default renewal window exceeds the lifetime", func() {
			// The operator-wide threshold is 7 days: taken literally, a password
			// living one hour would be due for renewal the moment it is generated.
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{
				Mode:     apiv1.PasswordModeGenerate,
				Duration: &metav1.Duration{Duration: time.Hour},
			})
			r, cli := buildReconciler(role, newCluster(false))

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			first := getSecret(cli, "role-dante-password")

			_, err = r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			Expect(getSecret(cli, "role-dante-password").Data).To(Equal(first.Data))
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
			r, cli := buildReconciler(role, newCluster(false), stale)

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			rotated := getSecret(cli, "role-dante-password")
			Expect(string(rotated.Data[corev1.BasicAuthPasswordKey])).NotTo(Equal("ancient-password"))
			Expect(getRole(cli, role).Status.Password.Expiration).NotTo(BeEmpty())
		})

		It("keeps a password younger than the requested lifetime, recording its expiration", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{
				Mode:     apiv1.PasswordModeGenerate,
				Duration: &metav1.Duration{Duration: 90 * 24 * time.Hour},
			})
			fresh := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "role-dante-password",
					Namespace:         namespace,
					CreationTimestamp: metav1.NewTime(time.Now().Add(-24 * time.Hour)),
					OwnerReferences: []metav1.OwnerReference{
						*metav1.NewControllerRef(role, apiv1.SchemeGroupVersion.WithKind("DatabaseRole")),
					},
				},
				Type: corev1.SecretTypeBasicAuth,
				Data: map[string][]byte{
					corev1.BasicAuthUsernameKey: []byte(roleName),
					corev1.BasicAuthPasswordKey: []byte("recent-password"),
				},
			}
			r, cli := buildReconciler(role, newCluster(false), fresh)

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			kept := getSecret(cli, "role-dante-password")
			Expect(string(kept.Data[corev1.BasicAuthPasswordKey])).To(Equal("recent-password"))
			expiration, err := time.Parse(time.RFC3339, getRole(cli, role).Status.Password.Expiration)
			Expect(err).NotTo(HaveOccurred())
			Expect(expiration).To(BeTemporally("~", time.Now().Add(89*24*time.Hour), time.Minute))
		})

		It("rotates the password when the recorded issue time cannot be read", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{
				Mode:     apiv1.PasswordModeGenerate,
				Duration: &metav1.Duration{Duration: 90 * 24 * time.Hour},
			})
			r, cli := buildReconciler(role, newCluster(false))

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

		It("forgets the expiration once the lifetime is removed", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{
				Mode:     apiv1.PasswordModeGenerate,
				Duration: &metav1.Duration{Duration: 90 * 24 * time.Hour},
			})
			r, cli := buildReconciler(role, newCluster(false))

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			first := getSecret(cli, "role-dante-password")
			Expect(getRole(cli, role).Status.Password.Expiration).NotTo(BeEmpty())

			stored := getRole(cli, role)
			stored.Spec.Password.Duration = nil
			Expect(cli.Update(ctx, stored)).To(Succeed())

			result, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			kept := getSecret(cli, "role-dante-password")
			Expect(kept.Data).To(Equal(first.Data))
			Expect(getRole(cli, role).Status.Password.Expiration).To(BeEmpty())
		})
	})

	It("regenerates a password that was emptied out of band", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
		r, cli := buildReconciler(role, newCluster(false))

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		emptied := getSecret(cli, "role-dante-password")
		emptied.Data[corev1.BasicAuthPasswordKey] = nil
		Expect(cli.Update(ctx, emptied)).To(Succeed())

		_, err = r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())
		Expect(getSecret(cli, "role-dante-password").Data[corev1.BasicAuthPasswordKey]).
			To(HaveLen(defaultPasswordLength))
	})

	It("refuses the secret reserved for the client certificate of the role", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{
			Mode:   apiv1.PasswordModeGenerate,
			Secret: "role-dante-client-cert",
		})
		r, cli := buildReconciler(role, newCluster(false))

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		// Both reconcilers own that Secret, so writing the password into it would
		// have them overwrite each other's data on every loop.
		var secret corev1.Secret
		err = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "role-dante-client-cert"}, &secret)
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
		Expect(getRole(cli, role).Status.Password.Message).To(ContainSubstring("client certificate"))
	})

	It("puts back the username of the role without rotating the password", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
		r, cli := buildReconciler(role, newCluster(false))

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())
		generated := string(getSecret(cli, "role-dante-password").Data[corev1.BasicAuthPasswordKey])

		secret := getSecret(cli, "role-dante-password")
		delete(secret.Data, corev1.BasicAuthUsernameKey)
		Expect(cli.Update(ctx, secret)).To(Succeed())

		_, err = r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		// The instance manager rejects a Secret naming another role, and there is
		// no reason to change a password that is still good.
		repaired := getSecret(cli, "role-dante-password")
		Expect(string(repaired.Data[corev1.BasicAuthUsernameKey])).To(Equal(roleName))
		Expect(string(repaired.Data[corev1.BasicAuthPasswordKey])).To(Equal(generated))
	})

	It("keeps rotating ahead of the deadline when the expiry check threshold is disabled", func() {
		configuration.Current = configuration.NewConfiguration()
		configuration.Current.ExpiringCheckThreshold = 0
		DeferCleanup(func() { configuration.Current = configuration.NewConfiguration() })

		role := newRoleWithPassword(&apiv1.PasswordConfiguration{
			Mode:     apiv1.PasswordModeGenerate,
			Duration: &metav1.Duration{Duration: 30 * 24 * time.Hour},
		})
		Expect(passwordRenewBefore(role)).To(Equal(7 * 24 * time.Hour))
	})

	It("records the password it generated even when the client certificate fails", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
		role.Spec.ClientCertificate = &apiv1.ClientCertificateConfiguration{}
		cluster := newCluster(false)

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
		r := &DatabaseRoleReconciler{Client: cli, Scheme: scheme}

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

	It("does not rotate the password of a role that is being deleted", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{
			Mode:     apiv1.PasswordModeGenerate,
			Duration: &metav1.Duration{Duration: time.Hour},
		})
		r, cli := buildReconciler(role, newCluster(false))

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())
		generated := string(getSecret(cli, "role-dante-password").Data[corev1.BasicAuthPasswordKey])

		// The role is deleted while the instance manager finalizer still holds it,
		// with the password already inside its renewal window.
		stored := getRole(cli, role)
		stored.Finalizers = []string{utils.RoleFinalizerName}
		Expect(cli.Update(ctx, stored)).To(Succeed())
		Expect(cli.Delete(ctx, stored)).To(Succeed())

		stored = getRole(cli, role)
		stored.Status.Password.IssuedAt = time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
		Expect(cli.Status().Update(ctx, stored)).To(Succeed())

		_, err = r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		// A password rotated now would be applied to a role that is about to be
		// retained, and then garbage collected together with its Secret.
		Expect(string(getSecret(cli, "role-dante-password").Data[corev1.BasicAuthPasswordKey])).
			To(Equal(generated))
	})

	It("never overwrites a secret it does not own", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
		foreign := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "role-dante-password", Namespace: namespace},
			Type:       corev1.SecretTypeBasicAuth,
			Data: map[string][]byte{
				corev1.BasicAuthUsernameKey: []byte(roleName),
				corev1.BasicAuthPasswordKey: []byte("user-managed"),
			},
		}
		r, cli := buildReconciler(role, newCluster(false), foreign)

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		Expect(string(getSecret(cli, "role-dante-password").Data[corev1.BasicAuthPasswordKey])).
			To(Equal("user-managed"))
		Expect(getRole(cli, role).Status.Password.Message).To(ContainSubstring("not owned"))
	})

	It("keeps the deadline of a password it can no longer rotate", func() {
		// The lifetime of a generated password is also the VALID UNTIL of the
		// role, so forgetting the deadline while the operator cannot rotate the
		// password lifts the expiration PostgreSQL enforces, turning a stalled
		// rotation into a credential that works forever.
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{
			Mode:     apiv1.PasswordModeGenerate,
			Duration: &metav1.Duration{Duration: 90 * 24 * time.Hour},
		})
		r, cli := buildReconciler(role, newCluster(false))

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())
		expiration := getRole(cli, role).Status.Password.Expiration
		Expect(expiration).NotTo(BeEmpty())

		// The user takes the generated Secret over, so the operator stops
		// rotating the password it holds.
		taken := getSecret(cli, "role-dante-password")
		taken.OwnerReferences = nil
		Expect(cli.Update(ctx, taken)).To(Succeed())

		_, err = r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		status := getRole(cli, role).Status.Password
		Expect(status.Message).To(ContainSubstring("not owned"))
		Expect(status.Expiration).To(Equal(expiration))
		Expect(status.IssuedAt).NotTo(BeEmpty())
	})

	It("skips generation on a replica cluster", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
		r, cli := buildReconciler(role, newCluster(true))

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		var secret corev1.Secret
		err = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "role-dante-password"}, &secret)
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
		Expect(getRole(cli, role).Status.Password.Message).To(ContainSubstring("replica cluster"))
	})

	When("generation is turned off", func() {
		It("deletes the secret it generated", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
			r, cli := buildReconciler(role, newCluster(false))

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			Expect(getSecret(cli, "role-dante-password")).NotTo(BeNil())

			stored := getRole(cli, role)
			stored.Spec.Password.Mode = apiv1.PasswordModeExternal
			Expect(cli.Update(ctx, stored)).To(Succeed())

			_, err = r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			var secret corev1.Secret
			err = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "role-dante-password"}, &secret)
			Expect(apierrs.IsNotFound(err)).To(BeTrue())

			updated := getRole(cli, role)
			// The password the Secret held is still set on the role in
			// PostgreSQL, and nothing can read it any more: the only way out is
			// to have the instance manager revoke it.
			Expect(updated.Status.Password).NotTo(BeNil())
			Expect(updated.Status.Password.PendingRevocation).To(BeTrue())
			Expect(updated.Status.Password.SecretName).To(BeEmpty())
			// With no password Secret left, the signal for the instance manager
			// must go away too.
			Expect(meta.FindStatusCondition(
				updated.Status.Conditions,
				string(apiv1.ConditionPasswordSecretChange),
			)).To(BeNil())
		})

		It("deletes the secret it generated when the password is set to NULL", func() {
			// The Secret lifecycle is identical to turning generation off: the
			// distinction between `external` and `setNull` only matters to the
			// instance manager, which decides whether to leave the PostgreSQL
			// password alone or set it to NULL.
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
			r, cli := buildReconciler(role, newCluster(false))

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			Expect(getSecret(cli, "role-dante-password")).NotTo(BeNil())

			stored := getRole(cli, role)
			stored.Spec.Password.Mode = apiv1.PasswordModeSetNull
			Expect(cli.Update(ctx, stored)).To(Succeed())

			_, err = r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			var secret corev1.Secret
			err = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "role-dante-password"}, &secret)
			Expect(apierrs.IsNotFound(err)).To(BeTrue())
			Expect(getRole(cli, role).Status.Password).To(BeNil())
		})

		It("deletes the secret it generated under a name of its own", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{
				Mode:   apiv1.PasswordModeGenerate,
				Secret: "dante-credentials",
			})
			r, cli := buildReconciler(role, newCluster(false))

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			Expect(getRole(cli, role).Status.Password.SecretName).To(Equal("dante-credentials"))

			// The name the password was generated into is gone from the
			// specification, since `secret` is only allowed in the two modes
			// that name a Secret: the status is what the operator recognizes
			// its own Secret by.
			stored := getRole(cli, role)
			stored.Spec.Password = &apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeExternal}
			Expect(cli.Update(ctx, stored)).To(Succeed())

			_, err = r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			var secret corev1.Secret
			err = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "dante-credentials"}, &secret)
			Expect(apierrs.IsNotFound(err)).To(BeTrue())
		})

		It("keeps track of its secret across a state it cannot generate in", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{
				Mode:   apiv1.PasswordModeGenerate,
				Secret: "dante-credentials",
			})
			cluster := newCluster(false)
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

			var secret corev1.Secret
			err = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "dante-credentials"}, &secret)
			Expect(apierrs.IsNotFound(err)).To(BeTrue())
		})

		It("deletes the secret it generated even when the role switches to reading from that same name", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
			r, cli := buildReconciler(role, newCluster(false))

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			Expect(getSecret(cli, "role-dante-password")).NotTo(BeNil())

			// Asking to read the password back from the very Secret the
			// operator generated it into is not a handover: what the operator
			// created, the operator deletes, so the role is left pointing at a
			// Secret that no longer exists rather than at one nobody
			// maintains.
			stored := getRole(cli, role)
			stored.Spec.Password = &apiv1.PasswordConfiguration{
				Mode:   apiv1.PasswordModeSecret,
				Secret: "role-dante-password",
			}
			Expect(cli.Update(ctx, stored)).To(Succeed())

			_, err = r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			var secret corev1.Secret
			err = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "role-dante-password"}, &secret)
			Expect(apierrs.IsNotFound(err)).To(BeTrue())
			// Nothing to revoke: the role reads its password from a Secret now,
			// even if that Secret has to be created again.
			Expect(getRole(cli, role).Status.Password).To(BeNil())
		})

		It("keeps the revocation recorded until the instance manager acknowledges it", func() {
			// The instance manager may well have applied this generation of the
			// role before the operator got to record the revocation, so the
			// record cannot be dropped on the strength of the generation alone:
			// only the acknowledgement retires it.
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeExternal})
			role.Status.ObservedGeneration = role.Generation
			role.Status.Applied = ptr.To(true)
			role.Status.Password = &apiv1.GeneratedPasswordState{PendingRevocation: true}
			r, cli := buildReconciler(role, newCluster(false))

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			Expect(getRole(cli, role).Status.Password.PendingRevocation).To(BeTrue())
		})

		It("forgets the revocation once the instance manager acknowledged it", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeExternal})
			role.Status.Password = &apiv1.GeneratedPasswordState{}
			r, cli := buildReconciler(role, newCluster(false))

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			Expect(getRole(cli, role).Status.Password).To(BeNil())
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
			r, cli := buildReconciler(role, newCluster(false), foreign)

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

		It("does not look for a secret it never generated", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeExternal})
			foreign := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "role-dante-password", Namespace: namespace},
				Data:       map[string][]byte{corev1.BasicAuthPasswordKey: []byte("user-managed")},
			}
			r, cli := buildReconciler(role, newCluster(false), foreign)

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			// Nothing was ever generated for this role, so a Secret that happens to
			// carry the name generation would have used is none of its business.
			Expect(string(getSecret(cli, "role-dante-password").Data[corev1.BasicAuthPasswordKey])).
				To(Equal("user-managed"))
			Expect(getRole(cli, role).Status.Password).To(BeNil())
		})
	})

	When("the password is read from an existing Secret (mode: secret)", func() {
		It("generates nothing and tracks the named Secret's ResourceVersion", func() {
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
			r, cli := buildReconciler(role, newCluster(false), existing)

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			// The Secret is untouched: no owner reference, no rewritten data.
			untouched := getSecret(cli, "byo-secret")
			Expect(untouched.Data[corev1.BasicAuthPasswordKey]).To(Equal([]byte("user-managed")))
			Expect(metav1.IsControlledBy(untouched, role)).To(BeFalse())

			// Nothing was generated, so there is no generated-password state to
			// report; the condition still tracks the Secret the role uses.
			Expect(getRole(cli, role).Status.Password).To(BeNil())
			cond := meta.FindStatusCondition(
				getRole(cli, role).Status.Conditions,
				string(apiv1.ConditionPasswordSecretChange),
			)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Message).To(Equal(untouched.ResourceVersion))
		})

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
			r, cli := buildReconciler(role, newCluster(false), existing)

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
			r, cli := buildReconciler(role, newCluster(false))

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			first := getSecret(cli, "role-dante-password")

			stored := getRole(cli, role)
			stored.Annotations = map[string]string{utils.RotatePasswordAnnotationName: "requested"}
			Expect(cli.Update(ctx, stored)).To(Succeed())

			_, err = r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			rotated := getSecret(cli, "role-dante-password")
			Expect(rotated.Data[corev1.BasicAuthPasswordKey]).NotTo(
				Equal(first.Data[corev1.BasicAuthPasswordKey]))

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

		It("rotates a password with no lifetime configured at all", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
			r, cli := buildReconciler(role, newCluster(false))

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())
			first := getSecret(cli, "role-dante-password")

			stored := getRole(cli, role)
			stored.Annotations = map[string]string{utils.RotatePasswordAnnotationName: "requested"}
			Expect(cli.Update(ctx, stored)).To(Succeed())

			_, err = r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			rotated := getSecret(cli, "role-dante-password")
			Expect(rotated.Data[corev1.BasicAuthPasswordKey]).NotTo(
				Equal(first.Data[corev1.BasicAuthPasswordKey]))
			Expect(getRole(cli, role).Annotations).NotTo(HaveKey(utils.RotatePasswordAnnotationName))
			// Nothing expires: the role asked for no lifetime, so the request
			// must not invent a deadline for the password it just issued.
			Expect(getRole(cli, role).Status.Password.Expiration).To(BeEmpty())
		})

		It("clears a request that has nothing to rotate", func() {
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeExternal})
			role.Annotations = map[string]string{utils.RotatePasswordAnnotationName: "requested"}
			r, cli := buildReconciler(role, newCluster(false))

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			Expect(getRole(cli, role).Annotations).NotTo(HaveKey(utils.RotatePasswordAnnotationName))
		})

		It("keeps a request generation is only temporarily blocked from honoring", func() {
			// A replica cluster owns the password of its roles: the rotation
			// cannot happen here, and the request must wait rather than be
			// consumed without effect.
			role := newRoleWithPassword(&apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate})
			role.Annotations = map[string]string{utils.RotatePasswordAnnotationName: "requested"}
			r, cli := buildReconciler(role, newCluster(true))

			_, err := r.Reconcile(ctx, requestFor(role))
			Expect(err).NotTo(HaveOccurred())

			Expect(getRole(cli, role).Annotations).To(HaveKey(utils.RotatePasswordAnnotationName))
		})
	})

	It("regenerates the password once its Secret is deleted", func() {
		role := newRoleWithPassword(&apiv1.PasswordConfiguration{
			Mode:     apiv1.PasswordModeGenerate,
			Duration: &metav1.Duration{Duration: 90 * 24 * time.Hour},
		})
		r, cli := buildReconciler(role, newCluster(false))

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())
		first := getSecret(cli, "role-dante-password")

		Expect(cli.Delete(ctx, first)).To(Succeed())

		_, err = r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		rotated := getSecret(cli, "role-dante-password")
		Expect(rotated.Data[corev1.BasicAuthPasswordKey]).NotTo(
			Equal(first.Data[corev1.BasicAuthPasswordKey]))
	})
})
