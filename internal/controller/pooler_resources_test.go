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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs/pgbouncer"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pooler_resources unit tests", func() {
	var env *testingEnvironment
	BeforeEach(func() {
		env = buildTestEnvironment()
	})

	assertResourceIsCorrect := func(expected metav1.ObjectMeta, result metav1.ObjectMeta, err error) {
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())
		Expect(result.Name).To(Equal(expected.Name))
		Expect(result.Namespace).To(Equal(expected.Namespace))
	}

	It("should correctly fetch the deployment when it exists", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		objectKey := client.ObjectKey{Namespace: pooler.Namespace, Name: pooler.Name}

		By("making sure that returns nil if the deployment and/or service doesn't exist", func() {
			deployment, err := getDeploymentOrNil(ctx, env.poolerReconciler.Client, objectKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(deployment).To(BeNil())

			service, err := getServiceOrNil(ctx, env.poolerReconciler.Client, objectKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(service).To(BeNil())
		})

		By("creating the deployment and service", func() {
			deployment, err := pgbouncer.Deployment(pooler, cluster)
			Expect(err).ToNot(HaveOccurred())
			err = env.poolerReconciler.Create(ctx, deployment)
			Expect(err).ToNot(HaveOccurred())

			service, err := pgbouncer.Service(pooler, cluster)
			Expect(err).ToNot(HaveOccurred())
			err = env.poolerReconciler.Create(ctx, service)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure it returns the created deployment and service", func() {
			deployment, err := getDeploymentOrNil(ctx, env.poolerReconciler.Client, objectKey)
			Expect(err).ToNot(HaveOccurred())
			assertResourceIsCorrect(pooler.ObjectMeta, deployment.ObjectMeta, err)

			service, err := getServiceOrNil(ctx, env.poolerReconciler.Client, objectKey)
			Expect(err).ToNot(HaveOccurred())
			assertResourceIsCorrect(pooler.ObjectMeta, service.ObjectMeta, err)
		})
	})

	It("should correctly fetch the cluster when it exists", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		objectKey := client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}

		By("making sure it returns the cluster object", func() {
			result, err := getClusterOrNil(ctx, env.poolerReconciler.Client, objectKey)
			assertResourceIsCorrect(cluster.ObjectMeta, result.ObjectMeta, err)
		})
	})

	It("should correctly fetch the secret when it exists", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		objectKey := client.ObjectKey{Name: pooler.GetAuthQuerySecretName(), Namespace: pooler.Namespace}

		By("making sure that returns nil if the secret doesn't exist", func() {
			result, err := getSecretOrNil(ctx, env.poolerReconciler.Client, objectKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeNil())
		})

		By("creating the secret", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: pooler.GetAuthQuerySecretName(), Namespace: pooler.Namespace},
			}
			err := env.poolerReconciler.Create(ctx, secret)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure it returns the created secret", func() {
			result, err := getSecretOrNil(ctx, env.poolerReconciler.Client, objectKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.Name).To(Equal(pooler.GetAuthQuerySecretName()))
			Expect(result.Namespace).To(Equal(pooler.Namespace))
		})
	})

	It("should correctly fetch the service when it exists", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		objectKey := client.ObjectKey{Namespace: pooler.Namespace, Name: pooler.Name}

		By("making sure that returns nil if the service doesn't exist", func() {
			result, err := getServiceOrNil(ctx, env.poolerReconciler.Client, objectKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeNil())
		})

		By("creating the service", func() {
			service, err := pgbouncer.Service(pooler, cluster)
			Expect(err).ToNot(HaveOccurred())
			err = env.poolerReconciler.Create(ctx, service)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure it returns the created service", func() {
			result, err := getServiceOrNil(ctx, env.poolerReconciler.Client, objectKey)
			assertResourceIsCorrect(pooler.ObjectMeta, result.ObjectMeta, err)
		})
	})

	// nolint: dupl
	It("should correctly fetch the role when it exists", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		objectKey := client.ObjectKey{Namespace: pooler.Namespace, Name: pooler.Name}

		By("making sure that returns nil if the role doesn't exist", func() {
			result, err := getRoleOrNil(ctx, env.poolerReconciler.Client, objectKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeNil())
		})

		By("creating the role", func() {
			role := pgbouncer.Role(pooler)
			err := env.poolerReconciler.Create(ctx, role)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure it returns the created role", func() {
			result, err := getRoleOrNil(ctx, env.poolerReconciler.Client, objectKey)
			Expect(err).ToNot(HaveOccurred())
			assertResourceIsCorrect(pooler.ObjectMeta, result.ObjectMeta, err)
		})
	})

	It("should correctly fetch the roleBinding when it exists", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		objectKey := client.ObjectKey{Namespace: pooler.Namespace, Name: pooler.Name}

		By("making sure that returns nil if the roleBinding doesn't exist", func() {
			result, err := getRoleBindingOrNil(ctx, env.poolerReconciler.Client, objectKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeNil())
		})

		By("creating the roleBinding", func() {
			roleBinding := pgbouncer.RoleBinding(pooler, pooler.GetServiceAccountName())
			err := env.poolerReconciler.Create(ctx, &roleBinding)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure it returns the created roleBinding", func() {
			result, err := getRoleBindingOrNil(ctx, env.poolerReconciler.Client, objectKey)
			Expect(err).ToNot(HaveOccurred())
			assertResourceIsCorrect(pooler.ObjectMeta, result.ObjectMeta, err)
		})
	})

	// nolint: dupl
	It("should correctly fetch the SA when it exists", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		objectKey := client.ObjectKey{Namespace: pooler.Namespace, Name: pooler.Name}

		By("making sure that returns nil if the SA doesn't exist", func() {
			result, err := getServiceAccountOrNil(ctx, env.poolerReconciler.Client, objectKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeNil())
		})

		By("creating the SA", func() {
			serviceAccount := pgbouncer.ServiceAccount(pooler)
			err := env.poolerReconciler.Create(ctx, serviceAccount)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure it returns the created SA", func() {
			result, err := getServiceAccountOrNil(ctx, env.poolerReconciler.Client, objectKey)
			Expect(err).ToNot(HaveOccurred())
			assertResourceIsCorrect(pooler.ObjectMeta, result.ObjectMeta, err)
		})
	})
})
