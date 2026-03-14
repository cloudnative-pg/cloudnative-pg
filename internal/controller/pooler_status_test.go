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
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs/pgbouncer"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pooler_status unit tests", func() {
	var env *testingEnvironment
	BeforeEach(func() {
		env = buildTestEnvironment()
	})

	assertClusterInheritedStatus := func(pooler *apiv1.Pooler, cluster *apiv1.Cluster) {
		Expect(pooler.Status.Secrets.ServerCA.Name).To(Equal(cluster.GetServerCASecretName()))
		Expect(pooler.Status.Secrets.ServerTLS.Name).To(BeEmpty())
		Expect(pooler.Status.Secrets.ClientCA.Name).To(Equal(cluster.GetClientCASecretName()))

		// Yes, this may be confusing, but pgbouncer calls "client" its listening side, and
		// we just use the same PostgreSQL certificates for that.
		Expect(pooler.Status.Secrets.ClientTLS.Name).To(Equal(cluster.GetServerTLSSecretName()))
	}
	assertAuthUserStatus := func(pooler *apiv1.Pooler, authUserSecret *corev1.Secret) {
		Expect(pooler.Status.Secrets.PgBouncerSecrets.AuthQuery.Name).To(Equal(authUserSecret.Name))
		Expect(pooler.Status.Secrets.PgBouncerSecrets.AuthQuery.Version).To(Equal(authUserSecret.ResourceVersion))
	}
	assertLoadBalancerStatus := func(pooler *v1.Pooler, service *corev1.Service) {
		Expect(pooler.Status.LoadBalancer.Ingress).To(HaveLen(1))
		Expect(pooler.Status.LoadBalancer.Ingress[0].IP).To(Equal(service.Status.LoadBalancer.Ingress[0].IP))
	}

	It("should correctly deduce the status inherited from the cluster resource", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		res := &poolerManagedResources{
			Deployment: nil,
			Cluster:    cluster,
			ClientCASecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: pooler.GetClientCASecretNameOrDefault(cluster),
				},
			},
			ClientTLSSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: pooler.GetClientTLSSecretNameOrDefault(cluster),
				},
			},
			ServerCASecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: pooler.GetServerCASecretNameOrDefault(cluster),
				},
			},
		}

		err := env.poolerReconciler.updatePoolerStatus(ctx, pooler, res)
		Expect(err).ToNot(HaveOccurred())
		assertClusterInheritedStatus(pooler, cluster)
	})

	It("should correctly set the status for authUserSecret", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		authUserSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:            pooler.GetAuthQuerySecretName(),
				Namespace:       pooler.Namespace,
				ResourceVersion: "1",
			},
		}
		res := &poolerManagedResources{AuthUserSecret: authUserSecret, Cluster: cluster}

		err := env.poolerReconciler.updatePoolerStatus(ctx, pooler, res)
		Expect(err).ToNot(HaveOccurred())
		assertAuthUserStatus(pooler, authUserSecret)
	})

	It("should correctly set the deployment status", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		dep, err := pgbouncer.Deployment(pooler, cluster)
		dep.Status.Replicas = *dep.Spec.Replicas
		Expect(err).ToNot(HaveOccurred())
		res := &poolerManagedResources{Deployment: dep, Cluster: cluster}

		err = env.poolerReconciler.updatePoolerStatus(ctx, pooler, res)
		Expect(err).ToNot(HaveOccurred())
		Expect(pooler.Status.Instances).To(Equal(dep.Status.Replicas))
	})

	It("should correctly set the load balancer status", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		serviceTemplate := &v1.ServiceTemplateSpec{
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
			},
		}
		pooler.Spec.ServiceTemplate = serviceTemplate
		svc, err := pgbouncer.Service(pooler, cluster)
		Expect(err).ToNot(HaveOccurred())
		res := &poolerManagedResources{Service: svc, Cluster: cluster}
		svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
			{
				IP: "1.2.3.4",
			},
		}
		err = env.poolerReconciler.updatePoolerStatus(ctx, pooler, res)
		Expect(err).ToNot(HaveOccurred())
		assertLoadBalancerStatus(pooler, svc)
	})

	It("should correctly interact with the api server", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		poolerQuery := types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace}
		authUserSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:            pooler.GetAuthQuerySecretName(),
				Namespace:       pooler.Namespace,
				ResourceVersion: "1",
			},
		}
		dep, err := pgbouncer.Deployment(pooler, cluster)
		dep.Status.Replicas = *dep.Spec.Replicas
		Expect(err).ToNot(HaveOccurred())
		res := &poolerManagedResources{
			AuthUserSecret: authUserSecret,
			Cluster:        cluster,
			Deployment:     dep,
			ClientCASecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: pooler.GetClientCASecretNameOrDefault(cluster),
				},
			},
			ClientTLSSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: pooler.GetClientTLSSecretNameOrDefault(cluster),
				},
			},
			ServerCASecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: pooler.GetServerCASecretNameOrDefault(cluster),
				},
			},
		}

		By("making sure it updates the remote stored status when there are changes", func() {
			poolerBefore := &apiv1.Pooler{}
			err := env.client.Get(ctx, poolerQuery, poolerBefore)
			Expect(err).ToNot(HaveOccurred())

			err = env.poolerReconciler.updatePoolerStatus(ctx, pooler, res)
			Expect(err).ToNot(HaveOccurred())

			poolerAfter := &apiv1.Pooler{}
			err = env.client.Get(ctx, poolerQuery, poolerAfter)
			Expect(err).ToNot(HaveOccurred())

			Expect(poolerAfter.ResourceVersion).ToNot(Equal(poolerBefore.ResourceVersion))
			Expect(pooler.Status.Instances).To(Equal(dep.Status.Replicas))
			assertAuthUserStatus(pooler, authUserSecret)
			assertClusterInheritedStatus(pooler, cluster)
		})

		By("making sure it doesn't update the remote stored status when there aren't changes", func() {
			poolerBefore := &apiv1.Pooler{}
			err := env.client.Get(ctx, poolerQuery, poolerBefore)
			Expect(err).ToNot(HaveOccurred())

			err = env.poolerReconciler.updatePoolerStatus(ctx, pooler, res)
			Expect(err).ToNot(HaveOccurred())

			poolerAfter := &apiv1.Pooler{}
			err = env.client.Get(ctx, poolerQuery, poolerAfter)
			Expect(err).ToNot(HaveOccurred())
			Expect(poolerBefore.Status).To(BeEquivalentTo(poolerAfter.Status))
		})
	})

	It("should clear ServerTLS status when not using manual TLS authentication (migration to v1.28)", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)

		// Simulate a pre v1.28 Pooler with ServerTLS populated
		pooler.Status.Secrets = &apiv1.PoolerSecrets{
			ServerTLS: apiv1.SecretVersion{
				Name:    cluster.GetServerTLSSecretName(),
				Version: "stale-version-from-v127",
			},
		}

		res := &poolerManagedResources{
			Deployment: nil,
			Cluster:    cluster,
			ClientCASecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: pooler.GetClientCASecretNameOrDefault(cluster),
				},
			},
			ClientTLSSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: pooler.GetClientTLSSecretNameOrDefault(cluster),
				},
			},
			ServerCASecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: pooler.GetServerCASecretNameOrDefault(cluster),
				},
			},
			ServerTLSSecret: nil,
		}

		err := env.poolerReconciler.updatePoolerStatus(ctx, pooler, res)
		Expect(err).ToNot(HaveOccurred())

		Expect(pooler.Status.Secrets.ServerTLS.Name).To(BeEmpty())
		Expect(pooler.Status.Secrets.ServerTLS.Version).To(BeEmpty())
		assertClusterInheritedStatus(pooler, cluster)
	})
})
