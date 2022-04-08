/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs/pgbouncer"
)

var _ = Describe("pooler_status unit tests", func() {
	assertClusterInheritedStatus := func(pooler *v1.Pooler, cluster *v1.Cluster) {
		Expect(pooler.Status.Secrets.ServerCA.Name).To(Equal(cluster.GetServerCASecretName()))
		Expect(pooler.Status.Secrets.ServerTLS.Name).To(Equal(cluster.GetServerTLSSecretName()))
		Expect(pooler.Status.Secrets.ClientCA.Name).To(Equal(cluster.GetClientCASecretName()))
	}
	assertAuthUserStatus := func(pooler *v1.Pooler, authUserSecret *corev1.Secret) {
		Expect(pooler.Status.Secrets.PgBouncerSecrets.AuthQuery.Name).To(Equal(authUserSecret.Name))
		Expect(pooler.Status.Secrets.PgBouncerSecrets.AuthQuery.Version).To(Equal(authUserSecret.ResourceVersion))
	}

	It("should correctly deduce the status inherited from the cluster resource", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)
		res := &poolerManagedResources{Deployment: nil, Cluster: cluster}

		err := poolerReconciler.updatePoolerStatus(ctx, pooler, res)
		Expect(err).To(BeNil())
		assertClusterInheritedStatus(pooler, cluster)
	})

	It("should correctly set the status for authUserSecret", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)
		authUserSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:            pooler.GetAuthQuerySecretName(),
				Namespace:       pooler.Namespace,
				ResourceVersion: "1",
			},
		}
		res := &poolerManagedResources{AuthUserSecret: authUserSecret, Cluster: cluster}

		err := poolerReconciler.updatePoolerStatus(ctx, pooler, res)
		Expect(err).To(BeNil())
		assertAuthUserStatus(pooler, authUserSecret)
	})

	It("should correctly set the deployment status", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)
		dep, err := pgbouncer.Deployment(pooler, cluster)
		dep.Status.Replicas = *dep.Spec.Replicas
		Expect(err).To(BeNil())
		res := &poolerManagedResources{Deployment: dep, Cluster: cluster}

		err = poolerReconciler.updatePoolerStatus(ctx, pooler, res)
		Expect(err).To(BeNil())
		Expect(pooler.Status.Instances).To(Equal(dep.Status.Replicas))
	})

	It("should correctly interact with the api server", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)
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
		Expect(err).To(BeNil())
		res := &poolerManagedResources{AuthUserSecret: authUserSecret, Cluster: cluster, Deployment: dep}

		By("making sure it updates the remote stored status when there are changes", func() {
			poolerBefore := &v1.Pooler{}
			err := k8sClient.Get(ctx, poolerQuery, poolerBefore)
			Expect(err).To(BeNil())

			err = poolerReconciler.updatePoolerStatus(ctx, pooler, res)
			Expect(err).To(BeNil())

			poolerAfter := &v1.Pooler{}
			err = k8sClient.Get(ctx, poolerQuery, poolerAfter)
			Expect(err).To(BeNil())

			Expect(poolerAfter.ResourceVersion).ToNot(Equal(poolerBefore.ResourceVersion))
			Expect(pooler.Status.Instances).To(Equal(dep.Status.Replicas))
			assertAuthUserStatus(pooler, authUserSecret)
			assertClusterInheritedStatus(pooler, cluster)
		})

		By("making sure it doesn't update the remote stored status when there aren't changes", func() {
			poolerBefore := &v1.Pooler{}
			err := k8sClient.Get(ctx, poolerQuery, poolerBefore)
			Expect(err).To(BeNil())

			err = poolerReconciler.updatePoolerStatus(ctx, pooler, res)
			Expect(err).To(BeNil())

			poolerAfter := &v1.Pooler{}
			err = k8sClient.Get(ctx, poolerQuery, poolerAfter)
			Expect(err).To(BeNil())

			Expect(poolerAfter.ResourceVersion).To(Equal(poolerBefore.ResourceVersion))
		})
	})
})
