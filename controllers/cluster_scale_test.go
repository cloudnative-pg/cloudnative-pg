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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("scale down", func() {
	When("there's no separate WAL storage", func() {
		It("delete the PGDATA PVC", func() {
			ctx := context.Background()
			namespace := newFakeNamespace()
			cluster := newFakeCNPGCluster(namespace)

			resources := &managedResources{
				pvcs:      corev1.PersistentVolumeClaimList{Items: generateFakePVCWithDefaultClient(cluster)},
				jobs:      batchv1.JobList{Items: generateFakeInitDBJobsWithDefaultClient(cluster)},
				instances: corev1.PodList{Items: generateFakeClusterPodsWithDefaultClient(cluster, true)},
			}

			instanceName := findDeletableInstance(cluster, resources.instances.Items)
			Expect(isResourceExisting(
				ctx,
				&corev1.Pod{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeTrue())
			Expect(isResourceExisting(
				ctx,
				&corev1.PersistentVolumeClaim{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeTrue())

			Expect(clusterReconciler.scaleDownCluster(
				ctx,
				cluster,
				resources,
			)).To(Succeed())

			Expect(isResourceExisting(
				ctx,
				&corev1.Pod{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeFalse())
			Expect(isResourceExisting(
				ctx,
				&corev1.PersistentVolumeClaim{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeFalse())
		})
	})

	When("WAL storage is separate", func() {
		It("delete the PGDATA and WAL PVC", func() {
			ctx := context.Background()
			namespace := newFakeNamespace()
			cluster := newFakeCNPGClusterWithPGWal(namespace)

			resources := &managedResources{
				pvcs:      corev1.PersistentVolumeClaimList{Items: generateFakePVCWithDefaultClient(cluster)},
				jobs:      batchv1.JobList{Items: generateFakeInitDBJobsWithDefaultClient(cluster)},
				instances: corev1.PodList{Items: generateFakeClusterPodsWithDefaultClient(cluster, true)},
			}

			instanceName := findDeletableInstance(cluster, resources.instances.Items)
			pvcWalName := persistentvolumeclaim.GetName(instanceName, utils.PVCRolePgWal)
			Expect(isResourceExisting(
				ctx,
				&corev1.Pod{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeTrue())
			Expect(isResourceExisting(
				ctx,
				&corev1.PersistentVolumeClaim{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeTrue())
			Expect(isResourceExisting(
				ctx,
				&corev1.PersistentVolumeClaim{},
				types.NamespacedName{Name: pvcWalName, Namespace: cluster.Namespace},
			)).To(BeTrue())

			Expect(clusterReconciler.scaleDownCluster(
				ctx,
				cluster,
				resources,
			)).To(Succeed())

			Expect(isResourceExisting(
				ctx,
				&corev1.Pod{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeFalse())
			Expect(isResourceExisting(
				ctx,
				&corev1.PersistentVolumeClaim{},
				types.NamespacedName{Name: instanceName, Namespace: cluster.Namespace},
			)).To(BeFalse())
			Expect(isResourceExisting(
				ctx,
				&corev1.PersistentVolumeClaim{},
				types.NamespacedName{Name: pvcWalName, Namespace: cluster.Namespace},
			)).To(BeFalse())
		})
	})
})

// isResourceExisting check is a certain resource exists in the Kubernetes space and has not been deleted
func isResourceExisting(ctx context.Context, store ctrl.Object, key ctrl.ObjectKey) (bool, error) {
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
