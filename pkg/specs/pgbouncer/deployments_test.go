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

package pgbouncer

import (
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	pgBouncerConfig "github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/hash"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Deployment", func() {
	var (
		pooler  *apiv1.Pooler
		cluster *apiv1.Cluster
	)

	BeforeEach(func() {
		pooler = &apiv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pooler",
				Namespace: "test-namespace",
			},
			Spec: apiv1.PoolerSpec{
				Cluster:   apiv1.LocalObjectReference{Name: "test-cluster"},
				Type:      apiv1.PoolerTypeRW,
				Instances: ptr.To(int32(1)),
				Template:  &apiv1.PodTemplateSpec{},
				PgBouncer: &apiv1.PgBouncerSpec{
					PoolMode:  apiv1.PgBouncerPoolModeSession,
					AuthQuery: "test",
				},
				DeploymentStrategy: &appsv1.DeploymentStrategy{
					Type: appsv1.RollingUpdateDeploymentStrategyType,
				},
				Monitoring: &apiv1.PoolerMonitoringConfiguration{
					EnablePodMonitor: true,
				},
			},
		}

		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-namespace",
			},
		}
	})

	It("creates a Deployment correctly", func() {
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())

		expectedHash, err := hash.ComputeVersionedHash(pooler.Spec, 3)
		Expect(err).ShouldNot(HaveOccurred())

		// Check the computed hash
		Expect(deployment.ObjectMeta.Annotations[utils.PoolerSpecHashAnnotationName]).Should(Equal(expectedHash))

		// Check the metadata
		Expect(deployment.ObjectMeta.Name).To(Equal(pooler.Name))
		Expect(deployment.ObjectMeta.Namespace).To(Equal(pooler.Namespace))
		Expect(deployment.Labels[utils.ClusterLabelName]).To(Equal(cluster.Name))
		Expect(deployment.Labels[utils.PgbouncerNameLabel]).To(Equal(pooler.Name))
		Expect(deployment.Labels[utils.PodRoleLabelName]).To(BeEquivalentTo(utils.PodRolePooler))

		// Check the DeploymentSpec
		Expect(deployment.Spec.Replicas).To(Equal(pooler.Spec.Instances))
		Expect(deployment.Spec.Selector.MatchLabels[utils.PgbouncerNameLabel]).To(Equal(pooler.Name))

		// Check the PodTemplateSpec
		podTemplate := deployment.Spec.Template
		Expect(podTemplate.ObjectMeta.Annotations).To(Equal(pooler.Spec.Template.ObjectMeta.Annotations))
		Expect(podTemplate.ObjectMeta.Labels[utils.PgbouncerNameLabel]).To(Equal(pooler.Name))
		Expect(podTemplate.ObjectMeta.Labels[utils.PodRoleLabelName]).To(BeEquivalentTo(utils.PodRolePooler))

		// Check the containers
		Expect(podTemplate.Spec.Containers).ToNot(BeEmpty())
		Expect(podTemplate.Spec.Containers[0].Name).To(Equal("pgbouncer"))
		Expect(podTemplate.Spec.Containers[0].Image).To(Equal(DefaultPgbouncerImage))
	})

	It("sets the correct number of replicas", func() {
		pooler.Spec.Instances = ptr.To(int32(3))
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())
		Expect(deployment.Spec.Replicas).To(Equal(pooler.Spec.Instances))
	})

	It("sets the correct deployment strategy", func() {
		pooler.Spec.DeploymentStrategy = &appsv1.DeploymentStrategy{
			Type: appsv1.RecreateDeploymentStrategyType,
		}
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())
		Expect(deployment.Spec.Strategy.Type).To(Equal(appsv1.RecreateDeploymentStrategyType))
	})

	It("creates correct volume mounts", func() {
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts).ToNot(BeEmpty())
		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name).To(Equal("scratch-data"))
		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath).To(Equal(postgres.ScratchDataDirectory))
	})

	It("creates correct init containers", func() {
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())

		Expect(deployment.Spec.Template.Spec.InitContainers).ToNot(BeEmpty())
		Expect(deployment.Spec.Template.Spec.InitContainers[0].Name).To(Equal(specs.BootstrapControllerContainerName))
	})

	It("sets the correct service account name", func() {
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())
		Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(pooler.Name))
	})

	It("sets the correct readiness probe", func() {
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TimeoutSeconds).To(Equal(int32(5)))
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket.Port).
			To(Equal(intstr.FromInt32(pgBouncerConfig.PgBouncerPort)))
	})
})
