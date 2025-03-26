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

package volumesnapshot

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("offlineExecutor", func() {
	var (
		backup  *apiv1.Backup
		cluster *apiv1.Cluster
		pod     *corev1.Pod
		oe      *offlineExecutor
	)

	BeforeEach(func() {
		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-test-1",
				Namespace: "default",
			},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.ContainersReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		backup = &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "backup-test",
				Namespace: "default",
			},
		}

		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-test",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{},
			Status: apiv1.ClusterStatus{
				CurrentPrimary: pod.Name,
				TargetPrimary:  pod.Name,
			},
		}

		oe = &offlineExecutor{
			cli: fake.NewClientBuilder().
				WithScheme(scheme.BuildWithAllKnownScheme()).
				WithObjects(cluster, pod).
				WithStatusSubresource(cluster, pod).
				Build(),
			recorder: record.NewFakeRecorder(100000),
		}
	})

	It("ensurePodIsFenced should correctly fence the pod", func(ctx SpecContext) {
		err := oe.ensurePodIsFenced(ctx, cluster, backup, pod.Name)
		Expect(err).ToNot(HaveOccurred())

		var patchedCluster apiv1.Cluster
		err = oe.cli.Get(ctx, k8client.ObjectKeyFromObject(cluster), &patchedCluster)
		Expect(err).ToNot(HaveOccurred())

		list, err := utils.GetFencedInstances(patchedCluster.Annotations)
		Expect(err).ToNot(HaveOccurred())
		Expect(list.ToList()).ToNot(BeEmpty())
		Expect(list.Has(pod.Name)).To(BeTrue())
	})

	It("should ensure that waitForPodToBeFenced correctly evaluates pod conditions", func(ctx SpecContext) {
		res, err := oe.waitForPodToBeFenced(ctx, pod)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(time.Second * 10))

		pod.Status.Conditions[0].Status = corev1.ConditionFalse
		err = oe.cli.Status().Update(ctx, pod)
		Expect(err).ToNot(HaveOccurred())

		res, err = oe.waitForPodToBeFenced(ctx, pod)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(BeNil())
	})

	It("finalize should remove the fencing annotation from the cluster", func(ctx SpecContext) {
		modified, err := utils.AddFencedInstance(pod.Name, &cluster.ObjectMeta)
		Expect(err).ToNot(HaveOccurred())
		Expect(modified).To(BeTrue())

		err = oe.cli.Update(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())

		list, err := utils.GetFencedInstances(cluster.Annotations)
		Expect(err).ToNot(HaveOccurred())
		Expect(list.ToList()).ToNot(BeEmpty())
		Expect(list.Has(pod.Name)).To(BeTrue())

		res, err := oe.finalize(ctx, cluster, backup, pod)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(BeNil())

		var patchedCluster apiv1.Cluster
		err = oe.cli.Get(ctx, k8client.ObjectKeyFromObject(cluster), &patchedCluster)
		Expect(err).ToNot(HaveOccurred())

		list, err = utils.GetFencedInstances(patchedCluster.Annotations)
		Expect(err).ToNot(HaveOccurred())
		Expect(list.ToList()).To(BeEmpty())
	})
})
