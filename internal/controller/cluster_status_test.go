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
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

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

	It("produces a scale-subresource selector matching managed instance pods", func() {
		// updateResourceStatus publishes cluster.GetInstancesSelector() into
		// .status.selector so VPA/HPA can discover instance pods through the scale
		// subresource. We exercise the production code (GetInstancesSelector) and
		// verify both that it has the expected format and that it actually matches
		// the labels the operator applies to every instance pod.
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pods := generateFakeClusterPods(env.client, cluster, true)
		Expect(pods).ToNot(BeEmpty())

		selectorString := cluster.GetInstancesSelector()

		// GetInstancesSelector serializes through labels.SelectorFromSet, which
		// sorts requirements by key. The cluster label key sorts before the pod
		// role label key, so the expected string lists them in that order.
		expected := fmt.Sprintf("%s=%s,%s=%s",
			utils.ClusterLabelName, cluster.Name,
			utils.PodRoleLabelName, string(utils.PodRoleInstance))
		Expect(selectorString).To(Equal(expected))

		selector, err := labels.Parse(selectorString)
		Expect(err).ToNot(HaveOccurred())

		for i := range pods {
			Expect(selector.Matches(labels.Set(pods[i].Labels))).To(BeTrue(),
				"selector %q must match pod %s labels %v", selectorString, pods[i].Name, pods[i].Labels)
		}
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
	})

	Describe("getPodsTopology", func() {
		const zoneLabel = "topology.kubernetes.io/zone"

		makePod := func(name, nodeName string, podLabels map[string]string) corev1.Pod {
			return corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: name, Labels: podLabels},
				Spec:       corev1.PodSpec{NodeName: nodeName},
			}
		}

		makeNode := func(name string, nodeLabels map[string]string) corev1.Node {
			return corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: name, Labels: nodeLabels},
			}
		}

		labelNames := []string{zoneLabel}

		It("reads pod failure domain keys from pod labels without consulting nodes", func() {
			pods := []corev1.Pod{
				makePod("pod-1", "node-1", map[string]string{zoneLabel: "az1"}),
				makePod("pod-2", "node-2", map[string]string{zoneLabel: "az2"}),
			}
			result := getPodsTopology(context.Background(), pods, nil, labelNames, nil)

			Expect(result.SuccessfullyExtracted).To(BeTrue())
			Expect(result.Instances["pod-1"][zoneLabel]).To(Equal("az1"))
			Expect(result.Instances["pod-2"][zoneLabel]).To(Equal("az2"))
			Expect(result.NodesUsed).To(BeEquivalentTo(2))
		})

		It("fails the extraction when a pod failure domain label is missing, without consulting the node", func() {
			pods := []corev1.Pod{
				makePod("pod-1", "node-1", map[string]string{zoneLabel: "az1"}),
				makePod("pod-2", "node-2", nil),
			}
			// the node carries the label: it must not be used as a fallback
			nodes := map[string]corev1.Node{
				"node-2": makeNode("node-2", map[string]string{zoneLabel: "az2"}),
			}
			result := getPodsTopology(context.Background(), pods, nodes, labelNames, nil)

			Expect(result.SuccessfullyExtracted).To(BeFalse())
		})

		It("keeps a pod label explicitly set to an empty value", func() {
			pods := []corev1.Pod{
				makePod("pod-1", "node-1", map[string]string{zoneLabel: ""}),
			}
			result := getPodsTopology(context.Background(), pods, nil, labelNames, nil)

			Expect(result.SuccessfullyExtracted).To(BeTrue())
			Expect(result.Instances["pod-1"]).To(HaveKeyWithValue(zoneLabel, ""))
		})

		It("reads node failure domain keys from node labels", func() {
			pods := []corev1.Pod{
				makePod("pod-1", "node-1", nil),
				makePod("pod-2", "node-2", nil),
			}
			nodes := map[string]corev1.Node{
				"node-1": makeNode("node-1", map[string]string{zoneLabel: "az1"}),
				"node-2": makeNode("node-2", map[string]string{zoneLabel: "az2"}),
			}
			result := getPodsTopology(context.Background(), pods, nodes, nil, labelNames)

			Expect(result.SuccessfullyExtracted).To(BeTrue())
			Expect(result.Instances["pod-1"][zoneLabel]).To(Equal("az1"))
			Expect(result.Instances["pod-2"][zoneLabel]).To(Equal("az2"))
		})

		It("ignores pod labels when reading node failure domain keys", func() {
			pods := []corev1.Pod{
				makePod("pod-1", "node-1", map[string]string{zoneLabel: "az9"}),
			}
			nodes := map[string]corev1.Node{
				"node-1": makeNode("node-1", map[string]string{zoneLabel: "az1"}),
			}
			result := getPodsTopology(context.Background(), pods, nodes, nil, labelNames)

			Expect(result.SuccessfullyExtracted).To(BeTrue())
			Expect(result.Instances["pod-1"][zoneLabel]).To(Equal("az1"))
		})

		It("returns empty topology when a node failure domain key is configured and the node is not found", func() {
			pods := []corev1.Pod{
				makePod("pod-1", "node-1", nil),
			}
			result := getPodsTopology(context.Background(), pods, nil, nil, labelNames)

			Expect(result.SuccessfullyExtracted).To(BeFalse())
		})

		It("returns successfully extracted topology when no labels are configured", func() {
			pods := []corev1.Pod{
				makePod("pod-1", "node-1", nil),
			}
			result := getPodsTopology(context.Background(), pods, nil, nil, nil)

			Expect(result.SuccessfullyExtracted).To(BeTrue())
			Expect(result.NodesUsed).To(BeEquivalentTo(1))
		})

		It("does not count unscheduled pods as used nodes", func() {
			pods := []corev1.Pod{
				makePod("pod-1", "node-1", nil),
				makePod("pod-2", "", nil),
			}
			result := getPodsTopology(context.Background(), pods, nil, nil, nil)

			Expect(result.SuccessfullyExtracted).To(BeTrue())
			Expect(result.NodesUsed).To(BeEquivalentTo(1))
		})
	})

	Describe("updateSyncReplicationTopologyCondition", func() {
		const zoneLabel = "topology.kubernetes.io/zone"

		makeCluster := func(
			failureDomainKeys []string,
			primary string,
			instances map[apiv1.PodName]apiv1.PodTopologyLabels,
			extracted bool,
		) *apiv1.Cluster {
			cluster := &apiv1.Cluster{}
			if len(failureDomainKeys) > 0 {
				cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
					Method:                apiv1.SynchronousReplicaConfigurationMethodAny,
					Number:                1,
					NodeFailureDomainKeys: failureDomainKeys,
				}
			}
			cluster.Status.CurrentPrimary = primary
			names := make([]string, 0, len(instances))
			for name := range instances {
				names = append(names, string(name))
			}
			cluster.Status.InstancesStatus = map[apiv1.PodStatus][]string{apiv1.PodHealthy: names}
			cluster.Status.Topology = apiv1.Topology{
				SuccessfullyExtracted: extracted,
				Instances:             instances,
			}
			return cluster
		}

		getCondition := func(cluster *apiv1.Cluster) *metav1.Condition {
			for i := range cluster.Status.Conditions {
				if cluster.Status.Conditions[i].Type == string(apiv1.ConditionSyncReplicationTopologySatisfied) {
					return &cluster.Status.Conditions[i]
				}
			}
			return nil
		}

		It("does not set the condition when no failure domain keys are configured", func() {
			cluster := makeCluster(nil, "pod-1", map[apiv1.PodName]apiv1.PodTopologyLabels{
				"pod-1": {zoneLabel: "az1"},
				"pod-2": {zoneLabel: "az2"},
			}, true)
			updateSyncReplicationTopologyCondition(cluster)
			Expect(getCondition(cluster)).To(BeNil())
		})

		It("removes a stale condition when the failure domain keys are removed", func() {
			cluster := makeCluster(nil, "pod-1", map[apiv1.PodName]apiv1.PodTopologyLabels{
				"pod-1": {zoneLabel: "az1"},
			}, true)
			meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
				Type:   string(apiv1.ConditionSyncReplicationTopologySatisfied),
				Status: metav1.ConditionFalse,
				Reason: string(apiv1.ConditionReasonInsufficientCrossDomainReplicas),
			})
			updateSyncReplicationTopologyCondition(cluster)
			Expect(getCondition(cluster)).To(BeNil())
		})

		It("sets condition to False with TopologyNotExtracted when extraction failed", func() {
			cluster := makeCluster([]string{zoneLabel}, "pod-1", nil, false)
			updateSyncReplicationTopologyCondition(cluster)
			cond := getCondition(cluster)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(string(apiv1.ConditionReasonTopologyNotExtracted)))
		})

		It("sets condition to False with TopologyNotExtracted when primary has no topology entry", func() {
			cluster := makeCluster([]string{zoneLabel}, "pod-1", map[apiv1.PodName]apiv1.PodTopologyLabels{
				"pod-2": {zoneLabel: "az2"},
			}, true)
			updateSyncReplicationTopologyCondition(cluster)
			cond := getCondition(cluster)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(string(apiv1.ConditionReasonTopologyNotExtracted)))
		})

		It("sets condition to True when a replica is in a different failure domain", func() {
			cluster := makeCluster([]string{zoneLabel}, "pod-1", map[apiv1.PodName]apiv1.PodTopologyLabels{
				"pod-1": {zoneLabel: "az1"},
				"pod-2": {zoneLabel: "az2"},
			}, true)
			updateSyncReplicationTopologyCondition(cluster)
			cond := getCondition(cluster)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal(string(apiv1.ConditionReasonTopologySatisfied)))
		})

		It("sets condition to False when all replicas are in the same failure domain as the primary", func() {
			cluster := makeCluster([]string{zoneLabel}, "pod-1", map[apiv1.PodName]apiv1.PodTopologyLabels{
				"pod-1": {zoneLabel: "az1"},
				"pod-2": {zoneLabel: "az1"},
			}, true)
			updateSyncReplicationTopologyCondition(cluster)
			cond := getCondition(cluster)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(string(apiv1.ConditionReasonInsufficientCrossDomainReplicas)))
		})

		It("sets condition to False when the only cross-domain replica is not electable", func() {
			cluster := makeCluster([]string{zoneLabel}, "pod-1", map[apiv1.PodName]apiv1.PodTopologyLabels{
				"pod-1": {zoneLabel: "az1"},
				"pod-2": {zoneLabel: "az1"},
				"pod-3": {zoneLabel: "az2"},
			}, true)
			// with preferred data durability only healthy replicas are
			// electable: the cross-domain pod-3 must not satisfy the condition
			cluster.Spec.PostgresConfiguration.Synchronous.DataDurability = apiv1.DataDurabilityLevelPreferred
			cluster.Status.InstancesStatus = map[apiv1.PodStatus][]string{
				apiv1.PodHealthy: {"pod-1", "pod-2"},
				apiv1.PodFailed:  {"pod-3"},
			}
			updateSyncReplicationTopologyCondition(cluster)
			cond := getCondition(cluster)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(string(apiv1.ConditionReasonInsufficientCrossDomainReplicas)))
		})

		It("sets condition to False when the cross-domain replicas are fewer than the requested number", func() {
			cluster := makeCluster([]string{zoneLabel}, "pod-1", map[apiv1.PodName]apiv1.PodTopologyLabels{
				"pod-1": {zoneLabel: "az1"},
				"pod-2": {zoneLabel: "az1"},
				"pod-3": {zoneLabel: "az2"},
			}, true)
			// with required data durability the constraint is applied only when
			// the cross-domain replicas cover the whole requested number
			cluster.Spec.PostgresConfiguration.Synchronous.Number = 2
			updateSyncReplicationTopologyCondition(cluster)
			cond := getCondition(cluster)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(string(apiv1.ConditionReasonInsufficientCrossDomainReplicas)))
		})

		It("sets condition to False when the cluster has no replicas", func() {
			cluster := makeCluster([]string{zoneLabel}, "primary-1", map[apiv1.PodName]apiv1.PodTopologyLabels{
				"primary-1": {zoneLabel: "az1"},
			}, true)
			updateSyncReplicationTopologyCondition(cluster)
			cond := getCondition(cluster)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(string(apiv1.ConditionReasonInsufficientCrossDomainReplicas)))
		})
	})
})
