/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs/pgbouncer"
)

var _ = Describe("pooler_resources unit tests", func() {
	assertResourceIsCorrect := func(expected metav1.ObjectMeta, result metav1.ObjectMeta, err error) {
		Expect(err).To(BeNil())
		Expect(result).ToNot(BeNil())
		Expect(result.Name).To(Equal(expected.Name))
		Expect(result.Namespace).To(Equal(expected.Namespace))
	}

	It("should correctly fetch the deployment when it exists", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)
		objectKey := client.ObjectKey{Namespace: pooler.Namespace, Name: pooler.Name}

		By("making sure that returns nil if the deployment doesn't exist", func() {
			result, err := getDeploymentOrNil(ctx, poolerReconciler.Client, objectKey)
			Expect(err).To(BeNil())
			Expect(result).To(BeNil())
		})

		By("creating the deployment", func() {
			dep, err := pgbouncer.Deployment(pooler, cluster)
			Expect(err).To(BeNil())

			err = poolerReconciler.Create(ctx, dep)
			Expect(err).To(BeNil())
		})

		By("making sure it returns the created deployment", func() {
			result, err := getDeploymentOrNil(ctx, poolerReconciler.Client, objectKey)
			assertResourceIsCorrect(pooler.ObjectMeta, result.ObjectMeta, err)
		})
	})

	It("should correctly fetch the cluster when it exists", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		objectKey := client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}

		By("making sure it returns the cluster object", func() {
			result, err := getClusterOrNil(ctx, poolerReconciler.Client, objectKey)
			assertResourceIsCorrect(cluster.ObjectMeta, result.ObjectMeta, err)
		})
	})

	It("should correctly fetch the secret when it exists", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)
		objectKey := client.ObjectKey{Name: pooler.GetAuthQuerySecretName(), Namespace: pooler.Namespace}

		By("making sure that returns nil if the secret doesn't exist", func() {
			result, err := getSecretOrNil(ctx, poolerReconciler.Client, objectKey)
			Expect(err).To(BeNil())
			Expect(result).To(BeNil())
		})

		By("creating the secret", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: pooler.GetAuthQuerySecretName(), Namespace: pooler.Namespace},
			}
			err := poolerReconciler.Create(ctx, secret)
			Expect(err).To(BeNil())
		})

		By("making sure it returns the created secret", func() {
			result, err := getSecretOrNil(ctx, poolerReconciler.Client, objectKey)
			Expect(err).To(BeNil())
			Expect(result).ToNot(BeNil())
			Expect(result.Name).To(Equal(pooler.GetAuthQuerySecretName()))
			Expect(result.Namespace).To(Equal(pooler.Namespace))
		})
	})

	It("should correctly fetch the service when it exists", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)
		objectKey := client.ObjectKey{Namespace: pooler.Namespace, Name: pooler.Name}

		By("making sure that returns nil if the service doesn't exist", func() {
			result, err := getServiceOrNil(ctx, poolerReconciler.Client, objectKey)
			Expect(err).To(BeNil())
			Expect(result).To(BeNil())
		})

		By("creating the service", func() {
			service := pgbouncer.Service(pooler)
			err := poolerReconciler.Create(ctx, service)
			Expect(err).To(BeNil())
		})

		By("making sure it returns the created service", func() {
			result, err := getServiceOrNil(ctx, poolerReconciler.Client, objectKey)
			assertResourceIsCorrect(pooler.ObjectMeta, result.ObjectMeta, err)
		})
	})

	It("should correctly fetch the role when it exists", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)
		objectKey := client.ObjectKey{Namespace: pooler.Namespace, Name: pooler.Name}

		By("making sure that returns nil if the role doesn't exist", func() {
			result, err := getRoleOrNil(ctx, poolerReconciler.Client, objectKey)
			Expect(err).To(BeNil())
			Expect(result).To(BeNil())
		})

		By("creating the role", func() {
			role := pgbouncer.Role(pooler)
			err := poolerReconciler.Create(ctx, role)
			Expect(err).To(BeNil())
		})

		By("making sure it returns the created role", func() {
			result, err := getRoleOrNil(ctx, poolerReconciler.Client, objectKey)
			assertResourceIsCorrect(pooler.ObjectMeta, result.ObjectMeta, err)
		})
	})

	It("should correctly fetch the roleBinding when it exists", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)
		objectKey := client.ObjectKey{Namespace: pooler.Namespace, Name: pooler.Name}

		By("making sure that returns nil if the roleBinding doesn't exist", func() {
			result, err := getRoleBindingOrNil(ctx, poolerReconciler.Client, objectKey)
			Expect(err).To(BeNil())
			Expect(result).To(BeNil())
		})

		By("creating the roleBinding", func() {
			roleBinding := pgbouncer.RoleBinding(pooler)
			err := poolerReconciler.Create(ctx, &roleBinding)
			Expect(err).To(BeNil())
		})

		By("making sure it returns the created roleBinding", func() {
			result, err := getRoleBindingOrNil(ctx, poolerReconciler.Client, objectKey)
			assertResourceIsCorrect(pooler.ObjectMeta, result.ObjectMeta, err)
		})
	})

	It("should correctly fetch the SA when it exists", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)
		objectKey := client.ObjectKey{Namespace: pooler.Namespace, Name: pooler.Name}

		By("making sure that returns nil if the SA doesn't exist", func() {
			result, err := getServiceAccountOrNil(ctx, poolerReconciler.Client, objectKey)
			Expect(err).To(BeNil())
			Expect(result).To(BeNil())
		})

		By("creating the SA", func() {
			serviceAccount := pgbouncer.ServiceAccount(pooler)
			err := poolerReconciler.Create(ctx, serviceAccount)
			Expect(err).To(BeNil())
		})

		By("making sure it returns the created SA", func() {
			result, err := getServiceAccountOrNil(ctx, poolerReconciler.Client, objectKey)
			assertResourceIsCorrect(pooler.ObjectMeta, result.ObjectMeta, err)
		})
	})
})
