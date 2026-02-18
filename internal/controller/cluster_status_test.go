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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("cluster_status unit tests", func() {
	var env *testingEnvironment
	BeforeEach(func() {
		env = buildTestEnvironment()
	})

	It("should make sure setCertExpiration works correctly", func() {
		var certExpirationDate string
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		secretName := rand.String(10)

		By("creating the required secret", func() {
			secret, keyPair := generateFakeCASecret(env.client, secretName, namespace, "unittest.com")
			Expect(secret.Name).To(Equal(secretName))

			_, expDate, err := keyPair.IsExpiring()
			Expect(err).ToNot(HaveOccurred())

			certExpirationDate = expDate.String()
		})
		By("making sure that sets the status of the secret correctly", func() {
			cluster.Status.Certificates.Expirations = map[string]string{}
			err := env.clusterReconciler.setCertExpiration(ctx, cluster, secretName, namespace, certs.CACertKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(cluster.Status.Certificates.Expirations[secretName]).To(Equal(certExpirationDate))
		})
	})

	It("makes sure that getPgbouncerIntegrationStatus returns the correct secret name without duplicates", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler1 := *newFakePooler(env.client, cluster)
		pooler2 := *newFakePooler(env.client, cluster)
		Expect(pooler1.Name).ToNot(Equal(pooler2.Name))
		poolerList := apiv1.PoolerList{Items: []apiv1.Pooler{pooler1, pooler2}}

		intStatus, err := env.clusterReconciler.getPgbouncerIntegrationStatus(ctx, cluster, poolerList)
		Expect(err).ToNot(HaveOccurred())
		Expect(intStatus.Secrets).To(HaveLen(1))
	})

	It("makes sure getObjectResourceVersion returns the correct object version", func() {
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)

		version, err := env.clusterReconciler.getObjectResourceVersion(ctx, cluster, pooler.Name, &apiv1.Pooler{})
		Expect(err).ToNot(HaveOccurred())
		Expect(version).To(Equal(pooler.ResourceVersion))
	})

	It("makes sure setPrimaryInstance works correctly", func() {
		const podName = "test-pod"
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		Expect(cluster.Status.TargetPrimaryTimestamp).To(BeEmpty())

		By("setting the primaryInstance and making sure the passed object is updated", func() {
			err := env.clusterReconciler.setPrimaryInstance(ctx, cluster, podName)
			Expect(err).ToNot(HaveOccurred())
			Expect(cluster.Status.TargetPrimaryTimestamp).ToNot(BeEmpty())
			Expect(cluster.Status.TargetPrimary).To(Equal(podName))
		})

		By("making sure the remote resource is updated", func() {
			remoteCluster := &apiv1.Cluster{}

			err := env.client.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, remoteCluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(remoteCluster.Status.TargetPrimaryTimestamp).ToNot(BeEmpty())
			Expect(remoteCluster.Status.TargetPrimary).To(Equal(podName))
		})
	})

	It("makes sure RegisterPhase works correctly", func() {
		const phaseReason = "testing"
		ctx := context.Background()
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)

		By("registering the phase and making sure the passed object is updated", func() {
			err := env.clusterReconciler.RegisterPhase(ctx, cluster, apiv1.PhaseSwitchover, phaseReason)
			Expect(err).ToNot(HaveOccurred())
			Expect(cluster.Status.Phase).To(Equal(apiv1.PhaseSwitchover))
			Expect(cluster.Status.PhaseReason).To(Equal(phaseReason))
		})

		By("making sure the remote resource is updated", func() {
			remoteCluster := &apiv1.Cluster{}
			err := env.client.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, remoteCluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(remoteCluster.Status.Phase).To(Equal(apiv1.PhaseSwitchover))
			Expect(remoteCluster.Status.PhaseReason).To(Equal(phaseReason))
		})
	})

	It("makes sure that getManagedResources works correctly", func() {
		ctx := context.Background()
		crReconciler := &ClusterReconciler{
			Client: fakeClientWithIndexAdapter{
				Client: env.clusterReconciler.Client,
			},
			DiscoveryClient: env.clusterReconciler.DiscoveryClient,
			Scheme:          env.clusterReconciler.Scheme,
			Recorder:        env.clusterReconciler.Recorder,
			InstanceClient:  env.clusterReconciler.InstanceClient,
		}

		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		var jobs []batchv1.Job
		var pods []corev1.Pod
		var pvcs []corev1.PersistentVolumeClaim

		By("creating the required resources", func() {
			jobs = generateFakeInitDBJobs(crReconciler.Client, cluster)
			pods = generateFakeClusterPods(crReconciler.Client, cluster, true)
			pvcs = generateClusterPVC(crReconciler.Client, cluster, persistentvolumeclaim.StatusReady)
			name, isOwned := IsOwnedByCluster(&pods[0])
			Expect(isOwned).To(BeTrue())
			Expect(name).To(Equal(cluster.Name))
		})

		By("making sure that the required resources are found", func() {
			Eventually(func() (*managedResources, error) {
				return crReconciler.getManagedResources(ctx, cluster)
			}).Should(Satisfy(func(mr *managedResources) bool {
				return len(mr.instances.Items) == len(pods) &&
					len(mr.jobs.Items) == len(jobs) &&
					len(mr.pvcs.Items) == len(pvcs)
			}))
		})
	})
})

var _ = Describe("updateClusterStatusThatRequiresInstancesState tests", func() {
	var (
		env     *testingEnvironment
		cluster *apiv1.Cluster
	)

	BeforeEach(func() {
		env = buildTestEnvironment()
		cluster = newFakeCNPGCluster(env.client, newFakeNamespace(env.client))
	})

	It("should handle empty status list", func(ctx SpecContext) {
		statuses := postgres.PostgresqlStatusList{}

		err := env.clusterReconciler.updateClusterStatusThatRequiresInstancesState(ctx, cluster, statuses)
		Expect(err).ToNot(HaveOccurred())

		Expect(cluster.Status.InstancesReportedState).To(BeEmpty())
		Expect(cluster.Status.SystemID).To(BeEmpty())

		condition := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionConsistentSystemID))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal("NotFound"))
		Expect(condition.Message).To(Equal("No instances are present in the cluster to report a system ID."))
	})

	It("should handle instances without SystemID", func(ctx SpecContext) {
		statuses := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
						Status:     corev1.PodStatus{PodIP: "192.168.1.1"},
					},
					IsPrimary:  true,
					TimeLineID: 123,
					SystemID:   "",
				},
				{
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-2"},
						Status:     corev1.PodStatus{PodIP: "192.168.1.2"},
					},
					IsPrimary: false,
					SystemID:  "",
				},
			},
		}

		err := env.clusterReconciler.updateClusterStatusThatRequiresInstancesState(ctx, cluster, statuses)
		Expect(err).ToNot(HaveOccurred())

		Expect(cluster.Status.InstancesReportedState).To(HaveLen(2))
		Expect(cluster.Status.TimelineID).To(Equal(123))
		Expect(cluster.Status.SystemID).To(BeEmpty())

		condition := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionConsistentSystemID))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal("NotFound"))
		Expect(condition.Message).To(Equal("Instances are present, but none have reported a system ID."))
	})

	It("should handle instances with a single SystemID", func(ctx SpecContext) {
		const systemID = "system123"
		statuses := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
						Status:     corev1.PodStatus{PodIP: "192.168.1.1"},
					},
					IsPrimary:  true,
					TimeLineID: 123,
					SystemID:   systemID,
				},
				{
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-2"},
						Status:     corev1.PodStatus{PodIP: "192.168.1.2"},
					},
					IsPrimary: false,
					SystemID:  systemID,
				},
			},
		}

		err := env.clusterReconciler.updateClusterStatusThatRequiresInstancesState(ctx, cluster, statuses)
		Expect(err).ToNot(HaveOccurred())

		Expect(cluster.Status.InstancesReportedState).To(HaveLen(2))
		Expect(cluster.Status.TimelineID).To(Equal(123))
		Expect(cluster.Status.SystemID).To(Equal(systemID))

		condition := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionConsistentSystemID))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal("Unique"))
		Expect(condition.Message).To(Equal("A single, unique system ID was found across reporting instances."))
	})

	It("should handle instances with multiple SystemIDs", func(ctx SpecContext) {
		statuses := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
						Status:     corev1.PodStatus{PodIP: "192.168.1.1"},
					},
					IsPrimary:  true,
					TimeLineID: 123,
					SystemID:   "system1",
				},
				{
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-2"},
						Status:     corev1.PodStatus{PodIP: "192.168.1.2"},
					},
					IsPrimary: false,
					SystemID:  "system2",
				},
			},
		}

		err := env.clusterReconciler.updateClusterStatusThatRequiresInstancesState(ctx, cluster, statuses)
		Expect(err).ToNot(HaveOccurred())

		Expect(cluster.Status.InstancesReportedState).To(HaveLen(2))
		Expect(cluster.Status.TimelineID).To(Equal(123))
		Expect(cluster.Status.SystemID).To(BeEmpty())

		condition := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionConsistentSystemID))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal("Mismatch"))
		Expect(condition.Message).To(ContainSubstring("Multiple differing system IDs reported by instances:"))
		Expect(condition.Message).To(ContainSubstring("system1"))
		Expect(condition.Message).To(ContainSubstring("system2"))
	})

	It("should update timeline ID from the primary instance", func(ctx SpecContext) {
		const timelineID = 999
		statuses := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
						Status:     corev1.PodStatus{PodIP: "192.168.1.1"},
					},
					IsPrimary:  true,
					TimeLineID: timelineID,
					SystemID:   "system1",
				},
				{
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-2"},
						Status:     corev1.PodStatus{PodIP: "192.168.1.2"},
					},
					IsPrimary:  false,
					TimeLineID: 123,
					SystemID:   "system1",
				},
			},
		}

		err := env.clusterReconciler.updateClusterStatusThatRequiresInstancesState(ctx, cluster, statuses)
		Expect(err).ToNot(HaveOccurred())

		Expect(cluster.Status.TimelineID).To(Equal(timelineID))
	})

	It("should correctly populate InstancesReportedState", func(ctx SpecContext) {
		statuses := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
						Status:     corev1.PodStatus{PodIP: "192.168.1.1"},
					},
					IsPrimary:  true,
					TimeLineID: 123,
				},
				{
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-2"},
						Status:     corev1.PodStatus{PodIP: "192.168.1.2"},
					},
					IsPrimary:  false,
					TimeLineID: 123,
				},
			},
		}

		err := env.clusterReconciler.updateClusterStatusThatRequiresInstancesState(ctx, cluster, statuses)
		Expect(err).ToNot(HaveOccurred())

		Expect(cluster.Status.InstancesReportedState).To(HaveLen(2))

		state1 := cluster.Status.InstancesReportedState["pod-1"]
		Expect(state1.IsPrimary).To(BeTrue())
		Expect(state1.TimeLineID).To(Equal(123))
		Expect(state1.IP).To(Equal("192.168.1.1"))

		state2 := cluster.Status.InstancesReportedState["pod-2"]
		Expect(state2.IsPrimary).To(BeFalse())
		Expect(state2.TimeLineID).To(Equal(123))
		Expect(state2.IP).To(Equal("192.168.1.2"))
	})

	Context("Pod termination reason detection", func() {
		It("should detect when a pod has no PostgreSQL container", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "other-container"},
					},
				},
			}

			// When no postgres container exists, hasPostgresContainerTerminationReason returns false
			result := hasPostgresContainerTerminationReason(pod, func(state *corev1.ContainerState) bool {
				return state.Terminated != nil
			})
			Expect(result).To(BeFalse())
		})

		It("should detect termination with specific exit code in current state", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "postgres",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: apiv1.MissingWALDiskSpaceExitCode,
								},
							},
						},
					},
				},
			}

			result := hasPostgresContainerTerminationReason(pod, func(state *corev1.ContainerState) bool {
				return state.Terminated != nil && state.Terminated.ExitCode == apiv1.MissingWALDiskSpaceExitCode
			})
			Expect(result).To(BeTrue())
		})

		It("should detect termination with specific exit code in last termination state", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "postgres",
							State: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{},
							},
							Ready: false,
							LastTerminationState: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: apiv1.MissingWALArchivePlugin,
								},
							},
						},
					},
				},
			}

			result := hasPostgresContainerTerminationReason(pod, func(state *corev1.ContainerState) bool {
				return state.Terminated != nil && state.Terminated.ExitCode == apiv1.MissingWALArchivePlugin
			})
			Expect(result).To(BeTrue())
		})

		It("should return false when termination reason does not match", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "postgres",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 1, // Different exit code
								},
							},
						},
					},
				},
			}

			result := hasPostgresContainerTerminationReason(pod, func(state *corev1.ContainerState) bool {
				return state.Terminated != nil && state.Terminated.ExitCode == apiv1.MissingWALDiskSpaceExitCode
			})
			Expect(result).To(BeFalse())
		})

		It("should return false when container is ready despite last termination", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "postgres",
							State: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{},
							},
							Ready: true,
							LastTerminationState: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: apiv1.MissingWALDiskSpaceExitCode,
								},
							},
						},
					},
				},
			}

			result := hasPostgresContainerTerminationReason(pod, func(state *corev1.ContainerState) bool {
				return state.Terminated != nil && state.Terminated.ExitCode == apiv1.MissingWALDiskSpaceExitCode
			})
			Expect(result).To(BeFalse())
		})

		It("isWALSpaceAvailableOnPod should return true when space is available", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "postgres",
							State: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{},
							},
							Ready: true,
						},
					},
				},
			}

			Expect(isWALSpaceAvailableOnPod(pod)).To(BeTrue())
		})

		It("isWALSpaceAvailableOnPod should return false when terminated due to missing disk space", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "postgres",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: apiv1.MissingWALDiskSpaceExitCode,
								},
							},
						},
					},
				},
			}

			Expect(isWALSpaceAvailableOnPod(pod)).To(BeFalse())
		})

		It("isTerminatedBecauseOfMissingWALArchivePlugin should return true when terminated due to missing plugin", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "postgres",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: apiv1.MissingWALArchivePlugin,
								},
							},
						},
					},
				},
			}

			Expect(isTerminatedBecauseOfMissingWALArchivePlugin(pod)).To(BeTrue())
		})

		It("isTerminatedBecauseOfMissingWALArchivePlugin should return false when not terminated", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "postgres",
							State: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{},
							},
							Ready: true,
						},
					},
				},
			}

			Expect(isTerminatedBecauseOfMissingWALArchivePlugin(pod)).To(BeFalse())
		})
	})
})

var _ = Describe("managedResources", func() {
	Context("runningJobNames", func() {
		It("should exclude completed jobs", func() {
			resources := &managedResources{
				jobs: batchv1.JobList{
					Items: []batchv1.Job{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "completed-job"},
							Status: batchv1.JobStatus{
								Succeeded: 1,
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{Name: "running-job"},
							Status: batchv1.JobStatus{
								Succeeded: 0,
							},
						},
					},
				},
			}

			names := resources.runningJobNames()
			Expect(names).To(HaveLen(1))
			Expect(names).To(ContainElement("running-job"))
			Expect(names).NotTo(ContainElement("completed-job"))
		})

		It("should exclude failed jobs", func() {
			resources := &managedResources{
				jobs: batchv1.JobList{
					Items: []batchv1.Job{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "failed-job"},
							Status: batchv1.JobStatus{
								Succeeded: 0,
								Conditions: []batchv1.JobCondition{
									{
										Type:   batchv1.JobFailed,
										Status: corev1.ConditionTrue,
									},
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{Name: "running-job"},
							Status: batchv1.JobStatus{
								Succeeded: 0,
							},
						},
					},
				},
			}

			names := resources.runningJobNames()
			Expect(names).To(HaveLen(1))
			Expect(names).To(ContainElement("running-job"))
			Expect(names).NotTo(ContainElement("failed-job"))
		})

		It("should return empty when all jobs are completed or failed", func() {
			resources := &managedResources{
				jobs: batchv1.JobList{
					Items: []batchv1.Job{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "completed-job"},
							Status: batchv1.JobStatus{
								Succeeded: 1,
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{Name: "failed-job"},
							Status: batchv1.JobStatus{
								Succeeded: 0,
								Conditions: []batchv1.JobCondition{
									{
										Type:   batchv1.JobFailed,
										Status: corev1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			}

			names := resources.runningJobNames()
			Expect(names).To(BeEmpty())
		})
	})
})
