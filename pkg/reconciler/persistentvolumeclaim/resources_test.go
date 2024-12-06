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

package persistentvolumeclaim

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PVC detection", func() {
	It("will list PVCs with Jobs or Pods or which are Ready", func(ctx SpecContext) {
		clusterName := "myCluster"
		makeClusterPVC := func(serial string, isResizing bool) corev1.PersistentVolumeClaim {
			return makePVC(clusterName, serial, serial, NewPgDataCalculator(), isResizing)
		}
		pvcs := []corev1.PersistentVolumeClaim{
			makeClusterPVC("1", false), // has a Pod
			makeClusterPVC("2", false), // has a Job
			makeClusterPVC("3", true),  // resizing
			makeClusterPVC("4", false), // dangling
		}
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		}
		EnrichStatus(
			ctx,
			cluster,
			[]corev1.Pod{
				makePod(clusterName, "1", specs.ClusterRoleLabelPrimary),
				makePod(clusterName, "3", specs.ClusterRoleLabelReplica),
			},
			[]batchv1.Job{makeJob(clusterName, "2")},
			pvcs,
		)

		Expect(cluster.Status.PVCCount).Should(BeEquivalentTo(4))
		Expect(cluster.Status.InstanceNames).Should(Equal([]string{
			clusterName + "-1",
			clusterName + "-2",
			clusterName + "-3",
			clusterName + "-4",
		}))
		Expect(cluster.Status.InitializingPVC).Should(Equal([]string{
			clusterName + "-2",
		}))
		Expect(cluster.Status.ResizingPVC).Should(Equal([]string{
			clusterName + "-3",
		}))
		Expect(cluster.Status.DanglingPVC).Should(Equal([]string{
			clusterName + "-4",
		}))
		Expect(cluster.Status.HealthyPVC).Should(Equal([]string{
			clusterName + "-1",
		}))
		Expect(cluster.Status.UnusablePVC).Should(BeEmpty())
	})
})

var _ = Describe("PVCs used by instance", func() {
	clusterName := "cluster-pvc-instance"
	instanceName := clusterName + "-1"

	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
		Spec: apiv1.ClusterSpec{
			WalStorage: &apiv1.StorageConfiguration{},
		},
	}

	It("true if the pvc belongs to the instance name", func() {
		res := BelongToInstance(cluster, instanceName, instanceName)
		Expect(res).To(BeTrue())

		res = BelongToInstance(cluster, instanceName, instanceName+"-wal")
		Expect(res).To(BeTrue())
	})

	It("fails when trying to get a pvc that doesn't belong to the instance", func() {
		res := BelongToInstance(cluster, instanceName, instanceName+"-nil")
		Expect(res).To(BeFalse())
	})
})

var _ = Describe("instance with tablespace test", func() {
	clusterName := "cluster-tbs-pvc-instance"
	instanceName := clusterName + "-1"

	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
		Spec: apiv1.ClusterSpec{
			StorageConfiguration: apiv1.StorageConfiguration{},
			WalStorage:           &apiv1.StorageConfiguration{},
			Tablespaces: []apiv1.TablespaceConfiguration{
				{
					Name: "tbs1",
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
				},
				{
					Name: "tbs2",
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
				},
				{
					Name: "tbs3",
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
				},
			},
		},
	}

	It("Get all the expected pvc out", func() {
		expectedPVCs := getExpectedPVCsFromCluster(cluster, instanceName)
		Expect(expectedPVCs).Should(HaveLen(5))
		for _, pvc := range expectedPVCs {
			Expect(pvc.name).Should(Equal(pvc.calculator.GetName(instanceName)))
		}
	})
})
