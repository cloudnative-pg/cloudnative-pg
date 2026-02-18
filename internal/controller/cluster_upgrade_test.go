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
	"encoding/json"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin"
	pluginClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	rolloutManager "github.com/cloudnative-pg/cloudnative-pg/internal/controller/rollout"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pod upgrade", Ordered, func() {
	const (
		newOperatorImage = "ghcr.io/cloudnative-pg/cloudnative-pg:next"
	)

	var cluster apiv1.Cluster

	BeforeEach(func() {
		cluster = apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:13.11",
			},
		}
		configuration.Current = configuration.NewConfiguration()
	})

	AfterAll(func() {
		configuration.Current = configuration.NewConfiguration()
	})

	It("will not require a restart for just created Pods", func(ctx SpecContext) {
		pod, err := specs.NewInstance(ctx, cluster, 1, true)
		Expect(err).ToNot(HaveOccurred())

		status := postgres.PostgresqlStatus{
			Pod:            pod,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}

		rollout := isInstanceNeedingRollout(ctx, status, &cluster)
		Expect(rollout.reason).To(BeEmpty())
		Expect(rollout.required).To(BeFalse())
	})

	It("requires rollout when running a different image name", func(ctx SpecContext) {
		pod, err := specs.NewInstance(ctx, cluster, 1, true)
		Expect(err).ToNot(HaveOccurred())

		pod.Spec.Containers[0].Image = "postgres:13.10"
		status := postgres.PostgresqlStatus{
			Pod:            pod,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}
		rollout := isInstanceNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.reason).To(BeEquivalentTo("the instance is using a different image: postgres:13.10 -> postgres:13.11"))
		Expect(rollout.needsChangeOperandImage).To(BeTrue())
		Expect(rollout.needsChangeOperatorImage).To(BeFalse())
	})

	It("requires rollout when a restart annotation has been added to the cluster", func(ctx SpecContext) {
		pod, err := specs.NewInstance(ctx, cluster, 1, true)
		Expect(err).ToNot(HaveOccurred())
		clusterRestart := cluster
		clusterRestart.Annotations = make(map[string]string)
		clusterRestart.Annotations[utils.ClusterRestartAnnotationName] = "now"

		status := postgres.PostgresqlStatus{
			Pod:            pod,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}

		rollout := isInstanceNeedingRollout(ctx, status, &clusterRestart)
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.reason).To(Equal("cluster has been explicitly restarted via annotation"))
		Expect(rollout.canBeInPlace).To(BeFalse())
		Expect(rollout.needsChangeOperandImage).To(BeFalse())
		Expect(rollout.needsChangeOperatorImage).To(BeFalse())

		rollout = isInstanceNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeFalse())
		Expect(rollout.reason).To(BeEmpty())
	})

	It("should prioritize full rollout over inplace restarts", func(ctx SpecContext) {
		pod, err := specs.NewInstance(ctx, cluster, 1, true)
		Expect(err).ToNot(HaveOccurred())

		status := postgres.PostgresqlStatus{
			Pod:            pod,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}

		rollout := isInstanceNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeFalse())
		Expect(rollout.reason).To(BeEmpty())

		// Set a different image to trigger a full rollout
		pod.Spec.Containers[0].Image = "postgres:13.10"

		// Set pending restart to true in the status
		status = postgres.PostgresqlStatus{
			Pod:            pod,
			IsPodReady:     true,
			PendingRestart: true,
			ExecutableHash: "test_hash",
		}

		rollout = isInstanceNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.canBeInPlace).To(BeFalse())
		Expect(rollout.reason).To(Equal("the instance is using a different image: postgres:13.10 -> postgres:13.11"))
	})

	It("requires rollout when PostgreSQL needs to be restarted", func(ctx SpecContext) {
		pod, err := specs.NewInstance(ctx, cluster, 1, true)
		Expect(err).ToNot(HaveOccurred())

		status := postgres.PostgresqlStatus{
			Pod:            pod,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}

		rollout := isInstanceNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeFalse())
		Expect(rollout.reason).To(BeEmpty())

		status = postgres.PostgresqlStatus{
			Pod:            pod,
			IsPodReady:     true,
			PendingRestart: true,
			ExecutableHash: "test_hash",
		}
		rollout = isInstanceNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.reason).To(Equal("Postgres needs a restart to apply some configuration changes"))
		Expect(rollout.needsChangeOperandImage).To(BeFalse())
		Expect(rollout.needsChangeOperatorImage).To(BeFalse())
	})

	It("requires pod rollout if executable does not have a hash", func(ctx SpecContext) {
		pod, err := specs.NewInstance(ctx, cluster, 1, true)
		Expect(err).ToNot(HaveOccurred())
		status := postgres.PostgresqlStatus{
			Pod:            pod,
			PendingRestart: false,
			IsPodReady:     true,
		}
		rollout := isInstanceNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.reason).To(Equal("pod 'test-1' is not reporting the executable hash"))
		Expect(rollout.canBeInPlace).To(BeFalse())
		Expect(rollout.needsChangeOperandImage).To(BeFalse())
		Expect(rollout.needsChangeOperatorImage).To(BeTrue())
	})

	It("checkPodSpecIsOutdated should not return any error", func() {
		pod, err := specs.NewInstance(context.TODO(), cluster, 1, true)
		Expect(err).ToNot(HaveOccurred())
		rollout, err := checkPodSpecIsOutdated(context.TODO(), pod, &cluster)
		Expect(rollout.required).To(BeFalse())
		Expect(rollout.canBeInPlace).To(BeFalse())
		Expect(rollout.reason).To(BeEmpty())
		Expect(err).NotTo(HaveOccurred())
	})

	It("checks when a rollout is needed for any reason", func(ctx SpecContext) {
		pod, err := specs.NewInstance(ctx, cluster, 1, true)
		Expect(err).ToNot(HaveOccurred())
		status := postgres.PostgresqlStatus{
			Pod:            pod,
			PendingRestart: true,
		}
		rollout := isInstanceNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeFalse())
		Expect(rollout.canBeInPlace).To(BeFalse())
		Expect(rollout.reason).To(BeEmpty())

		status = postgres.PostgresqlStatus{
			Pod:            pod,
			PendingRestart: true,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}
		rollout = isInstanceNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.reason).To(BeEquivalentTo("Postgres needs a restart to apply some configuration changes"))
		Expect(rollout.canBeInPlace).To(BeTrue())
		Expect(rollout.needsChangeOperandImage).To(BeFalse())
		Expect(rollout.needsChangeOperatorImage).To(BeFalse())
	})

	When("the PodSpec annotation is not available", func() {
		It("should trigger a rollout when the scheduler changes", func(ctx SpecContext) {
			pod, err := specs.NewInstance(ctx, cluster, 1, true)
			Expect(err).ToNot(HaveOccurred())
			cluster.Spec.SchedulerName = "newScheduler"
			delete(pod.Annotations, utils.PodSpecAnnotationName)

			status := postgres.PostgresqlStatus{
				Pod:            pod,
				PendingRestart: false,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}

			rollout := isInstanceNeedingRollout(ctx, status, &cluster)
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.reason).To(ContainSubstring("scheduler name changed"))
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeFalse())
		})
	})

	It("should trigger a rollout when the scheduler changes", func(ctx SpecContext) {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:13.11",
			},
		}
		pod, err := specs.NewInstance(ctx, cluster, 1, true)
		Expect(err).ToNot(HaveOccurred())
		cluster.Spec.SchedulerName = "newScheduler"

		status := postgres.PostgresqlStatus{
			Pod:            pod,
			PendingRestart: false,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}

		rollout := isInstanceNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.reason).To(ContainSubstring("scheduler-name"))
		Expect(rollout.needsChangeOperandImage).To(BeFalse())
		Expect(rollout.needsChangeOperatorImage).To(BeFalse())
	})

	When("cluster has resources specified", func() {
		clusterWithResources := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:13.0",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{"storage": resource.MustParse("1Gi")},
					Limits: corev1.ResourceList{
						"cpu":    resource.MustParse("2"),
						"memory": resource.MustParse("1Gi"),
					},
				},
			},
		}
		It("should trigger a rollout when the cluster has a Resource changed", func(ctx SpecContext) {
			pod, err := specs.NewInstance(
				context.TODO(),
				clusterWithResources,
				1,
				true,
			)
			Expect(err).ToNot(HaveOccurred())
			clusterWithResources.Spec.Resources.Limits["cpu"] = resource.MustParse("3") // was "2"

			status := postgres.PostgresqlStatus{
				Pod:            pod,
				PendingRestart: false,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}

			rollout := isInstanceNeedingRollout(ctx, status, &clusterWithResources)
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.reason).To(ContainSubstring("original and target PodSpec differ in containers"))
			Expect(rollout.reason).To(ContainSubstring("container postgres differs in resources"))
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeFalse())
		})
		It("should trigger a rollout when the cluster has Resources deleted from spec", func(ctx SpecContext) {
			pod, err := specs.NewInstance(context.TODO(), clusterWithResources, 1, true)
			Expect(err).ToNot(HaveOccurred())
			clusterWithResources.Spec.Resources = corev1.ResourceRequirements{}

			status := postgres.PostgresqlStatus{
				Pod:            pod,
				PendingRestart: false,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}

			rollout := isInstanceNeedingRollout(ctx, status, &clusterWithResources)
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.reason).To(ContainSubstring("original and target PodSpec differ in containers"))
			Expect(rollout.reason).To(ContainSubstring("container postgres differs in resources"))
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeFalse())
		})
	})

	When("the PodSpec annotation is not available", func() {
		It("detects when a new custom environment variable is set", func(ctx SpecContext) {
			pod, err := specs.NewInstance(ctx, cluster, 1, true)
			Expect(err).ToNot(HaveOccurred())
			delete(pod.Annotations, utils.PodSpecAnnotationName)

			cluster := cluster.DeepCopy()
			cluster.Spec.Env = []corev1.EnvVar{
				{
					Name:  "TEST",
					Value: "test",
				},
			}

			status := postgres.PostgresqlStatus{
				Pod:            pod,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}

			rollout := isInstanceNeedingRollout(ctx, status, cluster)
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.reason).To(Equal("environment variable configuration hash changed"))
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeFalse())
		})

		It("should not trigger a rollout on operator changes with inplace upgrades", func(ctx SpecContext) {
			cluster := apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageName: "postgres:13.11",
				},
			}
			pod, err := specs.NewInstance(ctx, cluster, 1, true)
			Expect(err).ToNot(HaveOccurred())
			delete(pod.Annotations, utils.PodSpecAnnotationName)

			status := postgres.PostgresqlStatus{
				Pod:            pod,
				PendingRestart: false,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}

			// let's simulate an operator upgrade, with online upgrades allowed
			configuration.Current.OperatorImageName = newOperatorImage
			configuration.Current.EnableInstanceManagerInplaceUpdates = true
			rollout := isInstanceNeedingRollout(ctx, status, &cluster)
			Expect(rollout.reason).To(BeEmpty())
			Expect(rollout.required).To(BeFalse())
		})

		It("should trigger an explicit rollout if operator changes without inplace upgrades", func(ctx SpecContext) {
			cluster := apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageName: "postgres:13.11",
				},
			}
			pod, err := specs.NewInstance(ctx, cluster, 1, true)
			Expect(err).ToNot(HaveOccurred())
			delete(pod.Annotations, utils.PodSpecAnnotationName)

			status := postgres.PostgresqlStatus{
				Pod:            pod,
				PendingRestart: false,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}

			// let's simulate an operator upgrade, with online upgrades allowed
			configuration.Current.OperatorImageName = newOperatorImage
			configuration.Current.EnableInstanceManagerInplaceUpdates = false
			rollout := isInstanceNeedingRollout(ctx, status, &cluster)
			Expect(rollout.reason).To(ContainSubstring("the instance is using an old bootstrap container image"))
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeTrue())
		})
	})

	When("the podSpec annotation is available", func() {
		It("detects when a new custom environment variable is set", func(ctx SpecContext) {
			pod, err := specs.NewInstance(ctx, cluster, 1, true)
			Expect(err).ToNot(HaveOccurred())

			cluster := cluster.DeepCopy()
			cluster.Spec.Env = []corev1.EnvVar{
				{
					Name:  "TEST",
					Value: "test",
				},
			}

			status := postgres.PostgresqlStatus{
				Pod:            pod,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}

			rollout := isInstanceNeedingRollout(ctx, status, cluster)
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.reason).To(ContainSubstring("original and target PodSpec differ in containers"))
			Expect(rollout.reason).To(ContainSubstring("container postgres differs in environment"))
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeFalse())
		})

		It("should not trigger a rollout on operator changes with inplace upgrades", func(ctx SpecContext) {
			cluster := apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageName: "postgres:13.11",
				},
			}
			pod, err := specs.NewInstance(ctx, cluster, 1, true)
			Expect(err).ToNot(HaveOccurred())

			status := postgres.PostgresqlStatus{
				Pod:            pod,
				PendingRestart: false,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}

			// let's simulate an operator upgrade, with online upgrades allowed
			configuration.Current.OperatorImageName = newOperatorImage
			configuration.Current.EnableInstanceManagerInplaceUpdates = true
			rollout := isInstanceNeedingRollout(ctx, status, &cluster)
			Expect(rollout.reason).To(BeEmpty())
			Expect(rollout.required).To(BeFalse())
		})

		It("should trigger an explicit rollout if operator changes without inplace upgrades", func(ctx SpecContext) {
			cluster := apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageName: "postgres:13.11",
				},
			}
			pod, err := specs.NewInstance(ctx, cluster, 1, true)
			Expect(err).ToNot(HaveOccurred())

			status := postgres.PostgresqlStatus{
				Pod:            pod,
				PendingRestart: false,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}

			// let's simulate an operator upgrade, with online upgrades allowed
			configuration.Current.OperatorImageName = newOperatorImage
			configuration.Current.EnableInstanceManagerInplaceUpdates = false
			rollout := isInstanceNeedingRollout(ctx, status, &cluster)
			Expect(rollout.reason).To(ContainSubstring("the instance is using an old bootstrap container image"))
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeTrue())
		})
	})

	When("The projected volume changed", func() {
		It("should not require rollout if projected volume is 0 length slice in cluster",
			func(ctx SpecContext) {
				cluster.Spec.ProjectedVolumeTemplate = &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{},
				}
				pod, err := specs.NewInstance(ctx, cluster, 1, true)
				Expect(err).ToNot(HaveOccurred())
				status := postgres.PostgresqlStatus{
					Pod:            pod,
					IsPodReady:     true,
					ExecutableHash: "test",
				}

				rollout := isInstanceNeedingRollout(ctx, status, &cluster)
				Expect(rollout.reason).To(BeEmpty())
				Expect(rollout.required).To(BeFalse())
			})

		It("should not require rollout if projected volume source is nil",
			func(ctx SpecContext) {
				cluster.Spec.ProjectedVolumeTemplate = &corev1.ProjectedVolumeSource{
					Sources: nil,
				}
				pod, err := specs.NewInstance(ctx, cluster, 1, true)
				Expect(err).ToNot(HaveOccurred())
				status := postgres.PostgresqlStatus{
					Pod:            pod,
					IsPodReady:     true,
					ExecutableHash: "test",
				}

				rollout := isInstanceNeedingRollout(ctx, status, &cluster)
				Expect(rollout.reason).To(BeEmpty())
				Expect(rollout.required).To(BeFalse())
			})

		It("should not require rollout if projected volume  is nil",
			func(ctx SpecContext) {
				cluster.Spec.ProjectedVolumeTemplate = nil
				pod, err := specs.NewInstance(ctx, cluster, 1, true)
				Expect(err).ToNot(HaveOccurred())
				status := postgres.PostgresqlStatus{
					Pod:            pod,
					IsPodReady:     true,
					ExecutableHash: "test",
				}

				rollout := isInstanceNeedingRollout(ctx, status, &cluster)
				Expect(rollout.reason).To(BeEmpty())
				Expect(rollout.required).To(BeFalse())
			})
	})
})

var _ = Describe("Test pod rollout due to topology", func() {
	var cluster *apiv1.Cluster
	var pod *corev1.Pod

	BeforeEach(func() {
		topology := corev1.TopologySpreadConstraint{
			MaxSkew:           1,
			TopologyKey:       "zone",
			WhenUnsatisfiable: corev1.DoNotSchedule,
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-app"},
			},
		}
		cluster = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{topology},
			},
		}
		var err error
		pod, err = specs.NewInstance(context.TODO(), *cluster, 1, true)
		Expect(err).ToNot(HaveOccurred())
	})

	When("the original podSpec annotation is available", func() {
		It("should not require rollout when cluster and pod have the same TopologySpreadConstraints",
			func(ctx SpecContext) {
				status := postgres.PostgresqlStatus{
					Pod:            pod,
					IsPodReady:     true,
					ExecutableHash: "test_hash",
				}
				rollout := isInstanceNeedingRollout(ctx, status, cluster)
				Expect(rollout.reason).To(BeEmpty())
				Expect(rollout.required).To(BeFalse())
			})

		It("should require rollout when the cluster and pod do not have "+
			"the same TopologySpreadConstraints", func(ctx SpecContext) {
			cluster.Spec.TopologySpreadConstraints[0].MaxSkew = 2
			status := postgres.PostgresqlStatus{
				Pod:            pod,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}
			rollout := isInstanceNeedingRollout(ctx, status, cluster)
			Expect(rollout.reason).To(ContainSubstring("topology-spread-constraints"))
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeFalse())
		})

		It("should require rollout when the LabelSelector maps are different", func(ctx SpecContext) {
			cluster.Spec.TopologySpreadConstraints[0].LabelSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "different-app"},
			}

			status := postgres.PostgresqlStatus{
				Pod:            pod,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}
			rollout := isInstanceNeedingRollout(ctx, status, cluster)
			Expect(rollout.reason).To(ContainSubstring("topology-spread-constraints"))
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeFalse())
		})

		It("should require rollout when TopologySpreadConstraints is nil in one of the objects", func(ctx SpecContext) {
			cluster.Spec.TopologySpreadConstraints = nil

			status := postgres.PostgresqlStatus{
				Pod:            pod,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}
			rollout := isInstanceNeedingRollout(ctx, status, cluster)
			Expect(rollout.reason).To(ContainSubstring("topology-spread-constraints"))
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeFalse())
		})

		It("should not require rollout if pod and spec both lack TopologySpreadConstraints", func(ctx SpecContext) {
			cluster.Spec.TopologySpreadConstraints = nil
			var err error
			pod, err = specs.NewInstance(context.TODO(), *cluster, 1, true)
			Expect(err).ToNot(HaveOccurred())
			Expect(pod.Spec.TopologySpreadConstraints).To(BeNil())

			status := postgres.PostgresqlStatus{
				Pod:            pod,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}
			rollout := isInstanceNeedingRollout(ctx, status, cluster)
			Expect(rollout.reason).To(BeEmpty())
			Expect(rollout.required).To(BeFalse())
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeFalse())
		})
	})

	When("the original podSpec annotation is not available", func() {
		It("should not require rollout when cluster and pod have the same TopologySpreadConstraints", func(ctx SpecContext) {
			status := postgres.PostgresqlStatus{
				Pod:            pod,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}
			rollout := isInstanceNeedingRollout(ctx, status, cluster)
			Expect(rollout.reason).To(BeEmpty())
			Expect(rollout.required).To(BeFalse())
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeFalse())
		})

		It("should require rollout when the cluster and pod do not have "+
			"the same TopologySpreadConstraints", func(ctx SpecContext) {
			pod2 := pod.DeepCopy()
			pod2.Spec.TopologySpreadConstraints[0].MaxSkew = 2
			delete(pod2.Annotations, utils.PodSpecAnnotationName)
			status := postgres.PostgresqlStatus{
				Pod:            pod2,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}
			rollout := isInstanceNeedingRollout(ctx, status, cluster)
			Expect(rollout.reason).To(ContainSubstring("does not have up-to-date TopologySpreadConstraints"))
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeFalse())
		})

		It("should require rollout when the LabelSelector maps are different", func(ctx SpecContext) {
			pod2 := pod.DeepCopy()
			pod2.Spec.TopologySpreadConstraints[0].LabelSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "different-app"},
			}
			delete(pod2.Annotations, utils.PodSpecAnnotationName)

			status := postgres.PostgresqlStatus{
				Pod:            pod2,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}
			rollout := isInstanceNeedingRollout(ctx, status, cluster)
			Expect(rollout.reason).To(ContainSubstring("does not have up-to-date TopologySpreadConstraints"))
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeFalse())
		})

		It("should require rollout when TopologySpreadConstraints is nil in one of the objects", func(ctx SpecContext) {
			pod2 := pod.DeepCopy()
			pod2.Spec.TopologySpreadConstraints = nil
			delete(pod2.Annotations, utils.PodSpecAnnotationName)

			status := postgres.PostgresqlStatus{
				Pod:            pod2,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}
			rollout := isInstanceNeedingRollout(ctx, status, cluster)
			Expect(rollout.reason).To(ContainSubstring("does not have up-to-date TopologySpreadConstraints"))
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeFalse())
		})

		It("should not require rollout if pod and spec both lack TopologySpreadConstraints", func(ctx SpecContext) {
			cluster.Spec.TopologySpreadConstraints = nil
			pod2 := pod.DeepCopy()
			pod2.Spec.TopologySpreadConstraints = nil
			delete(pod2.Annotations, utils.PodSpecAnnotationName)

			status := postgres.PostgresqlStatus{
				Pod:            pod2,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}
			rollout := isInstanceNeedingRollout(ctx, status, cluster)
			Expect(rollout.reason).To(BeEmpty())
			Expect(rollout.required).To(BeFalse())
			Expect(rollout.needsChangeOperandImage).To(BeFalse())
			Expect(rollout.needsChangeOperatorImage).To(BeFalse())
		})
	})
})

var _ = Describe("hasValidPodSpec", func() {
	var pod *corev1.Pod

	BeforeEach(func() {
		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
		}
	})

	Context("when the PodSpecAnnotation is absent", func() {
		It("should return false", func() {
			Expect(hasValidPodSpec(pod)).To(BeFalse())
		})
	})

	Context("when the PodSpecAnnotation is present", func() {
		Context("and the PodSpecAnnotation is valid", func() {
			It("should return true", func() {
				podSpec := &corev1.PodSpec{}
				podSpecBytes, _ := json.Marshal(podSpec)
				pod.Annotations[utils.PodSpecAnnotationName] = string(podSpecBytes)
				Expect(hasValidPodSpec(pod)).To(BeTrue())
			})
		})

		Context("and the PodSpecAnnotation is invalid", func() {
			It("should return false", func() {
				pod.Annotations[utils.PodSpecAnnotationName] = "invalid JSON"
				Expect(hasValidPodSpec(pod)).To(BeFalse())
			})
		})
	})
})

var _ = Describe("Cluster upgrade with podSpec reconciliation disabled", func() {
	var cluster apiv1.Cluster

	BeforeEach(func() {
		cluster = apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test",
				Annotations: map[string]string{},
				Labels:      map[string]string{},
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:13.11",
			},
		}
		configuration.Current = configuration.NewConfiguration()
	})

	It("skips the rollout if the annotation that disables PodSpec reconciliation is set", func(ctx SpecContext) {
		cluster.Annotations[utils.ReconcilePodSpecAnnotationName] = "disabled"

		pod, err := specs.NewInstance(ctx, cluster, 1, true)
		Expect(err).ToNot(HaveOccurred())
		cluster.Spec.SchedulerName = "newScheduler"
		delete(pod.Annotations, utils.PodSpecAnnotationName)

		status := postgres.PostgresqlStatus{
			Pod:            pod,
			PendingRestart: false,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}

		rollout := isInstanceNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeFalse())
		Expect(rollout.canBeInPlace).To(BeFalse())
		Expect(rollout.reason).To(BeEmpty())
	})
})

type fakePluginClientRollout struct {
	pluginClient.Client
	returnedPod   *corev1.Pod
	returnedError error
}

func (f fakePluginClientRollout) LifecycleHook(
	_ context.Context,
	_ plugin.OperationVerb,
	_ k8client.Object,
	_ k8client.Object,
) (k8client.Object, error) {
	return f.returnedPod, f.returnedError
}

var _ = Describe("checkPodSpec with plugins", Ordered, func() {
	var cluster apiv1.Cluster

	BeforeEach(func() {
		cluster = apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:13.11",
			},
		}
		configuration.Current = configuration.NewConfiguration()
	})

	AfterAll(func() {
		configuration.Current = configuration.NewConfiguration()
	})

	It("image change", func() {
		pod, err := specs.NewInstance(context.TODO(), cluster, 1, true)
		Expect(err).ToNot(HaveOccurred())

		podModifiedByPlugins := pod.DeepCopy()

		podModifiedByPlugins.Spec.Containers[0].Image = "postgres:19.0"

		pluginCli := fakePluginClientRollout{
			returnedPod: podModifiedByPlugins,
		}
		ctx := pluginClient.SetPluginClientInContext(context.TODO(), pluginCli)

		rollout, err := checkPodSpecIsOutdated(ctx, pod, &cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.reason).To(Equal(
			"original and target PodSpec differ in containers: container postgres differs in image"))
	})

	It("init-container change", func() {
		pod, err := specs.NewInstance(context.TODO(), cluster, 1, true)
		Expect(err).ToNot(HaveOccurred())

		podModifiedByPlugins := pod.DeepCopy()

		podModifiedByPlugins.Spec.InitContainers = []corev1.Container{
			{
				Name:  "new-init-container",
				Image: "postgres:19.0",
			},
		}

		pluginCli := fakePluginClientRollout{
			returnedPod: podModifiedByPlugins,
		}
		ctx := pluginClient.SetPluginClientInContext(context.TODO(), pluginCli)

		rollout, err := checkPodSpecIsOutdated(ctx, pod, &cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.reason).To(Equal(
			"original and target PodSpec differ in init-containers: container new-init-container has been added"))
	})

	It("environment variable change", func() {
		pod, err := specs.NewInstance(context.TODO(), cluster, 1, true)
		Expect(err).ToNot(HaveOccurred())

		podModifiedByPlugins := pod.DeepCopy()

		podModifiedByPlugins.Spec.Containers[0].Env = append(podModifiedByPlugins.Spec.Containers[0].Env,
			corev1.EnvVar{
				Name:  "NEW_ENV",
				Value: "new_value",
			})

		pluginCli := fakePluginClientRollout{
			returnedPod: podModifiedByPlugins,
		}
		ctx := pluginClient.SetPluginClientInContext(context.TODO(), pluginCli)

		rollout, err := checkPodSpecIsOutdated(ctx, pod, &cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.reason).To(Equal(
			"original and target PodSpec differ in containers: container postgres differs in environment"))
	})
})

var _ = Describe("Supervised primary update strategy and rollout slots", func() {
	const namespace = "supervised-test"

	var (
		reconciler *ClusterReconciler
		rm         *rolloutManager.Manager
		k8sClient  k8client.Client
	)

	BeforeEach(func() {
		scheme := schemeBuilder.BuildWithAllKnownScheme()
		k8sClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&apiv1.Cluster{}).
			Build()

		// Create namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		}
		Expect(k8sClient.Create(context.Background(), ns)).To(Succeed())

		// Rollout manager with a large cluster delay so we can detect slot consumption
		rm = rolloutManager.New(time.Hour, 0)

		reconciler = &ClusterReconciler{
			Client:         k8sClient,
			Scheme:         scheme,
			Recorder:       record.NewFakeRecorder(120),
			rolloutManager: rm,
		}

		configuration.Current = configuration.NewConfiguration()
	})

	// Helper to create a cluster in the fake client with the given primary update strategy
	createCluster := func(strategy apiv1.PrimaryUpdateStrategy) *apiv1.Cluster {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: namespace,
			},
			Spec: apiv1.ClusterSpec{
				Instances:             1,
				ImageName:             "postgres:16.0",
				PrimaryUpdateStrategy: strategy,
				PrimaryUpdateMethod:   apiv1.PrimaryUpdateMethodRestart,
				StorageConfiguration:  apiv1.StorageConfiguration{Size: "1Gi"},
			},
		}
		cluster.SetDefaults()
		// Set status fields after SetDefaults() since it may clear them
		cluster.Status.CurrentPrimary = "test-cluster-1"
		cluster.Status.Image = "postgres:16.1"
		cluster.Status.Instances = 1
		Expect(k8sClient.Create(context.Background(), cluster)).To(Succeed())
		Expect(k8sClient.Status().Update(context.Background(), cluster)).To(Succeed())
		return cluster
	}

	// Helper to build a pod status list where the primary needs rollout (image mismatch)
	buildPodListWithPrimaryNeedingRollout := func(cluster *apiv1.Cluster) *postgres.PostgresqlStatusList {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.Status.CurrentPrimary,
				Namespace: cluster.Namespace,
				Annotations: map[string]string{
					utils.ClusterSerialAnnotationName: "1",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "postgres",
						Image: "postgres:16.0", // Different from cluster.Status.Image (16.1) -> triggers rollout
					},
				},
			},
		}
		return &postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					Pod:            pod,
					IsPodReady:     true,
					ExecutableHash: "test_hash",
				},
			},
		}
	}

	It("supervised cluster does NOT consume a rollout slot", func(ctx SpecContext) {
		cluster := createCluster(apiv1.PrimaryUpdateStrategySupervised)
		podList := buildPodListWithPrimaryNeedingRollout(cluster)

		restarted, err := reconciler.rolloutRequiredInstances(ctx, cluster, podList)
		Expect(err).ToNot(HaveOccurred())
		Expect(restarted).To(BeTrue())

		// Verify the rollout slot was NOT consumed: a second cluster should still be allowed
		secondCluster := k8client.ObjectKey{Namespace: namespace, Name: "other-cluster"}
		result := rm.CoordinateRollout(secondCluster, "other-pod")
		Expect(result.RolloutAllowed).To(BeTrue(),
			"supervised strategy should not consume the rollout slot")
	})

	It("supervised cluster returns true and sets PhaseWaitingForUser", func(ctx SpecContext) {
		cluster := createCluster(apiv1.PrimaryUpdateStrategySupervised)
		podList := buildPodListWithPrimaryNeedingRollout(cluster)

		restarted, err := reconciler.rolloutRequiredInstances(ctx, cluster, podList)
		Expect(err).ToNot(HaveOccurred())
		Expect(restarted).To(BeTrue())

		// Re-fetch the cluster to see the updated status
		var updatedCluster apiv1.Cluster
		Expect(k8sClient.Get(ctx,
			k8client.ObjectKeyFromObject(cluster),
			&updatedCluster)).To(Succeed())
		Expect(updatedCluster.Status.Phase).To(Equal(apiv1.PhaseWaitingForUser))
	})

	It("unsupervised cluster DOES consume a rollout slot", func(ctx SpecContext) {
		cluster := createCluster(apiv1.PrimaryUpdateStrategyUnsupervised)
		podList := buildPodListWithPrimaryNeedingRollout(cluster)

		// Create the pod in the fake client so upgradePod (Delete) can find it
		pod := podList.Items[0].Pod.DeepCopy()
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())

		restarted, err := reconciler.rolloutRequiredInstances(ctx, cluster, podList)
		Expect(err).ToNot(HaveOccurred())
		Expect(restarted).To(BeTrue())

		// Verify the rollout slot WAS consumed: a second cluster should be blocked
		secondCluster := k8client.ObjectKey{Namespace: namespace, Name: "other-cluster"}
		result := rm.CoordinateRollout(secondCluster, "other-pod")
		Expect(result.RolloutAllowed).To(BeFalse(),
			"unsupervised strategy should consume the rollout slot")
	})
})
