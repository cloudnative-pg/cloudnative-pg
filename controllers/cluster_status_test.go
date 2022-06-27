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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
)

var _ = Describe("cluster_status unit tests", func() {
	It("should make sure setCertExpiration works correctly", func() {
		var certExpirationDate string

		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace)
		secretName := rand.String(10)

		By("creating the required secret", func() {
			secret, keyPair := generateFakeCASecret(secretName, namespace, "unittest.com")
			Expect(secret.Name).To(Equal(secretName))

			_, expDate, err := keyPair.IsExpiring()
			Expect(err).To(BeNil())

			certExpirationDate = expDate.String()
		})
		By("making sure that sets the status of the secret correctly", func() {
			cluster.Status.Certificates.Expirations = map[string]string{}
			err := clusterReconciler.setCertExpiration(ctx, cluster, secretName, namespace, certs.CACertKey)
			Expect(err).To(BeNil())
			Expect(cluster.Status.Certificates.Expirations[secretName]).To(Equal(certExpirationDate))
		})
	})

	It("makes sure that getPgbouncerIntegrationStatus returns the correct secret name without duplicates", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace)
		pooler1 := *newFakePooler(cluster)
		pooler2 := *newFakePooler(cluster)
		Expect(pooler1.Name).ToNot(Equal(pooler2.Name))
		poolerList := v1.PoolerList{Items: []v1.Pooler{pooler1, pooler2}}

		intStatus, err := clusterReconciler.getPgbouncerIntegrationStatus(ctx, cluster, poolerList)
		Expect(err).To(BeNil())
		Expect(intStatus.Secrets).To(HaveLen(1))
	})

	It("makes sure getObjectResourceVersion returns the correct object version", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace)
		pooler := newFakePooler(cluster)

		version, err := clusterReconciler.getObjectResourceVersion(ctx, cluster, pooler.Name, &v1.Pooler{})
		Expect(err).To(BeNil())
		Expect(version).To(Equal(pooler.ResourceVersion))
	})

	It("makes sure setPrimaryInstance works correctly", func() {
		const podName = "test-pod"
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace)
		Expect(cluster.Status.TargetPrimaryTimestamp).To(BeEmpty())

		By("setting the primaryInstance and making sure the passed object is updated", func() {
			err := clusterReconciler.setPrimaryInstance(ctx, cluster, podName)
			Expect(err).To(BeNil())
			Expect(cluster.Status.TargetPrimaryTimestamp).ToNot(BeEmpty())
			Expect(cluster.Status.TargetPrimary).To(Equal(podName))
		})

		By("making sure the remote resource is updated", func() {
			remoteCluster := &v1.Cluster{}

			err := k8sClient.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, remoteCluster)
			Expect(err).To(BeNil())
			Expect(remoteCluster.Status.TargetPrimaryTimestamp).ToNot(BeEmpty())
			Expect(remoteCluster.Status.TargetPrimary).To(Equal(podName))
		})
	})

	It("makes sure RegisterPhase works correctly", func() {
		const phaseReason = "testing"
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace)

		By("registering the phase and making sure the passed object is updated", func() {
			err := clusterReconciler.RegisterPhase(ctx, cluster, v1.PhaseSwitchover, phaseReason)
			Expect(err).To(BeNil())
			Expect(cluster.Status.Phase).To(Equal(v1.PhaseSwitchover))
			Expect(cluster.Status.PhaseReason).To(Equal(phaseReason))
		})

		By("making sure the remote resource is updated", func() {
			remoteCluster := &v1.Cluster{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, remoteCluster)
			Expect(err).To(BeNil())
			Expect(remoteCluster.Status.Phase).To(Equal(v1.PhaseSwitchover))
			Expect(remoteCluster.Status.PhaseReason).To(Equal(phaseReason))
		})
	})

	It("makes sure that getManagedResources works correctly", func() {
		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace)
		var jobs []batchv1.Job
		var pods []corev1.Pod
		var pvcs []corev1.PersistentVolumeClaim

		withManager(func(ctx context.Context, reconciler *ClusterReconciler, manager manager.Manager) {
			By("creating the required resources", func() {
				jobs = generateFakeInitDBJobs(cluster)
				pods = generateFakeClusterPods(cluster, true)
				pvcs = generateFakePVC(cluster)
				name, isOwned := isOwnedByCluster(&pods[0])
				Expect(isOwned).To(BeTrue())
				Expect(name).To(Equal(cluster.Name))
			})

			assertRefreshManagerCache(ctx, manager)

			By("making sure that the required resources are found", func() {
				Eventually(func() (*managedResources, error) {
					return reconciler.getManagedResources(ctx, cluster)
				}).Should(Satisfy(func(mr *managedResources) bool {
					return len(mr.pods.Items) == len(pods) &&
						len(mr.jobs.Items) == len(jobs) &&
						len(mr.pvcs.Items) == len(pvcs)
				}))
			})
		})
	})
})
