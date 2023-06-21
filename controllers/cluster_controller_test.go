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
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Filtering cluster", func() {
	metrics := make(map[string]string, 1)
	metrics["a-secret"] = "test-version"

	cluster := apiv1.Cluster{
		Spec: apiv1.ClusterSpec{
			ImageName: "postgres:13.0",
		},
		Status: apiv1.ClusterStatus{
			SecretsResourceVersion:   apiv1.SecretsResourceVersion{Metrics: metrics},
			ConfigMapResourceVersion: apiv1.ConfigMapResourceVersion{Metrics: metrics},
		},
	}

	items := []apiv1.Cluster{cluster}
	clusterList := apiv1.ClusterList{Items: items}

	It("using a secret", func() {
		secret := corev1.Secret{}
		secret.Name = "a-secret"
		req := filterClustersUsingSecret(clusterList, &secret)
		Expect(req).ToNot(BeNil())
	})

	It("using a config map", func() {
		configMap := corev1.ConfigMap{}
		configMap.Name = "a-secret"
		req := filterClustersUsingConfigMap(clusterList, &configMap)
		Expect(req).ToNot(BeNil())
	})
})

var _ = Describe("Updating target primary", func() {
	It("selects the new target primary right away", func() {
		ctx := context.TODO()
		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace)

		By("creating the cluster resources")
		jobs := generateFakeInitDBJobs(clusterReconciler.Client, cluster)
		instances := generateFakeClusterPods(clusterReconciler.Client, cluster, true)
		pvc := generateClusterPVC(clusterReconciler.Client, cluster, persistentvolumeclaim.StatusReady)

		managedResources := &managedResources{
			nodes:     nil,
			instances: corev1.PodList{Items: instances},
			pvcs:      corev1.PersistentVolumeClaimList{Items: pvc},
			jobs:      batchv1.JobList{Items: jobs},
		}
		statusList := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					CurrentLsn:  postgres.LSN("0/0"),
					ReceivedLsn: postgres.LSN("0/0"),
					ReplayLsn:   postgres.LSN("0/0"),
					IsPodReady:  true,
					Pod:         instances[1],
				},
				{
					CurrentLsn:  postgres.LSN("0/0"),
					ReceivedLsn: postgres.LSN("0/0"),
					ReplayLsn:   postgres.LSN("0/0"),
					IsPodReady:  true,
					Pod:         instances[2],
				},
				{
					CurrentLsn:  postgres.LSN("0/0"),
					ReceivedLsn: postgres.LSN("0/0"),
					ReplayLsn:   postgres.LSN("0/0"),
					IsPodReady:  false,
					Pod:         instances[0],
				},
			},
		}

		By("creating the status list from the cluster pods", func() {
			cluster.Status.TargetPrimary = instances[0].Name
		})

		By("updating target primary pods for the cluster", func() {
			selectedPrimary, err := clusterReconciler.updateTargetPrimaryFromPods(
				ctx,
				cluster,
				statusList,
				managedResources,
			)

			Expect(err).ToNot(HaveOccurred())
			Expect(selectedPrimary).To(Equal(statusList.Items[0].Pod.Name))
		})
	})

	It("it should wait the failover delay to select the new target primary", func() {
		ctx := context.TODO()
		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace, func(cluster *apiv1.Cluster) {
			cluster.Spec.FailoverDelay = 2
		})

		By("creating the cluster resources")
		jobs := generateFakeInitDBJobs(clusterReconciler.Client, cluster)
		instances := generateFakeClusterPods(clusterReconciler.Client, cluster, true)
		pvc := generateClusterPVC(clusterReconciler.Client, cluster, persistentvolumeclaim.StatusReady)

		managedResources := &managedResources{
			nodes:     nil,
			instances: corev1.PodList{Items: instances},
			pvcs:      corev1.PersistentVolumeClaimList{Items: pvc},
			jobs:      batchv1.JobList{Items: jobs},
		}
		statusList := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					CurrentLsn:  postgres.LSN("0/0"),
					ReceivedLsn: postgres.LSN("0/0"),
					ReplayLsn:   postgres.LSN("0/0"),
					IsPodReady:  false,
					IsPrimary:   false,
					Pod:         instances[0],
				},
				{
					CurrentLsn:  postgres.LSN("0/0"),
					ReceivedLsn: postgres.LSN("0/0"),
					ReplayLsn:   postgres.LSN("0/0"),
					IsPodReady:  false,
					IsPrimary:   true,
					Pod:         instances[1],
				},
				{
					CurrentLsn:  postgres.LSN("0/0"),
					ReceivedLsn: postgres.LSN("0/0"),
					ReplayLsn:   postgres.LSN("0/0"),
					IsPodReady:  true,
					Pod:         instances[2],
				},
			},
		}

		By("creating the status list from the cluster pods", func() {
			cluster.Status.TargetPrimary = instances[1].Name
			cluster.Status.CurrentPrimary = instances[1].Name
		})

		By("returning the ErrWaitingOnFailOverDelay when first detecting the failure", func() {
			selectedPrimary, err := clusterReconciler.updateTargetPrimaryFromPodsPrimaryCluster(
				ctx,
				cluster,
				statusList,
				managedResources,
			)

			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(ErrWaitingOnFailOverDelay))
			Expect(selectedPrimary).To(Equal(""))
		})

		By("eventually updating the primary pod once the delay is elapsed", func() {
			Eventually(func(g Gomega) {
				selectedPrimary, err := clusterReconciler.updateTargetPrimaryFromPodsPrimaryCluster(
					ctx,
					cluster,
					statusList,
					managedResources,
				)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(selectedPrimary).To(Equal(statusList.Items[0].Pod.Name))
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})
	})

	It("Issue #1783: ensure that the scale-down behaviour remain consistent", func() {
		ctx := context.TODO()
		namespace := newFakeNamespace()
		cluster := newFakeCNPGCluster(namespace, func(cluster *apiv1.Cluster) {
			cluster.Spec.Instances = 2
			cluster.Status.LatestGeneratedNode = 2
			cluster.Status.ReadyInstances = 2
		})

		By("creating the cluster resources")
		jobs := generateFakeInitDBJobs(clusterReconciler.Client, cluster)
		instances := generateFakeClusterPods(clusterReconciler.Client, cluster, true)
		pvcs := generateClusterPVC(clusterReconciler.Client, cluster, persistentvolumeclaim.StatusReady)
		thirdInstancePVCGroup := newFakePVC(clusterReconciler.Client, cluster, 3, persistentvolumeclaim.StatusReady)
		pvcs = append(pvcs, thirdInstancePVCGroup...)

		cluster.Status.DanglingPVC = append(cluster.Status.DanglingPVC, thirdInstancePVCGroup[0].Name)

		managedResources := &managedResources{
			nodes:     nil,
			instances: corev1.PodList{Items: instances},
			pvcs:      corev1.PersistentVolumeClaimList{Items: pvcs},
			jobs:      batchv1.JobList{Items: jobs},
		}
		statusList := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					CurrentLsn:         postgres.LSN("0/0"),
					ReceivedLsn:        postgres.LSN("0/0"),
					ReplayLsn:          postgres.LSN("0/0"),
					IsPodReady:         true,
					IsPrimary:          false,
					Pod:                instances[0],
					MightBeUnavailable: false,
				},
				{
					CurrentLsn:         postgres.LSN("0/0"),
					ReceivedLsn:        postgres.LSN("0/0"),
					ReplayLsn:          postgres.LSN("0/0"),
					IsPodReady:         true,
					IsPrimary:          true,
					Pod:                instances[1],
					MightBeUnavailable: false,
				},
			},
		}

		By("triggering ensureInstancesAreCreated", func() {
			res, err := clusterReconciler.ensureInstancesAreCreated(ctx, cluster, managedResources, statusList)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal(reconcile.Result{RequeueAfter: time.Second}))
		})

		By("checking that the third instance exists even if the cluster has two instances", func() {
			var expectedPod corev1.Pod
			instanceName := specs.GetInstanceName(cluster.Name, 3)
			err := clusterReconciler.Client.Get(ctx, types.NamespacedName{
				Name:      instanceName,
				Namespace: cluster.Namespace,
			}, &expectedPod)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

var _ = Describe("object metadata test", func() {
	makeReconciler := func(pods []corev1.Pod) *ClusterReconciler {
		builder := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme())

		for idx := range pods {
			builder = builder.WithObjects(&pods[idx])
		}

		return &ClusterReconciler{Client: builder.Build()}
	}

	getPod := func(re *ClusterReconciler, name string) *corev1.Pod {
		pod := &corev1.Pod{}
		err := re.Client.Get(context.Background(), types.NamespacedName{Name: name}, pod)
		Expect(err).ToNot(HaveOccurred())
		return pod
	}

	Context("updateRoleLabelsOnPods", func() {
		It("Should update the role labels correctly", func() {
			cluster := &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					CurrentPrimary: "primaryPod",
				},
			}

			pods := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "primaryPod",
						Labels: map[string]string{},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "replicaPod",
						Labels: map[string]string{},
					},
				},
			}

			re := makeReconciler(pods)
			err := re.updateRoleLabelsOnPods(context.Background(), cluster, corev1.PodList{Items: pods})
			Expect(err).ToNot(HaveOccurred())

			primaryPod := getPod(re, pods[0].Name)
			Expect(primaryPod.Labels[specs.ClusterRoleLabelName]).To(Equal(specs.ClusterRoleLabelPrimary))

			replicaPod := getPod(re, pods[1].Name)
			Expect(replicaPod.Labels[specs.ClusterRoleLabelName]).To(Equal(specs.ClusterRoleLabelReplica))
		})

		It("Should update the role labels when the primary and the replica switch roles", func() {
			cluster := &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					CurrentPrimary: "newPrimaryPod",
				},
			}

			pods := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "newPrimaryPod",
						Labels: map[string]string{
							specs.ClusterRoleLabelName: specs.ClusterRoleLabelReplica,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "newReplicaPod",
						Labels: map[string]string{
							specs.ClusterRoleLabelName: specs.ClusterRoleLabelPrimary,
						},
					},
				},
			}

			re := makeReconciler(pods)
			err := re.updateRoleLabelsOnPods(context.Background(), cluster, corev1.PodList{Items: pods})
			Expect(err).ToNot(HaveOccurred())

			newPrimaryPod := getPod(re, pods[0].Name)
			Expect(newPrimaryPod.Labels[specs.ClusterRoleLabelName]).To(Equal(specs.ClusterRoleLabelPrimary))

			newReplicaPod := getPod(re, pods[1].Name)
			Expect(newReplicaPod.Labels[specs.ClusterRoleLabelName]).To(Equal(specs.ClusterRoleLabelReplica))
		})

		It("Should not perform role reconciliation when there is no current primary", func() {
			cluster := &apiv1.Cluster{}

			pods := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "oldPrimaryPod",
						Labels: map[string]string{specs.ClusterRoleLabelName: specs.ClusterRoleLabelPrimary},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "oldReplicaPod",
						Labels: map[string]string{specs.ClusterRoleLabelName: specs.ClusterRoleLabelReplica},
					},
				},
			}

			re := makeReconciler(pods)
			err := re.updateRoleLabelsOnPods(context.Background(), cluster, corev1.PodList{Items: pods})
			Expect(err).ToNot(HaveOccurred())

			// Labels of pods should not have been updated
			for _, pod := range pods {
				updatedPod := getPod(re, pod.Name)
				Expect(updatedPod.Labels[specs.ClusterRoleLabelName]).To(Equal(pod.Labels[specs.ClusterRoleLabelName]))
			}
		})
	})

	Context("updateOperatorLabelsOnInstances", func() {
		const instanceName = "instance1"
		It("Should create labels if the instance has no labels", func() {
			instances := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: instanceName,
					},
				},
			}

			re := makeReconciler(instances)
			err := re.updateOperatorLabelsOnInstances(context.Background(), corev1.PodList{Items: instances})
			Expect(err).ToNot(HaveOccurred())

			updatedInstance := getPod(re, instances[0].Name)
			Expect(updatedInstance.Labels[utils.InstanceNameLabelName]).To(Equal(instances[0].Name))
			Expect(updatedInstance.Labels[utils.PodRoleLabelName]).To(Equal(string(utils.PodRoleInstance)))
		})

		It("Should not update labels if the instance already has the correct labels", func() {
			instances := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: instanceName,
						Labels: map[string]string{
							utils.InstanceNameLabelName: instanceName,
							utils.PodRoleLabelName:      string(utils.PodRoleInstance),
						},
					},
				},
			}

			re := makeReconciler(instances)
			err := re.updateOperatorLabelsOnInstances(context.Background(), corev1.PodList{Items: instances})
			Expect(err).ToNot(HaveOccurred())

			// Labels of instances should not have been updated
			for _, instance := range instances {
				updatedInstance := getPod(re, instance.Name)
				Expect(updatedInstance.Labels).To(Equal(instance.Labels))
			}
		})

		It("Should update name label if it's incorrect", func() {
			instances := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: instanceName,
						Labels: map[string]string{
							utils.InstanceNameLabelName: "incorrectName",
							utils.PodRoleLabelName:      string(utils.PodRoleInstance),
						},
					},
				},
			}

			re := makeReconciler(instances)
			err := re.updateOperatorLabelsOnInstances(context.Background(), corev1.PodList{Items: instances})
			Expect(err).ToNot(HaveOccurred())

			updatedInstance := getPod(re, instances[0].Name)
			Expect(updatedInstance.Labels[utils.InstanceNameLabelName]).To(Equal(instanceName))
		})

		It("Should update role label if it's incorrect", func() {
			instances := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: instanceName,
						Labels: map[string]string{
							utils.InstanceNameLabelName: instanceName,
							utils.PodRoleLabelName:      "incorrectRole",
						},
					},
				},
			}

			re := makeReconciler(instances)
			err := re.updateOperatorLabelsOnInstances(context.Background(), corev1.PodList{Items: instances})
			Expect(err).ToNot(HaveOccurred())

			updatedInstance := getPod(re, instances[0].Name)
			Expect(updatedInstance.Labels[utils.PodRoleLabelName]).To(Equal(string(utils.PodRoleInstance)))
		})

		It("Should correctly update labels for multiple instances", func() {
			instances := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: instanceName,
						Labels: map[string]string{
							utils.InstanceNameLabelName: "incorrectName1",
							utils.PodRoleLabelName:      string(utils.PodRoleInstance),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "instance2",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "instance3",
						Labels: map[string]string{
							utils.InstanceNameLabelName: "instance3",
							utils.PodRoleLabelName:      "incorrectRole",
						},
					},
				},
			}

			re := makeReconciler(instances)
			err := re.updateOperatorLabelsOnInstances(context.Background(), corev1.PodList{Items: instances})
			Expect(err).ToNot(HaveOccurred())

			for _, instance := range instances {
				updatedInstance := getPod(re, instance.Name)
				Expect(updatedInstance.Labels[utils.InstanceNameLabelName]).To(Equal(updatedInstance.Name))
				Expect(updatedInstance.Labels[utils.PodRoleLabelName]).To(Equal(string(utils.PodRoleInstance)))
			}
		})
	})

	Context("updateClusterLabelsOnPods", func() {
		const (
			labelKey      = "label1"
			labelValue    = "value1"
			labelKeyTwo   = "label2"
			labelValueTwo = "value2"
		)

		It("Should correctly add missing labels from cluster to pods", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					InheritedMetadata: &apiv1.EmbeddedObjectMetadata{
						Labels: map[string]string{
							labelKey:    labelValue,
							labelKeyTwo: labelValueTwo,
						},
					},
				},
			}

			pods := corev1.PodList{
				Items: []corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pod1",
							Labels: map[string]string{
								labelKey: labelValue,
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pod2",
						},
					},
				},
			}

			re := makeReconciler(pods.Items)
			err := re.updateClusterLabelsOnPods(context.Background(), cluster, pods)
			Expect(err).ToNot(HaveOccurred())

			for _, pod := range pods.Items {
				updatedPod := getPod(re, pod.Name)
				Expect(updatedPod.Labels).To(Equal(cluster.GetFixedInheritedLabels()))
			}
		})

		It("Should not change labels if they already match the cluster's", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					InheritedMetadata: &apiv1.EmbeddedObjectMetadata{
						Labels: map[string]string{
							labelKey:    labelValue,
							labelKeyTwo: labelValueTwo,
						},
					},
				},
			}
			pods := corev1.PodList{
				Items: []corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pod1",
							Labels: map[string]string{
								labelKey:    labelValue,
								labelKeyTwo: labelValueTwo,
							},
						},
					},
				},
			}

			Expect(cluster.Spec.InheritedMetadata.Labels).To(Equal(cluster.GetFixedInheritedLabels()))

			re := makeReconciler(pods.Items)
			err := re.updateClusterLabelsOnPods(context.Background(), cluster, pods)
			Expect(err).ToNot(HaveOccurred())

			updatedPod := getPod(re, pods.Items[0].Name)
			Expect(updatedPod.Labels).To(Equal(cluster.GetFixedInheritedLabels()))
		})

		It("Should correctly handle the case of no fixed inherited labels from the cluster", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					InheritedMetadata: &apiv1.EmbeddedObjectMetadata{},
				},
			}
			pods := corev1.PodList{
				Items: []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "pod1"}}},
			}
			re := makeReconciler(pods.Items)
			err := re.updateClusterLabelsOnPods(context.Background(), cluster, pods)
			Expect(err).ToNot(HaveOccurred())

			updatedPod := getPod(re, pods.Items[0].Name)
			Expect(updatedPod.Labels).To(BeEmpty())
		})
	})

	Context("updateClusterAnnotationsOnPods", func() {
		const key = "annotation1"
		const value = "value1"

		It("Should correctly add missing annotations from cluster to pods", func() {
			pods := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod1",
					},
				},
			}
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					InheritedMetadata: &apiv1.EmbeddedObjectMetadata{
						Annotations: map[string]string{key: value},
					},
				},
			}
			re := makeReconciler(pods)
			err := re.updateClusterAnnotationsOnPods(context.Background(), cluster, corev1.PodList{Items: pods})
			Expect(err).NotTo(HaveOccurred())

			updatedPod := getPod(re, pods[0].Name)
			Expect(updatedPod.Annotations[key]).To(Equal(value))
		})

		It("Should not change annotations if they already match the cluster's", func() {
			pods := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "pod1",
						Annotations: map[string]string{key: value},
					},
				},
			}
			cluster := &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{},
				Spec: apiv1.ClusterSpec{
					InheritedMetadata: &apiv1.EmbeddedObjectMetadata{
						Annotations: map[string]string{key: value},
					},
				},
			}

			Expect(cluster.Spec.InheritedMetadata.Annotations).To(Equal(cluster.GetFixedInheritedAnnotations()))

			re := makeReconciler(pods)
			err := re.updateClusterAnnotationsOnPods(context.Background(), cluster, corev1.PodList{Items: pods})
			Expect(err).NotTo(HaveOccurred())

			updatedPod := getPod(re, pods[0].Name)
			Expect(updatedPod.Annotations).To(HaveLen(1))
			Expect(updatedPod.Annotations[key]).To(Equal(value))
		})

		It("Should correctly add AppArmor annotations if present in the cluster's annotations", func() {
			const (
				key   = "container.apparmor.security.beta.kubernetes.io/pod"
				value = "runtime/default"
			)

			pods := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod1",
					},
				},
			}

			cluster := &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{key: value},
				},
			}
			re := makeReconciler(pods)
			err := re.updateClusterAnnotationsOnPods(context.Background(), cluster, corev1.PodList{Items: pods})
			Expect(err).NotTo(HaveOccurred())

			updatedPod := getPod(re, pods[0].Name)
			Expect(updatedPod.Annotations[key]).To(Equal(value))
		})

		It("Should correctly handle the case of no fixed inherited annotations from the cluster", func() {
			pods := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod1",
					},
				},
			}
			cluster := &apiv1.Cluster{}
			re := makeReconciler(pods)
			err := re.updateClusterAnnotationsOnPods(context.Background(), cluster, corev1.PodList{Items: pods})
			Expect(err).NotTo(HaveOccurred())

			updatedPod := getPod(re, pods[0].Name)
			Expect(updatedPod.Annotations).To(BeEmpty())
		})
	})
})
