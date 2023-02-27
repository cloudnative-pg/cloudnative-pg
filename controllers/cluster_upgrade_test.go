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
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pod upgrade", func() {
	cluster := apiv1.Cluster{
		Spec: apiv1.ClusterSpec{
			ImageName: "postgres:13.0",
		},
	}
	It("will not require a restart for just created Pods", func() {
		pod := specs.PodWithExistingStorage(cluster, 1)
		Expect(isPodNeedingRestart(&cluster, postgres.PostgresqlStatus{Pod: *pod})).
			To(BeFalse())
	})

	It("checks when we are running a different image name", func() {
		pod := specs.PodWithExistingStorage(cluster, 1)
		pod.Spec.Containers[0].Image = "postgres:13.1"
		oldImage, newImage, err := isPodNeedingUpgradedImage(&cluster, *pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(oldImage).NotTo(BeEmpty())
		Expect(newImage).NotTo(BeEmpty())
	})

	It("checks when a restart has been scheduled on the cluster", func() {
		pod := specs.PodWithExistingStorage(cluster, 1)
		clusterRestart := cluster
		clusterRestart.Annotations = make(map[string]string)
		clusterRestart.Annotations[specs.ClusterRestartAnnotationName] = "now"
		Expect(isPodNeedingRestart(&clusterRestart, postgres.PostgresqlStatus{Pod: *pod})).
			To(BeTrue())
		Expect(isPodNeedingRestart(&cluster, postgres.PostgresqlStatus{Pod: *pod})).
			To(BeFalse())
	})

	It("checks when a restart is being needed by PostgreSQL", func() {
		pod := specs.PodWithExistingStorage(cluster, 1)
		Expect(isPodNeedingRestart(&cluster, postgres.PostgresqlStatus{Pod: *pod})).
			To(BeFalse())

		Expect(isPodNeedingRestart(&cluster,
			postgres.PostgresqlStatus{
				Pod:            *pod,
				PendingRestart: true,
			})).
			To(BeTrue())
	})

	It("checks when a rollout is being needed for any reason", func() {
		pod := specs.PodWithExistingStorage(cluster, 1)
		status := postgres.PostgresqlStatus{Pod: *pod, PendingRestart: true}
		needRollout, inplacePossible, reason := IsPodNeedingRollout(status, &cluster)
		Expect(needRollout).To(BeFalse())
		Expect(inplacePossible).To(BeFalse())
		Expect(reason).To(BeEmpty())

		status.IsPodReady = true
		needRollout, inplacePossible, reason = IsPodNeedingRollout(status, &cluster)
		Expect(needRollout).To(BeTrue())
		Expect(inplacePossible).To(BeFalse())
		Expect(reason).To(BeEmpty())

		status.ExecutableHash = "test_hash"
		needRollout, inplacePossible, reason = IsPodNeedingRollout(status, &cluster)
		Expect(needRollout).To(BeTrue())
		Expect(inplacePossible).To(BeTrue())
		Expect(reason).To(BeEquivalentTo("configuration needs a restart to apply some configuration changes"))
	})

	It("add a WAL PVC to a single instance cluster", func() {
		ctx := context.Background()

		err := k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"}})
		Expect(err).ToNot(HaveOccurred())

		cluster := newFakeCNPGCluster("test-namespace", func(cluster *apiv1.Cluster) {
			cluster.Spec.Instances = 1
			cluster.Spec.WalStorage = &apiv1.StorageConfiguration{
				Size: "1Gi",
			}
		})

		pods := generateFakeClusterPods(k8sClient, cluster, true)
		upg, err := clusterReconciler.updatePrimaryPod(
			ctx,
			cluster,
			&postgres.PostgresqlStatusList{Items: []postgres.PostgresqlStatus{{Pod: pods[0]}}},
			pods[0],
			true,
			apiv1.NewWalReason,
		)

		Expect(err).ToNot(HaveOccurred())
		Expect(upg).To(BeTrue())

		var expectedPod corev1.Pod
		podName := pods[0].Name
		namespace := pods[0].Namespace
		err = k8sClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: namespace}, &expectedPod)
		Expect(apierrs.IsNotFound(err)).To(BeTrue())

		pvcName := persistentvolumeclaim.GetName(cluster, pods[0].Name, utils.PVCRolePgWal)
		var expectedPVC corev1.PersistentVolumeClaim
		err = k8sClient.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: namespace}, &expectedPVC)
		Expect(err).To(BeNil())
		Expect(expectedPVC.Labels[utils.PvcRoleLabelName]).To(Equal(string(utils.PVCRolePgWal)))
	})

	When("there's a custom environment variable set", func() {
		It("detects when a new custom environment variable is set", func() {
			pod := specs.PodWithExistingStorage(cluster, 1)

			cluster := cluster.DeepCopy()
			cluster.Spec.Env = []corev1.EnvVar{
				{
					Name:  "TEST",
					Value: "test",
				},
			}

			needRollout, _ := isPodNeedingUpdatedEnvironment(*cluster, *pod)
			Expect(needRollout).To(BeTrue())
		})
	})
})
