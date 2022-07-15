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

	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("cluster_create unit tests", func() {
	It("should make sure that reconcilePostgresSecrets works correctly", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace)
		pooler := newFakePooler(cluster)
		poolerSecretName := pooler.Name
		cluster.Status.PoolerIntegrations = &apiv1.PoolerIntegrations{
			PgBouncerIntegration: apiv1.PgBouncerIntegrationStatus{
				Secrets: []string{poolerSecretName},
			},
		}

		By("creating prerequisites", func() {
			generateFakeCASecretWithDefaultClient(cluster.GetClientCASecretName(), namespace, "testdomain.com")
		})

		By("executing reconcilePostgresSecrets", func() {
			err := clusterReconciler.reconcilePostgresSecrets(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure that the superUser secret have been created", func() {
			superUser := corev1.Secret{}
			err := k8sClient.Get(
				ctx,
				types.NamespacedName{Name: cluster.GetSuperuserSecretName(), Namespace: namespace},
				&superUser,
			)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure that the appUserSecret have been created", func() {
			appUser := corev1.Secret{}
			err := k8sClient.Get(
				ctx,
				types.NamespacedName{Name: cluster.GetApplicationSecretName(), Namespace: namespace},
				&appUser,
			)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure that the pooler secrets have been created", func() {
			poolerSecret := corev1.Secret{}
			err := k8sClient.Get(
				ctx,
				types.NamespacedName{Name: poolerSecretName, Namespace: namespace},
				&poolerSecret,
			)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	It("should make sure that createPostgresServices works correctly", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace)

		By("executing createPostgresServices", func() {
			err := clusterReconciler.createPostgresServices(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure that the services have been created", func() {
			expectResourceExistsWithDefaultClient(cluster.GetServiceAnyName(), namespace, &corev1.Service{})
			expectResourceExistsWithDefaultClient(cluster.GetServiceReadOnlyName(), namespace, &corev1.Service{})
			expectResourceExistsWithDefaultClient(cluster.GetServiceReadWriteName(), namespace, &corev1.Service{})
			expectResourceExistsWithDefaultClient(cluster.GetServiceReadName(), namespace, &corev1.Service{})
		})
	})

	It("should make sure that createOrPatchServiceAccount works correctly", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace)

		By("executing createOrPatchServiceAccount (create)", func() {
			err := clusterReconciler.createOrPatchServiceAccount(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		sa := &corev1.ServiceAccount{}

		By("making sure that the serviceaccount has been created", func() {
			expectResourceExistsWithDefaultClient(cluster.Name, namespace, sa)
		})

		By("adding an annotation, a label and an image pull secret to the service account", func() {
			sa.Annotations["test"] = "annotation"
			sa.Labels["test"] = "label"
			sa.ImagePullSecrets = append(sa.ImagePullSecrets, corev1.LocalObjectReference{
				Name: "sa-pullsecret",
			})
			err := k8sClient.Update(context.Background(), sa)
			Expect(err).ToNot(HaveOccurred())
		})

		By("executing createOrPatchServiceAccount (no-patch)", func() {
			err := clusterReconciler.createOrPatchServiceAccount(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure that the serviceaccount is untouched because there is no change in the cluster", func() {
			updatedSa := &corev1.ServiceAccount{}
			expectResourceExistsWithDefaultClient(cluster.Name, namespace, updatedSa)
			Expect(updatedSa).To(BeEquivalentTo(sa))
		})

		By("adding an image pull secret to the cluster to trigger a service account update", func() {
			cluster.Spec.ImagePullSecrets = append(cluster.Spec.ImagePullSecrets, apiv1.LocalObjectReference{
				Name: "cluster-pullsecret",
			})
			err := k8sClient.Update(context.Background(), cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		By("executing createOrPatchServiceAccount (patch)", func() {
			err := clusterReconciler.createOrPatchServiceAccount(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure that the serviceaccount is patched correctly", func() {
			updatedSA := &corev1.ServiceAccount{}
			expectResourceExistsWithDefaultClient(cluster.Name, namespace, updatedSA)
			Expect(updatedSA.Annotations["test"]).To(BeEquivalentTo("annotation"))
			Expect(updatedSA.Labels["test"]).To(BeEquivalentTo("label"))
			Expect(updatedSA.ImagePullSecrets).To(ContainElements(corev1.LocalObjectReference{
				Name: "cluster-pullsecret",
			}))
			Expect(updatedSA.ImagePullSecrets).To(ContainElements(corev1.LocalObjectReference{
				Name: "sa-pullsecret",
			}))
		})
	})

	It("should make sure that reconcilePodDisruptionBudget works correctly", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace)
		pdbReplicaName := specs.BuildReplicasPodDisruptionBudget(cluster).Name
		pdbPrimaryName := specs.BuildPrimaryPodDisruptionBudget(cluster).Name
		reconcilePDB := func() {
			err := clusterReconciler.reconcilePodDisruptionBudget(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		}

		By("creating the primary and replica PDB in a standard case scenario", func() {
			reconcilePDB()
		})

		By("making sure PDB exists", func() {
			expectResourceExistsWithDefaultClient(
				pdbPrimaryName,
				namespace,
				&policyv1beta1.PodDisruptionBudget{},
			)
			expectResourceExistsWithDefaultClient(
				pdbReplicaName,
				namespace,
				&policyv1beta1.PodDisruptionBudget{},
			)
		})

		By("enabling the cluster maintenance mode", func() {
			reusePVC := true
			cluster.Spec.NodeMaintenanceWindow = &apiv1.NodeMaintenanceWindow{
				InProgress: true,
				ReusePVC:   &reusePVC,
			}
		})

		By("reconciling pdb during the maintenance mode", func() {
			reconcilePDB()
		})

		By("making sure that the replicas PDB are deleted", func() {
			expectResourceDoesntExistWithDefaultClient(
				pdbReplicaName,
				namespace,
				&policyv1beta1.PodDisruptionBudget{},
			)
		})

		By("scaling the instances to 1 during maintenance mode", func() {
			cluster.Spec.Instances = 1
			cluster.Status.Instances = 1
		})

		By("reconciling pdb during the maintenance mode with a single node", func() {
			reconcilePDB()
		})

		By("making sure that both the replicas and main PDB are deleted", func() {
			expectResourceDoesntExistWithDefaultClient(
				pdbPrimaryName,
				namespace,
				&policyv1beta1.PodDisruptionBudget{},
			)
			expectResourceDoesntExistWithDefaultClient(
				pdbReplicaName,
				namespace,
				&policyv1beta1.PodDisruptionBudget{},
			)
		})
	})
})
