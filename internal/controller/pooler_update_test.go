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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs/pgbouncer"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("unit test of pooler_update reconciliation logic", func() {
	var env *testingEnvironment

	AfterEach(func() {
		configuration.Current = configuration.NewConfiguration()
	})

	BeforeEach(func() {
		env = buildTestEnvironment()
		configuration.Current = configuration.NewConfiguration()
	})

	It("it should test the deployment update logic", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		res := &poolerManagedResources{Deployment: nil, Cluster: cluster}

		By("making sure that the deployment doesn't already exists", func() {
			deployment := &appsv1.Deployment{}
			err := env.client.Get(
				ctx,
				types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace},
				deployment,
			)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		By("making sure that updateDeployment creates the deployment", func() {
			err := env.poolerReconciler.updateDeployment(ctx, pooler, res)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Deployment).ToNot(BeNil())

			deployment := getPoolerDeployment(ctx, env.client, pooler)
			Expect(deployment.Spec.Replicas).To(Equal(pooler.Spec.Instances))
		})

		By("making sure that if the pooler.spec doesn't change the deployment isn't updated", func() {
			beforeDep := getPoolerDeployment(ctx, env.client, pooler)

			err := env.poolerReconciler.updateDeployment(ctx, pooler, res)
			Expect(err).ToNot(HaveOccurred())

			afterDep := getPoolerDeployment(ctx, env.client, pooler)
			Expect(beforeDep.ResourceVersion).To(Equal(afterDep.ResourceVersion))
			Expect(beforeDep.Annotations[utils.PoolerSpecHashAnnotationName]).
				To(Equal(afterDep.Annotations[utils.PoolerSpecHashAnnotationName]))
		})

		By("making sure that the deployments gets updated if the pooler.spec changes", func() {
			const instancesNumber int32 = 3
			poolerUpdate := pooler.DeepCopy()
			poolerUpdate.Spec.Instances = ptr.To(instancesNumber)

			beforeDep := getPoolerDeployment(ctx, env.client, poolerUpdate)

			err := env.poolerReconciler.updateDeployment(ctx, poolerUpdate, res)
			Expect(err).ToNot(HaveOccurred())

			afterDep := getPoolerDeployment(ctx, env.client, poolerUpdate)

			Expect(beforeDep.ResourceVersion).ToNot(Equal(afterDep.ResourceVersion))
			Expect(beforeDep.Annotations[utils.PoolerSpecHashAnnotationName]).
				ToNot(Equal(afterDep.Annotations[utils.PoolerSpecHashAnnotationName]))
			Expect(*afterDep.Spec.Replicas).To(Equal(instancesNumber))
		})
	})

	It("should test the ServiceAccount and RBAC update logic", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		res := &poolerManagedResources{Cluster: cluster, ServiceAccount: nil}

		By("making sure the serviceAccount doesn't already exist", func() {
			sa := &corev1.ServiceAccount{}

			err := env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace}, sa)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		By("making sure that updateServiceAccount function creates the SA", func() {
			err := env.poolerReconciler.updateServiceAccount(ctx, pooler, res)
			Expect(err).ToNot(HaveOccurred())

			sa := &corev1.ServiceAccount{}

			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace}, sa)
			Expect(err).ToNot(HaveOccurred())

			Expect(sa.ImagePullSecrets).To(BeEmpty())

			res.ServiceAccount = sa
		})

		By("making sure that SA isn't updated if we don't change anything", func() {
			// the managedResources object is mutated, so we need to store the information
			beforeResourceVersion := res.ServiceAccount.ResourceVersion

			err := env.poolerReconciler.updateServiceAccount(ctx, pooler, res)
			Expect(err).ToNot(HaveOccurred())

			afterSa := &corev1.ServiceAccount{}

			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace}, afterSa)
			Expect(err).ToNot(HaveOccurred())

			Expect(beforeResourceVersion).To(Equal(afterSa.ResourceVersion))
		})

		By("creating the requirement for the imagePullSecret", func() {
			namespace := newFakeNamespace(env.client)

			configuration.Current.OperatorPullSecretName = "test-secret-pull"
			configuration.Current.OperatorNamespace = namespace

			pullSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configuration.Current.OperatorPullSecretName,
					Namespace: configuration.Current.OperatorNamespace,
				},
				Data: map[string][]byte{
					corev1.TLSCertKey:       []byte("test-cert"),
					corev1.TLSPrivateKeyKey: []byte("test-key"),
				},
				Type: corev1.SecretTypeTLS,
			}

			err := env.client.Create(ctx, pullSecret)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure it updates the SA if there are changes", func() {
			// the managedResources object is mutated, so we need to store the information
			beforeResourceVersion := res.ServiceAccount.ResourceVersion

			err := env.poolerReconciler.updateServiceAccount(ctx, pooler, res)
			Expect(err).ToNot(HaveOccurred())

			afterSa := &corev1.ServiceAccount{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace}, afterSa)
			Expect(err).ToNot(HaveOccurred())

			Expect(afterSa.ImagePullSecrets).To(HaveLen(1))
			Expect(afterSa.ImagePullSecrets).To(ContainElement(corev1.LocalObjectReference{
				Name: pooler.Name + "-pull",
			}))
			Expect(beforeResourceVersion).ToNot(Equal(afterSa.ResourceVersion))
		})

		By("making sure RBAC doesn't exist", func() {
			role := &rbacv1.Role{}
			err := env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace}, role)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			roleBinding := &rbacv1.RoleBinding{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace}, roleBinding)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		By("making sure that updateRBAC function creates the RBAC", func() {
			err := env.poolerReconciler.updateRBAC(ctx, pooler, res)
			Expect(err).ToNot(HaveOccurred())

			expectedRole := pgbouncer.Role(pooler)
			role := &rbacv1.Role{}
			err = env.client.Get(ctx, types.NamespacedName{Name: expectedRole.Name, Namespace: expectedRole.Namespace}, role)
			Expect(err).ToNot(HaveOccurred())

			Expect(expectedRole.Rules).To(Equal(role.Rules))

			expectedRb := pgbouncer.RoleBinding(pooler, pooler.GetServiceAccountName())
			roleBinding := &rbacv1.RoleBinding{}
			err = env.client.Get(ctx, types.NamespacedName{Name: expectedRb.Name, Namespace: expectedRb.Namespace}, roleBinding)
			Expect(err).ToNot(HaveOccurred())

			Expect(expectedRb.Subjects).To(Equal(roleBinding.Subjects))
			Expect(expectedRb.RoleRef).To(Equal(roleBinding.RoleRef))
		})
	})

	It("should reconcileService works correctly", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		res := &poolerManagedResources{Cluster: cluster}

		By("making sure the service doesn't exist", func() {
			svc := &corev1.Service{}
			expectedSVC, err := pgbouncer.Service(pooler, cluster)
			Expect(err).ToNot(HaveOccurred())
			err = env.client.Get(ctx, types.NamespacedName{Name: expectedSVC.Name, Namespace: expectedSVC.Namespace}, svc)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		By("making sure it creates the service", func() {
			err := env.poolerReconciler.reconcileService(ctx, pooler, res)
			Expect(err).ToNot(HaveOccurred())

			svc := &corev1.Service{}
			expectedSVC, err := pgbouncer.Service(pooler, cluster)
			Expect(err).ToNot(HaveOccurred())
			err = env.client.Get(ctx, types.NamespacedName{Name: expectedSVC.Name, Namespace: expectedSVC.Namespace}, svc)
			Expect(err).ToNot(HaveOccurred())

			Expect(expectedSVC.Labels[utils.ClusterLabelName]).To(Equal(cluster.Name))
			Expect(expectedSVC.Labels[utils.PgbouncerNameLabel]).To(Equal(pooler.Name))
			Expect(expectedSVC.Spec.Selector).To(Equal(svc.Spec.Selector))
			Expect(expectedSVC.Spec.Ports).To(Equal(svc.Spec.Ports))
			Expect(expectedSVC.Spec.Type).To(Equal(svc.Spec.Type))
			res.Service = svc
		})

		By("making sure the svc doesn't get updated if there are not changes", func() {
			previousService := res.Service.DeepCopy()
			err := env.poolerReconciler.reconcileService(ctx, pooler, res)
			Expect(err).ToNot(HaveOccurred())

			svc := &corev1.Service{}
			expectedSVC, err := pgbouncer.Service(pooler, cluster)
			Expect(err).ToNot(HaveOccurred())
			err = env.client.Get(ctx, types.NamespacedName{Name: expectedSVC.Name, Namespace: expectedSVC.Namespace}, svc)
			Expect(err).ToNot(HaveOccurred())
			Expect(previousService.Spec).To(BeEquivalentTo(svc.Spec))
			Expect(previousService.Labels).To(BeEquivalentTo(svc.Labels))
		})

		By("making sure it reconciles if differences from the living and expected service are present", func() {
			previousName := cluster.Name
			previousResourceVersion := res.Service.ResourceVersion
			cluster.Name = "new-name"

			err := env.poolerReconciler.reconcileService(ctx, pooler, res)
			Expect(err).ToNot(HaveOccurred())

			svc := &corev1.Service{}
			expectedSVC, err := pgbouncer.Service(pooler, cluster)
			Expect(err).ToNot(HaveOccurred())
			err = env.client.Get(ctx, types.NamespacedName{Name: expectedSVC.Name, Namespace: expectedSVC.Namespace}, svc)
			Expect(err).ToNot(HaveOccurred())
			Expect(previousResourceVersion).ToNot(Equal(svc.ResourceVersion))
			Expect(expectedSVC.Labels[utils.ClusterLabelName]).ToNot(Equal(previousName))
			Expect(expectedSVC.Labels[utils.ClusterLabelName]).To(Equal(cluster.Name))
		})
	})

	It("should not reconcile if pooler has podSpec reconciliation disabled", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		res := &poolerManagedResources{Deployment: nil, Cluster: cluster}
		By("setting the reconcilePodSpec annotation to disabled on the pooler ", func() {
			pooler.Annotations[utils.ReconcilePodSpecAnnotationName] = "disabled"
			pooler.Spec.Template = &apiv1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: ptr.To(int64(100)),
				},
			}
		})

		By("making sure that updateDeployment creates the deployment", func() {
			err := env.poolerReconciler.updateDeployment(ctx, pooler, res)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Deployment).ToNot(BeNil())

			deployment := getPoolerDeployment(ctx, env.client, pooler)
			Expect(deployment.Spec.Replicas).To(Equal(pooler.Spec.Instances))
		})

		By("making sure pooler change does not update the deployment", func() {
			beforeDep := getPoolerDeployment(ctx, env.client, pooler)
			pooler.Spec.Template.Spec.TerminationGracePeriodSeconds = ptr.To(int64(200))
			err := env.poolerReconciler.updateDeployment(ctx, pooler, res)
			Expect(err).ToNot(HaveOccurred())

			afterDep := getPoolerDeployment(ctx, env.client, pooler)
			Expect(afterDep.Spec.Template.Spec.TerminationGracePeriodSeconds).To(
				Equal(beforeDep.Spec.Template.Spec.TerminationGracePeriodSeconds))
			Expect(beforeDep.Annotations[utils.PoolerSpecHashAnnotationName]).
				NotTo(Equal(afterDep.Annotations[utils.PoolerSpecHashAnnotationName]))
		})

		By("making sure that the deployments gets updated if the pooler.spec changes", func() {
			const instancesNumber int32 = 3
			poolerUpdate := pooler.DeepCopy()
			poolerUpdate.Spec.Instances = ptr.To(instancesNumber)

			beforeDep := getPoolerDeployment(ctx, env.client, poolerUpdate)

			err := env.poolerReconciler.updateDeployment(ctx, poolerUpdate, res)
			Expect(err).ToNot(HaveOccurred())

			afterDep := getPoolerDeployment(ctx, env.client, poolerUpdate)
			Expect(beforeDep.Annotations[utils.PoolerSpecHashAnnotationName]).
				ToNot(Equal(afterDep.Annotations[utils.PoolerSpecHashAnnotationName]))
			Expect(*afterDep.Spec.Replicas).To(Equal(instancesNumber))
		})

		By("enable again, making sure pooler change updates the deployment", func() {
			delete(pooler.Annotations, utils.ReconcilePodSpecAnnotationName)
			beforeDep := getPoolerDeployment(ctx, env.client, pooler)
			pooler.Spec.Template.Spec.TerminationGracePeriodSeconds = ptr.To(int64(300))
			err := env.poolerReconciler.updateDeployment(ctx, pooler, res)
			Expect(err).ToNot(HaveOccurred())

			afterDep := getPoolerDeployment(ctx, env.client, pooler)
			Expect(afterDep.Spec.Template.Spec.TerminationGracePeriodSeconds).NotTo(
				Equal(beforeDep.Spec.Template.Spec.TerminationGracePeriodSeconds))
			Expect(afterDep.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(ptr.To(int64(300))))
			Expect(beforeDep.Annotations[utils.PoolerSpecHashAnnotationName]).
				NotTo(Equal(afterDep.Annotations[utils.PoolerSpecHashAnnotationName]))
		})
	})
})

var _ = Describe("ensureServiceAccountPullSecret", func() {
	var (
		r                *PoolerReconciler
		pooler           *apiv1.Pooler
		conf             *configuration.Data
		pullSecret       *corev1.Secret
		poolerSecretName string
	)

	generateOperatorPullSecret := func() *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test",
				Name:      "pull-secret",
			},
			Type: corev1.SecretTypeBasicAuth,
			Data: map[string][]byte{
				corev1.BasicAuthUsernameKey: []byte("some-username"),
				corev1.BasicAuthPasswordKey: []byte("some-password"),
			},
		}
	}

	BeforeEach(func() {
		pullSecret = generateOperatorPullSecret()

		conf = &configuration.Data{
			OperatorNamespace:      pullSecret.Namespace,
			OperatorPullSecretName: pullSecret.Name,
		}

		pooler = &apiv1.Pooler{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pooler",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "some-pooler",
				Namespace: "pooler-namespace",
			},
		}

		poolerSecretName = fmt.Sprintf("%s-pull", pooler.Name)

		knownScheme := schemeBuilder.BuildWithAllKnownScheme()
		r = &PoolerReconciler{
			Client: fake.NewClientBuilder().
				WithScheme(knownScheme).
				WithObjects(pullSecret, pooler).
				Build(),
			Scheme: knownScheme,
		}
	})

	It("should create the pull secret", func(ctx SpecContext) {
		name, err := r.ensureServiceAccountPullSecret(ctx, pooler, conf)
		Expect(err).ToNot(HaveOccurred())
		Expect(name).To(Equal(poolerSecretName))
	})

	It("should not change the pull secret if it matches", func(ctx SpecContext) {
		By("creating the secret before triggering the reconcile")
		secret := generateOperatorPullSecret()
		secret.Name = poolerSecretName
		secret.Namespace = pooler.Namespace
		err := ctrl.SetControllerReference(pooler, secret, r.Scheme)
		Expect(err).ToNot(HaveOccurred())
		err = r.Create(ctx, secret)
		Expect(err).ToNot(HaveOccurred())

		By("fetching the remote secret")
		var remoteSecret corev1.Secret
		err = r.Get(ctx, k8client.ObjectKeyFromObject(secret), &remoteSecret)
		Expect(err).ToNot(HaveOccurred())

		By("triggering the reconciliation")
		name, err := r.ensureServiceAccountPullSecret(ctx, pooler, conf)
		Expect(err).ToNot(HaveOccurred())
		Expect(name).To(Equal(poolerSecretName))

		By("ensuring the resource is the same")
		var remoteSecretAfter corev1.Secret
		err = r.Get(ctx, k8client.ObjectKeyFromObject(secret), &remoteSecretAfter)
		Expect(err).ToNot(HaveOccurred())
		Expect(remoteSecret).To(BeEquivalentTo(remoteSecret))
	})

	It("should reconcile the secret if it doesn't match", func(ctx SpecContext) {
		By("creating the secret before triggering the reconcile")
		secret := generateOperatorPullSecret()
		secret.Name = poolerSecretName
		secret.Namespace = pooler.Namespace
		secret.Data[corev1.BasicAuthUsernameKey] = []byte("bad-name")
		err := ctrl.SetControllerReference(pooler, secret, r.Scheme)
		Expect(err).ToNot(HaveOccurred())
		err = r.Create(ctx, secret)
		Expect(err).ToNot(HaveOccurred())

		By("fetching the remote secret")
		var remoteSecret corev1.Secret
		err = r.Get(ctx, k8client.ObjectKeyFromObject(secret), &remoteSecret)
		Expect(err).ToNot(HaveOccurred())

		By("triggering the reconciliation")
		name, err := r.ensureServiceAccountPullSecret(ctx, pooler, conf)
		Expect(err).ToNot(HaveOccurred())
		Expect(name).To(Equal(poolerSecretName))

		By("ensuring the resource is not the same")
		var remoteSecretAfter corev1.Secret
		err = r.Get(ctx, k8client.ObjectKeyFromObject(secret), &remoteSecretAfter)
		Expect(err).ToNot(HaveOccurred())
		Expect(remoteSecret).ToNot(BeEquivalentTo(remoteSecretAfter))
	})
})
