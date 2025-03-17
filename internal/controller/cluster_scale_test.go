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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("scale down", func() {
	var env *testingEnvironment
	BeforeEach(func() {
		env = buildTestEnvironment()
	})

	When("there's no separate WAL storage", func() {
		It("delete the PGDATA PVC", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace)

			resources := &managedResources{
				pvcs: corev1.PersistentVolumeClaimList{
					Items: generateClusterPVC(env.client, cluster, persistentvolumeclaim.StatusReady),
				},
				jobs: batchv1.JobList{Items: generateFakeInitDBJobsWithDefaultClient(env.client, cluster)},
				instances: corev1.PodList{
					Items: generateFakeClusterPodsWithDefaultClient(env.client, cluster, true),
				},
			}

			instanceName := findDeletableInstance(cluster, resources.instances.Items)
			Expect(isResourceExisting(
				ctx,
				env.client,
				&corev1.Pod{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeTrue())
			Expect(isResourceExisting(
				ctx,
				env.client,
				&corev1.PersistentVolumeClaim{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeTrue())

			Expect(env.clusterReconciler.scaleDownCluster(
				ctx,
				cluster,
				resources,
			)).To(Succeed())

			Expect(isResourceExisting(
				ctx,
				env.client,
				&corev1.Pod{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeFalse())
			Expect(isResourceExisting(
				ctx,
				env.client,
				&corev1.PersistentVolumeClaim{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeFalse())
		})
	})

	When("WAL storage is separate", func() {
		It("delete the PGDATA and WAL PVC", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGClusterWithPGWal(env.client, namespace)

			resources := &managedResources{
				pvcs: corev1.PersistentVolumeClaimList{
					Items: generateClusterPVC(env.client, cluster, persistentvolumeclaim.StatusReady),
				},
				jobs: batchv1.JobList{Items: generateFakeInitDBJobsWithDefaultClient(env.client, cluster)},
				instances: corev1.PodList{
					Items: generateFakeClusterPodsWithDefaultClient(env.client, cluster, true),
				},
			}

			instanceName := findDeletableInstance(cluster, resources.instances.Items)
			pvcWalName := persistentvolumeclaim.NewPgWalCalculator().GetName(instanceName)
			Expect(isResourceExisting(
				ctx,
				env.client,
				&corev1.Pod{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeTrue())
			Expect(isResourceExisting(
				ctx,
				env.client,
				&corev1.PersistentVolumeClaim{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeTrue())
			Expect(isResourceExisting(
				ctx,
				env.client,
				&corev1.PersistentVolumeClaim{},
				types.NamespacedName{Name: pvcWalName, Namespace: cluster.Namespace},
			)).To(BeTrue())

			Expect(env.clusterReconciler.scaleDownCluster(
				ctx,
				cluster,
				resources,
			)).To(Succeed())

			Expect(isResourceExisting(
				ctx,
				env.client,
				&corev1.Pod{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeFalse())
			Expect(isResourceExisting(
				ctx,
				env.client,
				&corev1.PersistentVolumeClaim{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeFalse())
			Expect(isResourceExisting(
				ctx,
				env.client,
				&corev1.PersistentVolumeClaim{},
				types.NamespacedName{Name: pvcWalName, Namespace: cluster.Namespace},
			)).To(BeFalse())
		})
	})
})

var _ = Describe("cluster scale pod and job deletion logic", func() {
	var (
		fakeClientSet client.WithWatch
		reconciler    *ClusterReconciler
		ctx           context.Context
		cancel        context.CancelFunc
		cluster       *apiv1.Cluster
		instanceName  string
	)

	BeforeEach(func() {
		fakeClientSet = fake.
			NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithIndex(&batchv1.Job{}, jobOwnerKey, jobOwnerIndexFunc).
			Build()
		ctx, cancel = context.WithCancel(context.Background())

		reconciler = &ClusterReconciler{
			Client: fakeClientSet,
		}

		instanceName = "test-instance"

		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
		}
		cluster.TypeMeta = metav1.TypeMeta{
			Kind:       apiv1.ClusterKind,
			APIVersion: apiv1.SchemeGroupVersion.String(),
		}
	})

	AfterEach(func() {
		cancel()
	})

	It("creates the cluster", func(ctx SpecContext) {
		err := fakeClientSet.Create(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should delete all the jobs", func(ctx SpecContext) {
		jobNames := []string{
			cluster.Name + "-initdb",
			cluster.Name + "-pgbasebackup",
		}
		for _, jobName := range jobNames {
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName,
					Namespace: cluster.Namespace,
					Labels: map[string]string{
						utils.InstanceNameLabelName: instanceName,
						utils.ClusterLabelName:      cluster.Name,
						utils.JobRoleLabelName:      "test",
					},
				},
			}
			cluster.SetInheritedDataAndOwnership(&job.ObjectMeta)
			err := fakeClientSet.Create(ctx, job)
			Expect(err).NotTo(HaveOccurred())
		}

		err := reconciler.ensureInstanceJobAreDeleted(ctx, cluster, instanceName)
		Expect(err).NotTo(HaveOccurred())

		for _, jobName := range jobNames {
			var expectedJob batchv1.Job
			err = fakeClientSet.Get(context.Background(),
				types.NamespacedName{Name: jobName, Namespace: cluster.Namespace},
				&expectedJob)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}
	})

	It("should return nil error when the instance pod is already deleted", func() {
		err := reconciler.ensureInstancePodIsDeleted(ctx, cluster, instanceName)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should delete the instance pod and report no errors", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      instanceName,
				Namespace: cluster.Namespace,
			},
		}
		err := fakeClientSet.Create(ctx, pod)
		Expect(err).NotTo(HaveOccurred())

		err = reconciler.ensureInstancePodIsDeleted(ctx, cluster, instanceName)
		Expect(err).ToNot(HaveOccurred())

		var expectedPod corev1.Pod
		err = fakeClientSet.Get(ctx,
			types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			&expectedPod)

		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})
})

// isResourceExisting check is a certain resource exists in the Kubernetes space and has not been deleted
func isResourceExisting(
	ctx context.Context,
	k8sClient client.Client,
	store client.Object,
	key client.ObjectKey,
) (bool, error) {
	err := k8sClient.Get(ctx, key, store)
	if err != nil && apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if store.GetDeletionTimestamp() != nil {
		return false, nil
	}
	return true, nil
}
