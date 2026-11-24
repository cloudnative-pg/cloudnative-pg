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

package pgbouncer

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	config "github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	pgBouncerConfig "github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

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
					EnablePodMonitor: true, //nolint:staticcheck
				},
			},
			Status: apiv1.PoolerStatus{
				Image: config.Current.PgbouncerImageName,
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

		expectedHash, err := computeTemplateHash(
			pooler, config.Current.OperatorImageName, createSecurityContexts(pooler, cluster))
		Expect(err).ShouldNot(HaveOccurred())

		// Check the computed hash
		Expect(deployment.ObjectMeta.Annotations[utils.PoolerSpecHashAnnotationName]).Should(Equal(expectedHash))

		// Check the metadata
		Expect(deployment.ObjectMeta.Name).To(Equal(pooler.Name))
		Expect(deployment.ObjectMeta.Namespace).To(Equal(pooler.Namespace))
		Expect(deployment.Labels).To(BeEquivalentTo(map[string]string{
			utils.ClusterLabelName:                cluster.Name,
			utils.PgbouncerNameLabel:              pooler.Name,
			utils.PodRoleLabelName:                string(utils.PodRolePooler),
			utils.KubernetesAppLabelName:          utils.AppName,
			utils.KubernetesAppInstanceLabelName:  cluster.Name,
			utils.KubernetesAppComponentLabelName: utils.PoolerComponentName,
			utils.KubernetesAppManagedByLabelName: utils.ManagerName,
		}))
		// Check the DeploymentSpec
		Expect(deployment.Spec.Replicas).To(Equal(pooler.Spec.Instances))
		Expect(deployment.Spec.Selector.MatchLabels[utils.PgbouncerNameLabel]).To(Equal(pooler.Name))

		// Check the PodTemplateSpec
		podTemplate := deployment.Spec.Template
		Expect(podTemplate.ObjectMeta.Annotations).To(Equal(pooler.Spec.Template.ObjectMeta.Annotations))
		Expect(podTemplate.Labels).To(BeEquivalentTo(map[string]string{
			utils.ClusterLabelName:                cluster.Name,
			utils.PgbouncerNameLabel:              pooler.Name,
			utils.PodRoleLabelName:                string(utils.PodRolePooler),
			utils.KubernetesAppLabelName:          utils.AppName,
			utils.KubernetesAppInstanceLabelName:  cluster.Name,
			utils.KubernetesAppComponentLabelName: utils.PoolerComponentName,
			utils.KubernetesAppManagedByLabelName: utils.ManagerName,
		}))

		// Check the containers
		Expect(podTemplate.Spec.Containers).ToNot(BeEmpty())
		Expect(podTemplate.Spec.Containers[0].Name).To(Equal("pgbouncer"))
		Expect(podTemplate.Spec.Containers[0].Image).To(Equal(config.Current.PgbouncerImageName))
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

	It("sets the correct service account name when not specified", func() {
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())
		Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(pooler.Name))
	})

	It("sets the custom service account name when specified", func() {
		customPooler := pooler.DeepCopy()
		customPooler.Spec.ServiceAccountName = "custom-service-account"
		deployment, err := Deployment(customPooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())
		Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal("custom-service-account"))
	})

	It("sets the correct readiness probe", func() {
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TimeoutSeconds).To(Equal(int32(5)))
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket.Port).
			To(Equal(intstr.FromInt32(pgBouncerConfig.PgBouncerPort)))
	})

	It("should correctly set pod resources to the bootstrap init container", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Template: &apiv1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
					},
				},
			},
		}

		dep, err := Deployment(pooler, cluster)
		Expect(err).ToNot(HaveOccurred())
		// check that the init container has the correct resources
		Expect(dep.Spec.Template.Spec.InitContainers).To(HaveLen(1))
		initResources := dep.Spec.Template.Spec.InitContainers[0].Resources
		Expect(initResources.Requests).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("100m")))
		Expect(initResources.Requests).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("128Mi")))
		Expect(initResources.Limits).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("200m")))
		Expect(initResources.Limits).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("256Mi")))
	})

	It("retains user-defined bootstrap-controller resources", func() {
		pooler.Spec.Template = &apiv1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{{
					Name: specs.BootstrapControllerContainerName,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2m"),
							corev1.ResourceMemory: resource.MustParse("30Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("100Mi"),
						},
					},
				}},
			},
		}

		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		var found *corev1.Container
		for i := range deployment.Spec.Template.Spec.InitContainers {
			if deployment.Spec.Template.Spec.InitContainers[i].Name == specs.BootstrapControllerContainerName {
				found = &deployment.Spec.Template.Spec.InitContainers[i]
				break
			}
		}
		Expect(found).ToNot(BeNil())
		Expect(found.Resources.Requests.Cpu().String()).To(Equal("2m"))
		Expect(found.Resources.Requests.Memory().String()).To(Equal("30Mi"))
		Expect(found.Resources.Limits.Cpu().String()).To(Equal("1"))
		Expect(found.Resources.Limits.Memory().String()).To(Equal("100Mi"))
	})

	Context("security contexts", func() {
		findBootstrapContainer := func(deployment *appsv1.Deployment) *corev1.Container {
			for i := range deployment.Spec.Template.Spec.InitContainers {
				if deployment.Spec.Template.Spec.InitContainers[i].Name == specs.BootstrapControllerContainerName {
					return &deployment.Spec.Template.Spec.InitContainers[i]
				}
			}
			return nil
		}

		findPgbouncerContainer := func(deployment *appsv1.Deployment) *corev1.Container {
			for i := range deployment.Spec.Template.Spec.Containers {
				if deployment.Spec.Template.Spec.Containers[i].Name == "pgbouncer" {
					return &deployment.Spec.Template.Spec.Containers[i]
				}
			}
			return nil
		}

		It("applies the default security contexts when the template defines none", func() {
			deployment, err := Deployment(pooler, cluster)
			Expect(err).ShouldNot(HaveOccurred())

			podSecurityContext := deployment.Spec.Template.Spec.SecurityContext
			Expect(podSecurityContext).ToNot(BeNil())
			Expect(podSecurityContext.RunAsNonRoot).To(HaveValue(BeTrue()))
			Expect(podSecurityContext.RunAsUser).To(HaveValue(Equal(int64(998))))
			Expect(podSecurityContext.RunAsGroup).To(HaveValue(Equal(int64(996))))
			Expect(podSecurityContext.FSGroup).To(HaveValue(Equal(int64(996))))
			Expect(podSecurityContext.SeccompProfile).To(Equal(cluster.GetSeccompProfile()))
			Expect(podSecurityContext.SupplementalGroups).To(BeEmpty())

			pgbouncer := findPgbouncerContainer(deployment)
			Expect(pgbouncer).ToNot(BeNil())
			Expect(pgbouncer.SecurityContext).To(Equal(specs.GetSecurityContext(cluster)))

			bootstrap := findBootstrapContainer(deployment)
			Expect(bootstrap).ToNot(BeNil())
			Expect(bootstrap.SecurityContext).To(Equal(specs.GetSecurityContext(cluster)))
		})

		It("merges the pod security context from the template with the defaults", func() {
			pooler.Spec.Template = &apiv1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						SupplementalGroups: []int64{1000},
					},
				},
			}

			deployment, err := Deployment(pooler, cluster)
			Expect(err).ShouldNot(HaveOccurred())

			podSecurityContext := deployment.Spec.Template.Spec.SecurityContext
			Expect(podSecurityContext).ToNot(BeNil())
			Expect(podSecurityContext.SupplementalGroups).To(Equal([]int64{1000}))
			Expect(podSecurityContext.RunAsNonRoot).To(HaveValue(BeTrue()))
			Expect(podSecurityContext.RunAsUser).To(HaveValue(Equal(int64(998))))
			Expect(podSecurityContext.RunAsGroup).To(HaveValue(Equal(int64(996))))
			Expect(podSecurityContext.FSGroup).To(HaveValue(Equal(int64(996))))
			Expect(podSecurityContext.SeccompProfile).To(Equal(cluster.GetSeccompProfile()))
		})

		It("gives precedence to the fields defined in the template pod security context", func() {
			localhostProfile := &corev1.SeccompProfile{
				Type:             corev1.SeccompProfileTypeLocalhost,
				LocalhostProfile: ptr.To("profiles/pgbouncer.json"),
			}
			pooler.Spec.Template = &apiv1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser:           ptr.To(int64(1001)),
						FSGroup:             ptr.To(int64(2000)),
						FSGroupChangePolicy: ptr.To(corev1.FSGroupChangeOnRootMismatch),
						SeccompProfile:      localhostProfile,
					},
				},
			}

			deployment, err := Deployment(pooler, cluster)
			Expect(err).ShouldNot(HaveOccurred())

			podSecurityContext := deployment.Spec.Template.Spec.SecurityContext
			Expect(podSecurityContext).ToNot(BeNil())
			Expect(podSecurityContext.RunAsUser).To(HaveValue(Equal(int64(1001))))
			Expect(podSecurityContext.FSGroup).To(HaveValue(Equal(int64(2000))))
			Expect(podSecurityContext.FSGroupChangePolicy).To(HaveValue(Equal(corev1.FSGroupChangeOnRootMismatch)))
			Expect(podSecurityContext.SeccompProfile).To(Equal(localhostProfile))
			Expect(podSecurityContext.RunAsNonRoot).To(HaveValue(BeTrue()))
			Expect(podSecurityContext.RunAsGroup).To(HaveValue(Equal(int64(996))))
		})

		It("does not mutate the pod security context defined in the Pooler template", func() {
			pooler.Spec.Template = &apiv1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						SupplementalGroups: []int64{1000},
					},
				},
			}

			_, err := Deployment(pooler, cluster)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(pooler.Spec.Template.Spec.SecurityContext).To(Equal(&corev1.PodSecurityContext{
				SupplementalGroups: []int64{1000},
			}))
		})

		It("merges the container security contexts from the template with the defaults", func() {
			pooler.Spec.Template = &apiv1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{
						Name: specs.BootstrapControllerContainerName,
						SecurityContext: &corev1.SecurityContext{
							RunAsUser: ptr.To(int64(1001)),
						},
					}},
					Containers: []corev1.Container{{
						Name: "pgbouncer",
						SecurityContext: &corev1.SecurityContext{
							RunAsUser:  ptr.To(int64(1002)),
							RunAsGroup: ptr.To(int64(2002)),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"NET_RAW"},
							},
						},
					}},
				},
			}

			deployment, err := Deployment(pooler, cluster)
			Expect(err).ShouldNot(HaveOccurred())

			defaults := specs.GetSecurityContext(cluster)

			pgbouncer := findPgbouncerContainer(deployment)
			Expect(pgbouncer).ToNot(BeNil())
			Expect(pgbouncer.SecurityContext).ToNot(BeNil())
			Expect(pgbouncer.SecurityContext.RunAsUser).To(HaveValue(Equal(int64(1002))))
			Expect(pgbouncer.SecurityContext.RunAsGroup).To(HaveValue(Equal(int64(2002))))
			Expect(pgbouncer.SecurityContext.Capabilities.Drop).To(Equal([]corev1.Capability{"NET_RAW"}))
			Expect(pgbouncer.SecurityContext.RunAsNonRoot).To(Equal(defaults.RunAsNonRoot))
			Expect(pgbouncer.SecurityContext.Privileged).To(Equal(defaults.Privileged))
			Expect(pgbouncer.SecurityContext.ReadOnlyRootFilesystem).To(Equal(defaults.ReadOnlyRootFilesystem))
			Expect(pgbouncer.SecurityContext.AllowPrivilegeEscalation).To(Equal(defaults.AllowPrivilegeEscalation))
			Expect(pgbouncer.SecurityContext.SeccompProfile).To(Equal(defaults.SeccompProfile))

			bootstrap := findBootstrapContainer(deployment)
			Expect(bootstrap).ToNot(BeNil())
			Expect(bootstrap.SecurityContext).ToNot(BeNil())
			Expect(bootstrap.SecurityContext.RunAsUser).To(HaveValue(Equal(int64(1001))))
			Expect(bootstrap.SecurityContext.Capabilities).To(Equal(defaults.Capabilities))
			Expect(bootstrap.SecurityContext.RunAsNonRoot).To(Equal(defaults.RunAsNonRoot))
			Expect(bootstrap.SecurityContext.Privileged).To(Equal(defaults.Privileged))
			Expect(bootstrap.SecurityContext.ReadOnlyRootFilesystem).To(Equal(defaults.ReadOnlyRootFilesystem))
			Expect(bootstrap.SecurityContext.AllowPrivilegeEscalation).To(Equal(defaults.AllowPrivilegeEscalation))
			Expect(bootstrap.SecurityContext.SeccompProfile).To(Equal(defaults.SeccompProfile))
		})

		It("uses the cluster security context for containers not defining one in the template", func() {
			cluster.Spec.SecurityContext = &corev1.SecurityContext{
				ReadOnlyRootFilesystem: ptr.To(false),
				Capabilities:           &corev1.Capabilities{Drop: []corev1.Capability{"NET_RAW"}},
			}

			deployment, err := Deployment(pooler, cluster)
			Expect(err).ShouldNot(HaveOccurred())

			pgbouncer := findPgbouncerContainer(deployment)
			Expect(pgbouncer).ToNot(BeNil())
			Expect(pgbouncer.SecurityContext.ReadOnlyRootFilesystem).To(HaveValue(BeFalse()))
			Expect(pgbouncer.SecurityContext.Capabilities.Drop).To(Equal([]corev1.Capability{"NET_RAW"}))

			bootstrap := findBootstrapContainer(deployment)
			Expect(bootstrap).ToNot(BeNil())
			Expect(bootstrap.SecurityContext.ReadOnlyRootFilesystem).To(HaveValue(BeFalse()))
			Expect(bootstrap.SecurityContext.Capabilities.Drop).To(Equal([]corev1.Capability{"NET_RAW"}))
		})

		It("removes the PostgreSQL identity from the cluster container security context", func() {
			cluster.Spec.SecurityContext = &corev1.SecurityContext{
				RunAsUser:    ptr.To(int64(26)),
				RunAsGroup:   ptr.To(int64(26)),
				RunAsNonRoot: ptr.To(false),
			}

			deployment, err := Deployment(pooler, cluster)
			Expect(err).ShouldNot(HaveOccurred())

			for _, securityContext := range []*corev1.SecurityContext{
				findPgbouncerContainer(deployment).SecurityContext,
				findBootstrapContainer(deployment).SecurityContext,
			} {
				Expect(securityContext.RunAsUser).To(BeNil())
				Expect(securityContext.RunAsGroup).To(BeNil())
				Expect(securityContext.RunAsNonRoot).To(HaveValue(BeTrue()))
			}
		})

		It("gives precedence to the template over the cluster container security context", func() {
			cluster.Spec.SecurityContext = &corev1.SecurityContext{
				ReadOnlyRootFilesystem: ptr.To(false),
				Capabilities:           &corev1.Capabilities{Drop: []corev1.Capability{"NET_RAW"}},
			}
			pooler.Spec.Template = &apiv1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "pgbouncer",
						SecurityContext: &corev1.SecurityContext{
							ReadOnlyRootFilesystem: ptr.To(true),
							RunAsUser:              ptr.To(int64(1002)),
						},
					}},
				},
			}

			deployment, err := Deployment(pooler, cluster)
			Expect(err).ShouldNot(HaveOccurred())

			pgbouncer := findPgbouncerContainer(deployment)
			Expect(pgbouncer).ToNot(BeNil())
			Expect(pgbouncer.SecurityContext.ReadOnlyRootFilesystem).To(HaveValue(BeTrue()))
			Expect(pgbouncer.SecurityContext.RunAsUser).To(HaveValue(Equal(int64(1002))))
			Expect(pgbouncer.SecurityContext.Capabilities.Drop).To(Equal([]corev1.Capability{"NET_RAW"}))
		})

		It("uses the cluster pod security context with the PgBouncer identity", func() {
			cluster.Spec.PodSecurityContext = &corev1.PodSecurityContext{
				RunAsNonRoot:        ptr.To(false),
				RunAsUser:           ptr.To(int64(26)),
				RunAsGroup:          ptr.To(int64(26)),
				FSGroup:             ptr.To(int64(26)),
				SupplementalGroups:  []int64{2000},
				FSGroupChangePolicy: ptr.To(corev1.FSGroupChangeOnRootMismatch),
				Sysctls:             []corev1.Sysctl{{Name: "net.ipv4.tcp_keepalive_time", Value: "60"}},
			}

			deployment, err := Deployment(pooler, cluster)
			Expect(err).ShouldNot(HaveOccurred())

			podSecurityContext := deployment.Spec.Template.Spec.SecurityContext
			Expect(podSecurityContext).ToNot(BeNil())
			Expect(podSecurityContext.RunAsNonRoot).To(HaveValue(BeTrue()))
			Expect(podSecurityContext.RunAsUser).To(HaveValue(Equal(int64(998))))
			Expect(podSecurityContext.RunAsGroup).To(HaveValue(Equal(int64(996))))
			Expect(podSecurityContext.FSGroup).To(HaveValue(Equal(int64(996))))
			Expect(podSecurityContext.SupplementalGroups).To(Equal([]int64{2000}))
			Expect(podSecurityContext.FSGroupChangePolicy).To(HaveValue(Equal(corev1.FSGroupChangeOnRootMismatch)))
			Expect(podSecurityContext.Sysctls).To(BeNil())
			Expect(podSecurityContext.SeccompProfile).To(Equal(cluster.GetSeccompProfile()))

			// The cluster is not mutated
			Expect(cluster.Spec.PodSecurityContext.RunAsUser).To(HaveValue(Equal(int64(26))))
			Expect(cluster.Spec.PodSecurityContext.Sysctls).To(HaveLen(1))
		})

		It("gives precedence to the template over the cluster pod security context", func() {
			cluster.Spec.PodSecurityContext = &corev1.PodSecurityContext{
				SupplementalGroups: []int64{2000},
			}
			pooler.Spec.Template = &apiv1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser:          ptr.To(int64(1001)),
						SupplementalGroups: []int64{3000},
					},
				},
			}

			deployment, err := Deployment(pooler, cluster)
			Expect(err).ShouldNot(HaveOccurred())

			podSecurityContext := deployment.Spec.Template.Spec.SecurityContext
			Expect(podSecurityContext.RunAsUser).To(HaveValue(Equal(int64(1001))))
			Expect(podSecurityContext.SupplementalGroups).To(Equal([]int64{3000}))
			Expect(podSecurityContext.RunAsGroup).To(HaveValue(Equal(int64(996))))
		})

		It("prefers the seccomp profile of the cluster pod security context", func() {
			clusterProfile := &corev1.SeccompProfile{
				Type:             corev1.SeccompProfileTypeLocalhost,
				LocalhostProfile: ptr.To("profiles/cluster.json"),
			}
			podProfile := &corev1.SeccompProfile{
				Type:             corev1.SeccompProfileTypeLocalhost,
				LocalhostProfile: ptr.To("profiles/pod.json"),
			}
			cluster.Spec.SeccompProfile = clusterProfile

			deployment, err := Deployment(pooler, cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.SecurityContext.SeccompProfile).To(Equal(clusterProfile))

			cluster.Spec.PodSecurityContext = &corev1.PodSecurityContext{SeccompProfile: podProfile}
			deployment, err = Deployment(pooler, cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(deployment.Spec.Template.Spec.SecurityContext.SeccompProfile).To(Equal(podProfile))
		})
	})

	Context("deployment hash", func() {
		getHash := func() string {
			deployment, err := Deployment(pooler, cluster)
			Expect(err).ShouldNot(HaveOccurred())
			return deployment.Annotations[utils.PoolerSpecHashAnnotationName]
		}

		It("is stable when nothing changes", func() {
			Expect(getHash()).To(Equal(getHash()))
		})

		It("does not change when unrelated cluster fields change", func() {
			before := getHash()
			cluster.Spec.Instances = 5
			Expect(getHash()).To(Equal(before))
		})

		It("changes when the cluster pod security context changes", func() {
			before := getHash()
			cluster.Spec.PodSecurityContext = &corev1.PodSecurityContext{SupplementalGroups: []int64{2000}}
			Expect(getHash()).ToNot(Equal(before))
		})

		It("changes when the cluster container security context changes", func() {
			before := getHash()
			cluster.Spec.SecurityContext = &corev1.SecurityContext{ReadOnlyRootFilesystem: ptr.To(false)}
			Expect(getHash()).ToNot(Equal(before))
		})

		It("changes when the cluster seccomp profile changes", func() {
			before := getHash()
			cluster.Spec.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}
			Expect(getHash()).ToNot(Equal(before))
		})
	})
})

var _ = Describe("mergePodSecurityContext", func() {
	var defaultContext *corev1.PodSecurityContext

	BeforeEach(func() {
		defaultContext = &corev1.PodSecurityContext{
			RunAsNonRoot:   ptr.To(true),
			RunAsUser:      ptr.To(int64(998)),
			RunAsGroup:     ptr.To(int64(996)),
			FSGroup:        ptr.To(int64(996)),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		}
	})

	It("returns the defaults when no context is defined", func() {
		Expect(mergePodSecurityContext(nil, defaultContext)).To(Equal(defaultContext))
	})

	It("returns a copy of the defined context when there are no defaults", func() {
		definedContext := &corev1.PodSecurityContext{SupplementalGroups: []int64{1000}}

		merged := mergePodSecurityContext(definedContext, nil)
		Expect(merged).To(Equal(definedContext))
		Expect(merged).ToNot(BeIdenticalTo(definedContext))
	})

	It("fills the unset fields with the defaults", func() {
		definedContext := &corev1.PodSecurityContext{SupplementalGroups: []int64{1000}}

		Expect(mergePodSecurityContext(definedContext, defaultContext)).To(Equal(&corev1.PodSecurityContext{
			RunAsNonRoot:       ptr.To(true),
			RunAsUser:          ptr.To(int64(998)),
			RunAsGroup:         ptr.To(int64(996)),
			FSGroup:            ptr.To(int64(996)),
			SeccompProfile:     &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			SupplementalGroups: []int64{1000},
		}))
	})

	It("gives precedence to the defined fields", func() {
		definedContext := &corev1.PodSecurityContext{
			RunAsNonRoot:       ptr.To(false),
			RunAsUser:          ptr.To(int64(1001)),
			SupplementalGroups: []int64{1000},
		}
		defaultContext.SupplementalGroups = []int64{2000, 3000}

		merged := mergePodSecurityContext(definedContext, defaultContext)
		Expect(merged.RunAsNonRoot).To(HaveValue(BeFalse()))
		Expect(merged.RunAsUser).To(HaveValue(Equal(int64(1001))))
		Expect(merged.SupplementalGroups).To(Equal([]int64{1000}))
		Expect(merged.RunAsGroup).To(HaveValue(Equal(int64(996))))
	})

	It("does not mutate the defined context", func() {
		definedContext := &corev1.PodSecurityContext{SupplementalGroups: []int64{1000}}

		_ = mergePodSecurityContext(definedContext, defaultContext)
		Expect(definedContext).To(Equal(&corev1.PodSecurityContext{SupplementalGroups: []int64{1000}}))
	})
})

var _ = Describe("mergeContainerSecurityContext", func() {
	var defaultContext *corev1.SecurityContext

	BeforeEach(func() {
		defaultContext = &corev1.SecurityContext{
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			Privileged:               ptr.To(false),
			RunAsNonRoot:             ptr.To(true),
			ReadOnlyRootFilesystem:   ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		}
	})

	It("returns the defaults when no context is defined", func() {
		Expect(mergeContainerSecurityContext(nil, defaultContext)).To(Equal(defaultContext))
	})

	It("returns a copy of the defined context when there are no defaults", func() {
		definedContext := &corev1.SecurityContext{RunAsUser: ptr.To(int64(1001))}

		merged := mergeContainerSecurityContext(definedContext, nil)
		Expect(merged).To(Equal(definedContext))
		Expect(merged).ToNot(BeIdenticalTo(definedContext))
	})

	It("fills the unset fields with the defaults", func() {
		definedContext := &corev1.SecurityContext{RunAsUser: ptr.To(int64(1001))}

		Expect(mergeContainerSecurityContext(definedContext, defaultContext)).To(Equal(&corev1.SecurityContext{
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			Privileged:               ptr.To(false),
			RunAsUser:                ptr.To(int64(1001)),
			RunAsNonRoot:             ptr.To(true),
			ReadOnlyRootFilesystem:   ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		}))
	})

	It("gives precedence to the defined fields", func() {
		definedContext := &corev1.SecurityContext{
			ReadOnlyRootFilesystem: ptr.To(false),
			Capabilities:           &corev1.Capabilities{Drop: []corev1.Capability{"NET_RAW"}},
		}

		merged := mergeContainerSecurityContext(definedContext, defaultContext)
		Expect(merged.ReadOnlyRootFilesystem).To(HaveValue(BeFalse()))
		Expect(merged.Capabilities.Drop).To(Equal([]corev1.Capability{"NET_RAW"}))
		Expect(merged.RunAsNonRoot).To(HaveValue(BeTrue()))
	})

	It("does not mutate the defined context", func() {
		definedContext := &corev1.SecurityContext{RunAsUser: ptr.To(int64(1001))}

		_ = mergeContainerSecurityContext(definedContext, defaultContext)
		Expect(definedContext).To(Equal(&corev1.SecurityContext{RunAsUser: ptr.To(int64(1001))}))
	})
})
