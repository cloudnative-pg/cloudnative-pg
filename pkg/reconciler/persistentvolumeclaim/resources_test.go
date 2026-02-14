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

var _ = Describe("isFileSystemResizePending", func() {
	It("returns false when no conditions are present", func() {
		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pvc",
			},
			Status: corev1.PersistentVolumeClaimStatus{
				Conditions: nil,
			},
		}
		Expect(isFileSystemResizePending(pvc)).To(BeFalse())
	})

	It("returns false when only Resizing condition is present", func() {
		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pvc",
			},
			Status: corev1.PersistentVolumeClaimStatus{
				Conditions: []corev1.PersistentVolumeClaimCondition{
					{
						Type:   corev1.PersistentVolumeClaimResizing,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		Expect(isFileSystemResizePending(pvc)).To(BeFalse())
	})

	It("returns true when FileSystemResizePending condition is present", func() {
		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pvc",
			},
			Status: corev1.PersistentVolumeClaimStatus{
				Conditions: []corev1.PersistentVolumeClaimCondition{
					{
						Type:   corev1.PersistentVolumeClaimFileSystemResizePending,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		Expect(isFileSystemResizePending(pvc)).To(BeTrue())
	})

	It("returns true when both Resizing and FileSystemResizePending conditions are present", func() {
		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pvc",
			},
			Status: corev1.PersistentVolumeClaimStatus{
				Conditions: []corev1.PersistentVolumeClaimCondition{
					{
						Type:   corev1.PersistentVolumeClaimResizing,
						Status: corev1.ConditionTrue,
					},
					{
						Type:   corev1.PersistentVolumeClaimFileSystemResizePending,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		Expect(isFileSystemResizePending(pvc)).To(BeTrue())
	})
})

var _ = Describe("PVC classification with FileSystemResizePending", func() {
	clusterName := "myCluster"

	It("classifies resizing PVC with pod as resizing (can complete resize)", func(ctx SpecContext) {
		// When a pod is attached and PVC is resizing, filesystem resize can complete
		pvc := makePVC(clusterName, "1", "1", NewPgDataCalculator(), true)
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		}
		EnrichStatus(
			ctx,
			cluster,
			[]corev1.Pod{makePod(clusterName, "1", specs.ClusterRoleLabelPrimary)},
			[]batchv1.Job{},
			[]corev1.PersistentVolumeClaim{pvc},
		)
		Expect(cluster.Status.ResizingPVC).Should(Equal([]string{clusterName + "-1"}))
		Expect(cluster.Status.DanglingPVC).Should(BeEmpty())
	})

	It("classifies FileSystemResizePending PVC without pod as dangling to trigger pod creation", func(ctx SpecContext) {
		// Key scenario: volume resize done at storage layer, but filesystem resize
		// needs a pod mount. Should be classified as dangling to trigger pod creation.
		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName + "-1",
				Labels: map[string]string{
					"cnpg.io/pvcRole": "PG_DATA",
				},
				Annotations: map[string]string{
					"cnpg.io/nodeSerial":    "1",
					"cnpg.io/operatorOwned": "true",
					"cnpg.io/pvcStatus":     StatusReady,
				},
			},
			Status: corev1.PersistentVolumeClaimStatus{
				Phase: corev1.ClaimBound,
				Conditions: []corev1.PersistentVolumeClaimCondition{
					{
						Type:   corev1.PersistentVolumeClaimResizing,
						Status: corev1.ConditionTrue,
					},
					{
						Type:   corev1.PersistentVolumeClaimFileSystemResizePending,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		}
		EnrichStatus(
			ctx,
			cluster,
			[]corev1.Pod{}, // No pod attached
			[]batchv1.Job{},
			[]corev1.PersistentVolumeClaim{pvc},
		)
		// Should be dangling (not resizing) to trigger pod creation
		Expect(cluster.Status.DanglingPVC).Should(Equal([]string{clusterName + "-1"}))
		Expect(cluster.Status.ResizingPVC).Should(BeEmpty())
	})

	It("classifies resizing PVC without pod and without FileSystemResizePending as resizing", func(ctx SpecContext) {
		// When volume resize is still in progress at storage layer (no FileSystemResizePending),
		// we should wait and not create a pod yet
		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName + "-1",
				Labels: map[string]string{
					"cnpg.io/pvcRole": "PG_DATA",
				},
				Annotations: map[string]string{
					"cnpg.io/nodeSerial":    "1",
					"cnpg.io/operatorOwned": "true",
					"cnpg.io/pvcStatus":     StatusReady,
				},
			},
			Status: corev1.PersistentVolumeClaimStatus{
				Phase: corev1.ClaimBound,
				Conditions: []corev1.PersistentVolumeClaimCondition{
					{
						Type:   corev1.PersistentVolumeClaimResizing,
						Status: corev1.ConditionTrue,
					},
					// No FileSystemResizePending - volume resize still in progress
				},
			},
		}
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		}
		EnrichStatus(
			ctx,
			cluster,
			[]corev1.Pod{}, // No pod attached
			[]batchv1.Job{},
			[]corev1.PersistentVolumeClaim{pvc},
		)
		// Should remain as resizing since volume resize not complete
		Expect(cluster.Status.ResizingPVC).Should(Equal([]string{clusterName + "-1"}))
		Expect(cluster.Status.DanglingPVC).Should(BeEmpty())
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
