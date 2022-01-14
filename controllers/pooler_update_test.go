/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs/pgbouncer"
)

var _ = Describe("unit test of pooler_update reconciliation logic", func() {
	AfterEach(func() {
		configuration.Current = configuration.NewConfiguration()
	})

	BeforeEach(func() {
		configuration.Current = configuration.NewConfiguration()
	})

	It("it should test the deployment update logic", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)
		res := &poolerManagedResources{Deployment: nil, Cluster: cluster}

		By("making sure that the deployment doesn't already exists", func() {
			deployment := &appsv1.Deployment{}
			err := k8sClient.Get(
				ctx,
				types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace},
				deployment,
			)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		By("making sure that updateDeployment creates the deployment", func() {
			err := poolerReconciler.updateDeployment(ctx, pooler, res)
			Expect(err).To(BeNil())

			deployment := getPoolerDeployment(ctx, pooler)

			Expect(*deployment.Spec.Replicas).To(Equal(pooler.Spec.Instances))
		})

		By("making sure that if the pooler.spec doesn't change the deployment isn't updated", func() {
			beforeDep := getPoolerDeployment(ctx, pooler)

			err := poolerReconciler.updateDeployment(ctx, pooler, res)
			Expect(err).To(BeNil())

			afterDep := getPoolerDeployment(ctx, pooler)

			Expect(beforeDep.ResourceVersion).To(Equal(afterDep.ResourceVersion))
			Expect(beforeDep.Annotations[pgbouncer.PgbouncerPoolerSpecHash]).
				To(Equal(afterDep.Annotations[pgbouncer.PgbouncerPoolerSpecHash]))
		})

		By("making sure that the deployments gets updated if the pooler.spec changes", func() {
			const instancesNumber int32 = 3
			poolerUpdate := pooler.DeepCopy()
			poolerUpdate.Spec.Instances = instancesNumber

			beforeDep := getPoolerDeployment(ctx, poolerUpdate)

			err := poolerReconciler.updateDeployment(ctx, poolerUpdate, res)
			Expect(err).To(BeNil())

			afterDep := getPoolerDeployment(ctx, poolerUpdate)

			Expect(beforeDep.ResourceVersion).ToNot(Equal(afterDep.ResourceVersion))
			Expect(beforeDep.Annotations[pgbouncer.PgbouncerPoolerSpecHash]).
				ToNot(Equal(afterDep.Annotations[pgbouncer.PgbouncerPoolerSpecHash]))
			Expect(*afterDep.Spec.Replicas).To(Equal(instancesNumber))
		})
	})

	It("should test the ServiceAccount and RBAC update logic", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)
		res := &poolerManagedResources{Cluster: cluster, ServiceAccount: nil}

		By("making sure the serviceAccount doesn't already exist", func() {
			sa := &corev1.ServiceAccount{}

			err := k8sClient.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace}, sa)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		By("making sure that updateServiceAccount function creates the SA", func() {
			err := poolerReconciler.updateServiceAccount(ctx, pooler, res)
			Expect(err).To(BeNil())
			sa := &corev1.ServiceAccount{}

			err = k8sClient.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace}, sa)
			Expect(err).To(BeNil())

			Expect(sa.ImagePullSecrets).To(BeEmpty())

			res.ServiceAccount = sa
		})

		By("making sure that SA isn't updated if we don't change anything", func() {
			// the managedResources object is mutated, so we need to store the information
			beforeResourceVersion := res.ServiceAccount.ResourceVersion

			err := poolerReconciler.updateServiceAccount(ctx, pooler, res)
			Expect(err).To(BeNil())

			afterSa := &corev1.ServiceAccount{}

			err = k8sClient.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace}, afterSa)
			Expect(err).To(BeNil())

			Expect(beforeResourceVersion).To(Equal(afterSa.ResourceVersion))
		})

		By("creating the requirement for the imagePullSecret", func() {
			namespace := newFakeNamespace()

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

			err := k8sClient.Create(ctx, pullSecret)
			Expect(err).To(BeNil())
		})

		By("making sure it updates the SA if there are changes", func() {
			// the managedResources object is mutated, so we need to store the information
			beforeResourceVersion := res.ServiceAccount.ResourceVersion

			err := poolerReconciler.updateServiceAccount(ctx, pooler, res)
			Expect(err).To(BeNil())

			afterSa := &corev1.ServiceAccount{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace}, afterSa)
			Expect(err).To(BeNil())

			Expect(afterSa.ImagePullSecrets).To(HaveLen(1))
			Expect(afterSa.ImagePullSecrets).To(ContainElement(corev1.LocalObjectReference{
				Name: pooler.Name + "-pull",
			}))
			Expect(beforeResourceVersion).ToNot(Equal(afterSa.ResourceVersion))
		})

		By("making sure RBAC doesn't exist", func() {
			role := &rbacv1.Role{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace}, role)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			roleBinding := &rbacv1.RoleBinding{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace}, roleBinding)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		By("making sure that updateRBAC function creates the RBAC", func() {
			err := poolerReconciler.updateRBAC(ctx, pooler, res)
			Expect(err).To(BeNil())

			expectedRole := pgbouncer.Role(pooler)
			role := &rbacv1.Role{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: expectedRole.Name, Namespace: expectedRole.Namespace}, role)
			Expect(err).To(BeNil())

			Expect(expectedRole.Rules).To(Equal(role.Rules))

			expectedRb := pgbouncer.RoleBinding(pooler)
			roleBinding := &rbacv1.RoleBinding{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: expectedRb.Name, Namespace: expectedRb.Namespace}, roleBinding)
			Expect(err).To(BeNil())

			Expect(expectedRb.Subjects).To(Equal(roleBinding.Subjects))
			Expect(expectedRb.RoleRef).To(Equal(roleBinding.RoleRef))
		})
	})

	It("should test the Service update logic", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)
		// k8s.machinery.rand doesn't always produce compatible names for SVC, we make sure the pooler has a proper name
		pooler.Name = "friendlyname" + pooler.Name
		res := &poolerManagedResources{Cluster: cluster}

		By("making sure the service doesn't exist", func() {
			svc := &corev1.Service{}
			expectedSVC := pgbouncer.Service(pooler)
			err := k8sClient.Get(ctx, types.NamespacedName{Name: expectedSVC.Name, Namespace: expectedSVC.Namespace}, svc)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		By("making sure it updateService creates the service", func() {
			err := poolerReconciler.updateService(ctx, pooler, res)
			Expect(err).To(BeNil())

			svc := &corev1.Service{}
			expectedSVC := pgbouncer.Service(pooler)
			err = k8sClient.Get(ctx, types.NamespacedName{Name: expectedSVC.Name, Namespace: expectedSVC.Namespace}, svc)
			Expect(err).To(BeNil())

			Expect(expectedSVC.Spec.Selector).To(Equal(svc.Spec.Selector))
			Expect(expectedSVC.Spec.Ports).To(Equal(svc.Spec.Ports))
			Expect(expectedSVC.Spec.Type).To(Equal(svc.Spec.Type))
			res.Service = svc
		})

		By("making sure the svc doesn't get updated if there are not changes", func() {
			previousResourceVersion := res.Service.ResourceVersion
			err := poolerReconciler.updateService(ctx, pooler, res)
			Expect(err).To(BeNil())

			svc := &corev1.Service{}
			expectedSVC := pgbouncer.Service(pooler)
			err = k8sClient.Get(ctx, types.NamespacedName{Name: expectedSVC.Name, Namespace: expectedSVC.Namespace}, svc)
			Expect(err).To(BeNil())

			Expect(previousResourceVersion).To(Equal(svc.ResourceVersion))
		})
	})
})
