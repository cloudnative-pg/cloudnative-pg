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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pod upgrade", Ordered, func() {
	cluster := apiv1.Cluster{
		Spec: apiv1.ClusterSpec{
			ImageName: "postgres:13.11",
		},
	}

	It("will not require a restart for just created Pods", func(ctx SpecContext) {
		pod := specs.PodWithExistingStorage(cluster, 1)

		status := postgres.PostgresqlStatus{
			Pod:            pod,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}

		rollout := isPodNeedingRollout(ctx, status, &cluster)
		Expect(rollout.reason).To(BeEmpty())
		Expect(rollout.required).To(BeFalse())
	})

	It("requires rollout when running a different image name", func(ctx SpecContext) {
		pod := specs.PodWithExistingStorage(cluster, 1)
		pod.Spec.Containers[0].Image = "postgres:13.10"
		status := postgres.PostgresqlStatus{
			Pod:            pod,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}
		rollout := isPodNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.reason).To(BeEquivalentTo("the instance is using an old image: postgres:13.10 -> postgres:13.11"))
	})

	It("does not ask for rollout when update is to a different major release", func(ctx SpecContext) {
		pod := specs.PodWithExistingStorage(cluster, 1)
		pod.Spec.Containers[0].Image = "postgres:12.15"
		status := postgres.PostgresqlStatus{
			Pod:            pod,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}
		rollout := isPodNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeFalse())
		Expect(rollout.reason).To(BeEmpty())
	})

	It("requires rollout when a restart annotation has been added to the cluster", func(ctx SpecContext) {
		pod := specs.PodWithExistingStorage(cluster, 1)
		clusterRestart := cluster
		clusterRestart.Annotations = make(map[string]string)
		clusterRestart.Annotations[specs.ClusterRestartAnnotationName] = "now"

		status := postgres.PostgresqlStatus{
			Pod:            pod,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}

		rollout := isPodNeedingRollout(ctx, status, &clusterRestart)
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.reason).To(Equal("cluster has been explicitly restarted via annotation"))
		Expect(rollout.canBeInPlace).To(BeTrue())

		rollout = isPodNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeFalse())
		Expect(rollout.reason).To(BeEmpty())
	})

	It("requires rollout when PostgreSQL needs to be restarted", func(ctx SpecContext) {
		pod := specs.PodWithExistingStorage(cluster, 1)

		status := postgres.PostgresqlStatus{
			Pod:            pod,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}

		rollout := isPodNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeFalse())
		Expect(rollout.reason).To(BeEmpty())

		status = postgres.PostgresqlStatus{
			Pod:            pod,
			IsPodReady:     true,
			PendingRestart: true,
			ExecutableHash: "test_hash",
		}
		rollout = isPodNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.reason).To(Equal("configuration needs a restart to apply some configuration changes"))
	})

	It("requires pod rollout if executable does not have a hash", func(ctx SpecContext) {
		pod := specs.PodWithExistingStorage(cluster, 1)
		status := postgres.PostgresqlStatus{
			Pod:            pod,
			PendingRestart: false,
			IsPodReady:     true,
		}
		rollout := isPodNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.reason).To(Equal("missing executable hash"))
		Expect(rollout.canBeInPlace).To(BeFalse())
	})

	It("checks when a rollout is needed for any reason", func(ctx SpecContext) {
		pod := specs.PodWithExistingStorage(cluster, 1)
		status := postgres.PostgresqlStatus{
			Pod:            pod,
			PendingRestart: true,
		}
		rollout := isPodNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeFalse())
		Expect(rollout.canBeInPlace).To(BeFalse())
		Expect(rollout.reason).To(BeEmpty())

		status = postgres.PostgresqlStatus{
			Pod:            pod,
			PendingRestart: true,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}
		rollout = isPodNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.reason).To(BeEquivalentTo("configuration needs a restart to apply some configuration changes"))
		Expect(rollout.canBeInPlace).To(BeTrue())
	})

	It("should trigger a rollout when the scheduler changes", func(ctx SpecContext) {
		pod := specs.PodWithExistingStorage(cluster, 1)
		cluster.Spec.SchedulerName = "newScheduler"

		status := postgres.PostgresqlStatus{
			Pod:            pod,
			PendingRestart: false,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}

		rollout := isPodNeedingRollout(ctx, status, &cluster)
		Expect(rollout.required).To(BeTrue())
		Expect(rollout.reason).To(ContainSubstring("scheduler name changed"))
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
			pod := specs.PodWithExistingStorage(clusterWithResources, 1)
			clusterWithResources.Spec.Resources.Limits["cpu"] = resource.MustParse("3") // was "2"

			status := postgres.PostgresqlStatus{
				Pod:            pod,
				PendingRestart: false,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}

			rollout := isPodNeedingRollout(ctx, status, &clusterWithResources)
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.reason).To(ContainSubstring("the instance resources don't match"))
		})
		It("should trigger a rollout when the cluster has Resources deleted from spec", func(ctx SpecContext) {
			pod := specs.PodWithExistingStorage(clusterWithResources, 1)
			clusterWithResources.Spec.Resources = corev1.ResourceRequirements{}

			status := postgres.PostgresqlStatus{
				Pod:            pod,
				PendingRestart: false,
				IsPodReady:     true,
				ExecutableHash: "test_hash",
			}

			rollout := isPodNeedingRollout(ctx, status, &clusterWithResources)
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.reason).To(ContainSubstring("the instance resources don't match"))
		})
	})

	When("there's a custom environment variable set", func() {
		It("detects when a new custom environment variable is set", func(ctx SpecContext) {
			pod := specs.PodWithExistingStorage(cluster, 1)

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

			rollout := isPodNeedingRollout(ctx, status, cluster)
			Expect(rollout.required).To(BeTrue())
			Expect(rollout.reason).To(Equal("environment variable configuration hash changed"))
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
		pod = specs.PodWithExistingStorage(*cluster, 1)
	})

	It("should not require rollout when cluster and pod have the same TopologySpreadConstraints", func(ctx SpecContext) {
		status := postgres.PostgresqlStatus{
			Pod:            pod,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}
		rollout := isPodNeedingRollout(ctx, status, cluster)
		Expect(rollout.reason).To(BeEmpty())
		Expect(rollout.required).To(BeFalse())
	})

	It("should require rollout when the cluster and pod do not have "+
		"the same TopologySpreadConstraints", func(ctx SpecContext) {
		pod2 := pod.DeepCopy()
		pod2.Spec.TopologySpreadConstraints[0].MaxSkew = 2
		status := postgres.PostgresqlStatus{
			Pod:            pod2,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}
		rollout := isPodNeedingRollout(ctx, status, cluster)
		Expect(rollout.reason).To(ContainSubstring("does not have up-to-date TopologySpreadConstraints"))
		Expect(rollout.required).To(BeTrue())
	})

	It("should require rollout when the LabelSelector maps are different", func(ctx SpecContext) {
		pod2 := pod.DeepCopy()
		pod2.Spec.TopologySpreadConstraints[0].LabelSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{"app": "different-app"},
		}

		status := postgres.PostgresqlStatus{
			Pod:            pod2,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}
		rollout := isPodNeedingRollout(ctx, status, cluster)
		Expect(rollout.reason).To(ContainSubstring("does not have up-to-date TopologySpreadConstraints"))
		Expect(rollout.required).To(BeTrue())
	})

	It("should require rollout when TopologySpreadConstraints is nil in one of the objects", func(ctx SpecContext) {
		pod2 := pod.DeepCopy()
		pod2.Spec.TopologySpreadConstraints = nil

		status := postgres.PostgresqlStatus{
			Pod:            pod2,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}
		rollout := isPodNeedingRollout(ctx, status, cluster)
		Expect(rollout.reason).To(ContainSubstring("does not have up-to-date TopologySpreadConstraints"))
		Expect(rollout.required).To(BeTrue())
	})

	It("should not require rollout if pod and spec both lack TopologySpreadConstraints", func(ctx SpecContext) {
		cluster.Spec.TopologySpreadConstraints = nil
		pod2 := pod.DeepCopy()
		pod2.Spec.TopologySpreadConstraints = nil

		status := postgres.PostgresqlStatus{
			Pod:            pod2,
			IsPodReady:     true,
			ExecutableHash: "test_hash",
		}
		rollout := isPodNeedingRollout(ctx, status, cluster)
		Expect(rollout.reason).To(BeEmpty())
		Expect(rollout.required).To(BeFalse())
	})
})
