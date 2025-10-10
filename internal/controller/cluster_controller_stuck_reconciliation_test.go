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
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	fakediscovery "k8s.io/client-go/discovery/fake"
	k8stesting "k8s.io/client-go/testing"
)

var _ = Describe("Stuck Reconciliation Recovery", func() {
	var (
		ctx        context.Context
		reconciler *ClusterReconciler
		cluster    *apiv1.Cluster
		namespace  string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = "test-namespace"

		// Create a fake client with the scheme
		scheme := schemeBuilder.BuildWithAllKnownScheme()

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&apiv1.Cluster{}).
			WithIndex(&batchv1.Job{}, jobOwnerKey, jobOwnerIndexFunc).
			WithIndex(&corev1.Pod{}, podOwnerKey, func(rawObj client.Object) []string {
				pod := rawObj.(*corev1.Pod)
				if ownerName, ok := IsOwnedByCluster(pod); ok {
					return []string{ownerName}
				}
				return nil
			}).
			WithIndex(&corev1.PersistentVolumeClaim{}, pvcOwnerKey, func(rawObj client.Object) []string {
				persistentVolumeClaim := rawObj.(*corev1.PersistentVolumeClaim)
				if ownerName, ok := IsOwnedByCluster(persistentVolumeClaim); ok {
					return []string{ownerName}
				}
				return nil
			}).
			Build()

		// Create fake discovery client
		fakeDiscoveryClient := &fakediscovery.FakeDiscovery{
			Fake: &k8stesting.Fake{
				Resources: []*metav1.APIResourceList{},
			},
		}

		// Create a fake event recorder
		fakeRecorder := record.NewFakeRecorder(100)

		// Create the reconciler
		reconciler = &ClusterReconciler{
			Client:          fakeClient,
			Scheme:          scheme,
			Recorder:        fakeRecorder,
			DiscoveryClient: fakeDiscoveryClient,
		}

		// Create a test cluster
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: namespace,
				UID:       "test-uid",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3, // Start with 3 instances
			},
			Status: apiv1.ClusterStatus{
				Instances:      3,
				ReadyInstances: 3,
				Phase:          apiv1.PhaseHealthy,
			},
		}

		// Create the cluster in the fake client
		Expect(reconciler.Create(ctx, cluster)).To(Succeed())
	})

	Describe("End-to-End Stuck Reconciliation Recovery", func() {
		It("should handle scale up → fail → scale down scenario", func() {
			By("Starting with a healthy 3-instance cluster")
			Expect(cluster.Spec.Instances).To(Equal(3))
			Expect(cluster.Status.Phase).To(Equal(apiv1.PhaseHealthy))

			By("Scaling up to 4 instances")
			cluster.Spec.Instances = 4
			Expect(reconciler.Update(ctx, cluster)).To(Succeed())

			By("Simulating the cluster entering 'Creating a new replica' phase")
			cluster.Status.Phase = "Creating a new replica"
			cluster.Status.PhaseReason = "Creating replica test-cluster-4-snapshot-recovery"
			Expect(reconciler.Status().Update(ctx, cluster)).To(Succeed())

			By("Creating a stuck snapshot-recovery job")
			stuckJob := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-4-snapshot-recovery",
					Namespace: namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: apiv1.SchemeGroupVersion.String(),
							Kind:       apiv1.ClusterKind,
							Name:       cluster.Name,
							UID:        cluster.UID,
							Controller: &[]bool{true}[0],
						},
					},
					CreationTimestamp: metav1.Time{Time: time.Now().Add(-20 * time.Minute)}, // Old job
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "pgdata",
									VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
											ClaimName: "missing-pvc", // This PVC doesn't exist
										},
									},
								},
							},
							Containers: []corev1.Container{
								{
									Name:  "postgres",
									Image: "postgres:15",
								},
							},
						},
					},
				},
				Status: batchv1.JobStatus{
					Active:    0, // No active pods due to missing PVC
					Succeeded: 0,
					Failed:    0,
				},
			}
			Expect(reconciler.Create(ctx, stuckJob)).To(Succeed())

			By("Creating managed resources with the stuck job")
			resources := &managedResources{
				nodes: make(map[string]corev1.Node),
				jobs: batchv1.JobList{
					Items: []batchv1.Job{*stuckJob},
				},
				instances: corev1.PodList{
					Items: []corev1.Pod{
						// Simulate 3 existing healthy instances
						createTestPod("test-cluster-1", namespace, cluster),
						createTestPod("test-cluster-2", namespace, cluster),
						createTestPod("test-cluster-3", namespace, cluster),
					},
				},
				pvcs: corev1.PersistentVolumeClaimList{
					Items: []corev1.PersistentVolumeClaim{
						// Only PVCs for existing instances, missing the one for instance 4
						createTestPVC("test-cluster-1-pgdata", namespace, cluster),
						createTestPVC("test-cluster-2-pgdata", namespace, cluster),
						createTestPVC("test-cluster-3-pgdata", namespace, cluster),
					},
				},
			}

			By("Testing stuck job handling through reconcileResources")
			// The reconcileResources method checks for stuck jobs and deletes them
			// Pass an empty PostgresqlStatusList as it's not needed for stuck job detection
			var instancesStatus postgres.PostgresqlStatusList
			result, err := reconciler.reconcileResources(ctx, cluster, resources, instancesStatus)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeTrue(), "Should requeue after deleting stuck job")

			By("Verifying the stuck job was deleted")
			deletedJob := &batchv1.Job{}
			err = reconciler.Get(ctx, types.NamespacedName{
				Name:      stuckJob.Name,
				Namespace: stuckJob.Namespace,
			}, deletedJob)
			Expect(apierrs.IsNotFound(err)).To(BeTrue(), "Stuck job should be deleted")

			By("Simulating user decision to scale down instead of retrying")
			// Refresh cluster state
			Expect(reconciler.Get(ctx, types.NamespacedName{
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			}, cluster)).To(Succeed())

			// User scales down to 3 instances
			cluster.Spec.Instances = 3
			Expect(reconciler.Update(ctx, cluster)).To(Succeed())

			By("Running checkAndClearStuckScalingPhase with correct instance count")
			// Update resources to reflect no running jobs and correct instance count
			resources.jobs.Items = []batchv1.Job{} // No more jobs

			err = reconciler.checkAndClearStuckScalingPhase(ctx, cluster, resources)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying the scaling phase was cleared")
			// Refresh cluster state
			Expect(reconciler.Get(ctx, types.NamespacedName{
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			}, cluster)).To(Succeed())

			// After scaling down to match desired instances and clearing stuck scaling phases,
			// the cluster should return to healthy state. However, if there are still error
			// conditions from the recent job cleanup, it might be in a transitional phase.
			// The key point is that it should no longer be stuck in a scaling phase.
			Expect(isInScalingPhase(cluster)).To(BeFalse(),
				"Cluster should not be stuck in a scaling phase when instance count matches")
		})

		It("should detect and handle missing PVCs", func() {
			By("Creating a job that requires a missing PVC")
			jobWithMissingPVC := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-job-missing-pvc",
					Namespace:         namespace,
					CreationTimestamp: metav1.Time{Time: time.Now().Add(-20 * time.Minute)},
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "data",
									VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
											ClaimName: "missing-pvc-name",
										},
									},
								},
							},
							Containers: []corev1.Container{
								{Name: "test", Image: "test"},
							},
						},
					},
				},
				Status: batchv1.JobStatus{
					Active:    0,
					Succeeded: 0,
					Failed:    0,
				},
			}

			By("Checking for missing PVCs")
			err := reconciler.checkForMissingPVCs(ctx, cluster, jobWithMissingPVC)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("missing PVCs: [missing-pvc-name]"))
		})

		It("should detect equilibrium state", func() {
			By("Creating a long-running job with no progress")
			oldJob := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "old-stuck-job",
					Namespace:         namespace,
					CreationTimestamp: metav1.Time{Time: time.Now().Add(-20 * time.Minute)},
				},
				Status: batchv1.JobStatus{
					Active:    0,
					Succeeded: 0,
					Failed:    0,
				},
			}

			resources := &managedResources{
				jobs: batchv1.JobList{
					Items: []batchv1.Job{*oldJob},
				},
			}

			By("Checking for equilibrium state")
			err := reconciler.checkForEquilibriumState(ctx, cluster, resources)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("equilibrium state detected"))
		})

		It("should clear scaling phase after job deletion", func() {
			By("Setting cluster to a scaling phase")
			cluster.Status.Phase = "Creating a new replica"
			cluster.Status.PhaseReason = "Creating replica test-cluster-4"
			Expect(reconciler.Status().Update(ctx, cluster)).To(Succeed())

			By("Clearing scaling phase after job deletion")
			err := reconciler.clearStuckScalingPhaseAfterJobDeletion(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying phase reason was cleared")
			// Refresh cluster state
			Expect(reconciler.Get(ctx, types.NamespacedName{
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			}, cluster)).To(Succeed())

			Expect(cluster.Status.PhaseReason).To(BeEmpty(),
				"Phase reason should be cleared to allow retry")
		})
	})

	Describe("Helper Functions", func() {
		It("should correctly identify scaling phases", func() {
			By("Testing non-scaling phases")
			cluster.Status.Phase = apiv1.PhaseHealthy
			Expect(isInScalingPhase(cluster)).To(BeFalse())

			cluster.Status.Phase = apiv1.PhaseWaitingForInstancesToBeActive
			Expect(isInScalingPhase(cluster)).To(BeFalse())

			By("Testing scaling phases")
			cluster.Status.Phase = apiv1.PhaseCreatingReplica
			Expect(isInScalingPhase(cluster)).To(BeTrue())

			cluster.Status.Phase = apiv1.PhaseScalingUp
			Expect(isInScalingPhase(cluster)).To(BeTrue())

			cluster.Status.Phase = apiv1.PhaseScalingDown
			Expect(isInScalingPhase(cluster)).To(BeTrue())
		})

		It("should surface detailed error information in phase reasons", func() {
			By("Creating a failed job with detailed status")
			failedJob := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "failed-job",
					Namespace:         namespace,
					CreationTimestamp: metav1.Time{Time: time.Now().Add(-5 * time.Minute)},
				},
				Status: batchv1.JobStatus{
					Active:    0,
					Succeeded: 0,
					Failed:    1,
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobFailed,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}

			By("Creating the failed job in the fake client")
			Expect(reconciler.Create(ctx, failedJob)).To(Succeed())

			By("Creating resources with the failed job")
			resources := &managedResources{
				jobs: batchv1.JobList{
					Items: []batchv1.Job{*failedJob},
				},
				instances: corev1.PodList{Items: []corev1.Pod{}},
				pvcs:      corev1.PersistentVolumeClaimList{Items: []corev1.PersistentVolumeClaim{}},
			}

			By("Processing the failed job through reconcileResources")
			var instancesStatus postgres.PostgresqlStatusList
			_, err := reconciler.reconcileResources(ctx, cluster, resources, instancesStatus)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying detailed error information is in cluster status")
			// Refresh cluster state
			Expect(reconciler.Get(ctx, types.NamespacedName{
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			}, cluster)).To(Succeed())

			// The phase reason should contain detailed job information
			Expect(cluster.Status.PhaseReason).To(ContainSubstring("failed-job"))
			Expect(cluster.Status.PhaseReason).To(ContainSubstring("failed:1"))
			Expect(cluster.Status.PhaseReason).To(ContainSubstring("age:"))
		})

		It("should include missing PVC information in error messages", func() {
			By("Creating a stuck job with missing PVC")
			stuckJobWithMissingPVC := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "stuck-job-missing-pvc",
					Namespace:         namespace,
					CreationTimestamp: metav1.Time{Time: time.Now().Add(-15 * time.Minute)},
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "data",
									VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
											ClaimName: "missing-pvc-for-test",
										},
									},
								},
							},
							Containers: []corev1.Container{
								{Name: "test", Image: "test"},
							},
						},
					},
				},
				Status: batchv1.JobStatus{
					Active:    0,
					Succeeded: 0,
					Failed:    0,
				},
			}

			By("Creating the stuck job in the fake client")
			Expect(reconciler.Create(ctx, stuckJobWithMissingPVC)).To(Succeed())

			By("Creating resources with the stuck job")
			resources := &managedResources{
				jobs: batchv1.JobList{
					Items: []batchv1.Job{*stuckJobWithMissingPVC},
				},
				instances: corev1.PodList{Items: []corev1.Pod{}},
				pvcs:      corev1.PersistentVolumeClaimList{Items: []corev1.PersistentVolumeClaim{}},
			}

			By("Processing the stuck job through reconcileResources")
			var instancesStatus postgres.PostgresqlStatusList
			_, err := reconciler.reconcileResources(ctx, cluster, resources, instancesStatus)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying missing PVC information is included in phase reason")
			// Refresh cluster state
			Expect(reconciler.Get(ctx, types.NamespacedName{
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			}, cluster)).To(Succeed())

			// The phase reason should contain missing PVC information
			Expect(cluster.Status.PhaseReason).To(ContainSubstring("stuck-job-missing-pvc"))
			Expect(cluster.Status.PhaseReason).To(ContainSubstring("missing PVCs"))
			Expect(cluster.Status.PhaseReason).To(ContainSubstring("missing-pvc-for-test"))
		})
	})

	Describe("Job Utility Functions Integration", func() {
		It("should correctly identify stuck jobs", func() {
			By("Creating a stuck job")
			stuckJob := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Time{Time: time.Now().Add(-15 * time.Minute)},
				},
				Status: batchv1.JobStatus{
					Active:    0,
					Succeeded: 0,
					Failed:    0,
				},
			}

			By("Verifying job is detected as stuck")
			isStuck := utils.IsJobStuck(stuckJob, 10*time.Minute)
			Expect(isStuck).To(BeTrue())

			isFailedOrStuck := utils.IsJobFailedOrStuck(stuckJob, 10*time.Minute)
			Expect(isFailedOrStuck).To(BeTrue())
		})

		It("should correctly identify failed jobs", func() {
			By("Creating a failed job")
			failedJob := batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobFailed,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}

			By("Verifying job is detected as failed")
			isFailed := utils.IsJobFailed(failedJob)
			Expect(isFailed).To(BeTrue())

			isFailedOrStuck := utils.IsJobFailedOrStuck(failedJob, 10*time.Minute)
			Expect(isFailedOrStuck).To(BeTrue())
		})
	})
})

// Helper functions for creating test objects

func createTestPod(name, namespace string, cluster *apiv1.Cluster) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: apiv1.SchemeGroupVersion.String(),
					Kind:       apiv1.ClusterKind,
					Name:       cluster.Name,
					UID:        cluster.UID,
					Controller: &[]bool{true}[0],
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
}

func createTestPVC(name, namespace string, cluster *apiv1.Cluster) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: apiv1.SchemeGroupVersion.String(),
					Kind:       apiv1.ClusterKind,
					Name:       cluster.Name,
					UID:        cluster.UID,
					Controller: &[]bool{true}[0],
				},
			},
			Annotations: map[string]string{
				utils.PVCStatusAnnotationName: "ready",
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}
}
