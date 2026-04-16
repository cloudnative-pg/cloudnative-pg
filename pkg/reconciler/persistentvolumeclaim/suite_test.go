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
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSpecs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PersistentVolumeClaim reconciler")
}

func makePVC(
	clusterName string,
	suffix string,
	serial string,
	meta Meta,
	isResizing bool,
) corev1.PersistentVolumeClaim {
	annotations := map[string]string{
		utils.ClusterSerialAnnotationName: serial,
		utils.PVCStatusAnnotationName:     StatusReady,
	}

	var conditions []corev1.PersistentVolumeClaimCondition
	if isResizing {
		conditions = append(conditions, corev1.PersistentVolumeClaimCondition{
			Type:   corev1.PersistentVolumeClaimResizing,
			Status: corev1.ConditionTrue,
		})
	}

	var labels map[string]string
	if meta != nil {
		labels = meta.GetLabels(clusterName + "-" + suffix)
	}

	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        clusterName + "-" + suffix,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PersistentVolumeClaimSpec{},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase:      corev1.ClaimBound,
			Conditions: conditions,
		},
	}
}

func makePod(clusterName, serial, role string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName + "-" + serial,
			Annotations: map[string]string{
				utils.ClusterSerialAnnotationName: serial,
			},
			Labels: map[string]string{
				utils.ClusterInstanceRoleLabelName: role,
			},
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: clusterName + "-" + serial,
						},
					},
				},
			},
		},
	}
}

var _ = Describe("PVC detection", func() {
	It("will list PVCs with Jobs or Pods or which are Ready", func(ctx SpecContext) {
		clusterName := "myCluster"
		makeClusterPVC := func(serial string, isResizing bool) corev1.PersistentVolumeClaim {
			return makePVC(clusterName, serial, serial, NewPgDataCalculator(), isResizing)
		}
		pvcs := []corev1.PersistentVolumeClaim{
			makeClusterPVC("1", false), // has a Pod
			makeClusterPVC("2", false), // has a Job
			makeClusterPVC("3", true),  // resizing, has a Pod so stays "resizing" (not affected by #9786 fix)
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

var _ = Describe("PVC classification with resizing PVCs", func() {
	clusterName := "myCluster"
	makeCluster := func() *apiv1.Cluster {
		return &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		}
	}

	It("classifies resizing PVC without pod and with FileSystemResizePending as dangling", func(ctx SpecContext) {
		pvc := makePVC(clusterName, "1", "1", NewPgDataCalculator(), true)
		pvc.Status.Conditions = append(pvc.Status.Conditions, corev1.PersistentVolumeClaimCondition{
			Type: corev1.PersistentVolumeClaimFileSystemResizePending, Status: corev1.ConditionTrue,
		})
		cluster := makeCluster()
		EnrichStatus(ctx, cluster, []corev1.Pod{}, []batchv1.Job{}, []corev1.PersistentVolumeClaim{pvc})
		Expect(cluster.Status.DanglingPVC).Should(Equal([]string{clusterName + "-1"}))
		Expect(cluster.Status.ResizingPVC).Should(BeEmpty())
	})

	It("classifies resizing PVC without pod and without FileSystemResizePending as dangling", func(ctx SpecContext) {
		pvc := makePVC(clusterName, "1", "1", NewPgDataCalculator(), true)
		cluster := makeCluster()
		EnrichStatus(ctx, cluster, []corev1.Pod{}, []batchv1.Job{}, []corev1.PersistentVolumeClaim{pvc})
		Expect(cluster.Status.DanglingPVC).Should(Equal([]string{clusterName + "-1"}))
		Expect(cluster.Status.ResizingPVC).Should(BeEmpty())
	})

	// isResizing check takes precedence over hasJob: a resizing PVC with
	// a Job but no Pod is classified as dangling, not initializing.
	It("classifies resizing PVC with a job but no pod as dangling", func(ctx SpecContext) {
		pvc := makePVC(clusterName, "1", "1", NewPgDataCalculator(), true)
		cluster := makeCluster()
		EnrichStatus(
			ctx,
			cluster,
			[]corev1.Pod{},
			[]batchv1.Job{makeJob(clusterName, "1")},
			[]corev1.PersistentVolumeClaim{pvc},
		)
		Expect(cluster.Status.DanglingPVC).Should(Equal([]string{clusterName + "-1"}))
		Expect(cluster.Status.ResizingPVC).Should(BeEmpty())
		Expect(cluster.Status.InitializingPVC).Should(BeEmpty())
	})

	It("classifies resizing PVC as dangling when pod was deleted during rollout (#9786)", func(ctx SpecContext) {
		// Simulate simultaneous storage + resource change:
		// instance-1 has a running pod (primary), instance-2's pod was deleted
		// for a rolling update while both PVCs are resizing.
		pvc1 := makePVC(clusterName, "1", "1", NewPgDataCalculator(), true) // resizing, has pod
		pvc2 := makePVC(clusterName, "2", "2", NewPgDataCalculator(), true) // resizing, pod deleted
		cluster := makeCluster()
		EnrichStatus(
			ctx,
			cluster,
			[]corev1.Pod{makePod(clusterName, "1", specs.ClusterRoleLabelPrimary)},
			[]batchv1.Job{},
			[]corev1.PersistentVolumeClaim{pvc1, pvc2},
		)
		Expect(cluster.Status.ResizingPVC).Should(Equal([]string{clusterName + "-1"}))
		Expect(cluster.Status.DanglingPVC).Should(Equal([]string{clusterName + "-2"}))
		Expect(cluster.Status.Instances).Should(BeEquivalentTo(2))
	})
})

func makeJob(clusterName, serial string) batchv1.Job {
	return batchv1.Job{
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: clusterName + "-" + serial,
								},
							},
						},
					},
				},
			},
		},
	}
}
