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

package specs

import (
	"encoding/json"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func pointerToBool(b bool) *bool {
	return &b
}

var (
	testAffinityTerm = corev1.PodAffinityTerm{
		LabelSelector: &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "test",
					Operator: metav1.LabelSelectorOpExists,
				},
			},
		},
	}
	testWeightedAffinityTerm = corev1.WeightedPodAffinityTerm{
		Weight:          100,
		PodAffinityTerm: testAffinityTerm,
	}
	testNodeSelectorTerm = corev1.NodeSelectorTerm{
		MatchExpressions: []corev1.NodeSelectorRequirement{
			{
				Key:      "test",
				Operator: corev1.NodeSelectorOpExists,
			},
		},
	}
)

var _ = Describe("The PostgreSQL security context with", func() {
	It("default RuntimeDefault profile", func() {
		cluster := apiv1.Cluster{}
		securityContext := CreatePodSecurityContext(cluster.GetSeccompProfile(), 26, 26)

		Expect(securityContext.SeccompProfile).ToNot(BeNil())
		Expect(securityContext.SeccompProfile.Type).To(BeEquivalentTo(corev1.SeccompProfileTypeRuntimeDefault))
	})

	It("defined SeccompProfile profile", func() {
		profilePath := "/path/to/profile"
		localhostProfile := &corev1.SeccompProfile{
			Type:             corev1.SeccompProfileTypeLocalhost,
			LocalhostProfile: &profilePath,
		}
		cluster := apiv1.Cluster{Spec: apiv1.ClusterSpec{SeccompProfile: localhostProfile}}
		securityContext := CreatePodSecurityContext(cluster.GetSeccompProfile(), 26, 26)

		Expect(securityContext.SeccompProfile).ToNot(BeNil())
		Expect(securityContext.SeccompProfile).To(BeEquivalentTo(localhostProfile))
		Expect(securityContext.SeccompProfile.LocalhostProfile).To(BeEquivalentTo(&profilePath))
	})
})

var _ = Describe("Create affinity section", func() {
	clusterName := "cluster-test"

	It("enable preferred pod affinity everything default", func() {
		config := apiv1.AffinityConfiguration{
			PodAntiAffinityType: "preferred",
		}
		affinity := CreateAffinitySection(clusterName, config)
		Expect(affinity).NotTo(BeNil())
		Expect(affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
	})

	It("can not set pod affinity if pod anti-affinity is disabled", func() {
		config := apiv1.AffinityConfiguration{
			EnablePodAntiAffinity: pointerToBool(false),
		}
		affinity := CreateAffinitySection(clusterName, config)
		Expect(affinity).To(BeNil())
	})

	It("can set pod anti affinity with 'preferred' pod anti-affinity type", func() {
		config := apiv1.AffinityConfiguration{
			EnablePodAntiAffinity: pointerToBool(true),
			PodAntiAffinityType:   "preferred",
		}
		affinity := CreateAffinitySection(clusterName, config)
		Expect(affinity.PodAntiAffinity).NotTo(BeNil())
		Expect(affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
	})

	It("can set pod anti-affinity with 'required' pod anti-affinity type", func() {
		config := apiv1.AffinityConfiguration{
			EnablePodAntiAffinity: pointerToBool(true),
			PodAntiAffinityType:   "required",
		}
		affinity := CreateAffinitySection(clusterName, config)
		Expect(affinity.PodAntiAffinity).NotTo(BeNil())
		Expect(affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
		Expect(affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).To(BeNil())
	})
	It("does not set pod anti-affinity if provided an invalid type", func() {
		config := apiv1.AffinityConfiguration{
			EnablePodAntiAffinity: pointerToBool(true),
			PodAntiAffinityType:   "not-a-type",
		}
		affinity := CreateAffinitySection(clusterName, config)
		Expect(affinity).To(BeNil())
		config.EnablePodAntiAffinity = pointerToBool(false)
		affinity = CreateAffinitySection(clusterName, config)
		Expect(affinity).To(BeNil())
	})

	When("given additional affinity terms", func() {
		When("generated pod anti-affinity is enabled", func() {
			It("sets both pod affinity and anti-affinity correctly if passed and set to required", func() {
				config := apiv1.AffinityConfiguration{
					EnablePodAntiAffinity: pointerToBool(true),
					PodAntiAffinityType:   "required",
					AdditionalPodAffinity: &corev1.PodAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{testWeightedAffinityTerm},
						RequiredDuringSchedulingIgnoredDuringExecution:  []corev1.PodAffinityTerm{testAffinityTerm},
					},
					AdditionalPodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{testWeightedAffinityTerm},
						RequiredDuringSchedulingIgnoredDuringExecution:  []corev1.PodAffinityTerm{testAffinityTerm},
					},
				}
				affinity := CreateAffinitySection(clusterName, config)
				Expect(affinity).NotTo(BeNil())
				Expect(affinity.PodAffinity).NotTo(BeNil())
				Expect(affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution).
					To(BeEquivalentTo([]corev1.PodAffinityTerm{testAffinityTerm}))
				Expect(affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution).
					To(BeEquivalentTo([]corev1.WeightedPodAffinityTerm{testWeightedAffinityTerm}))
				Expect(affinity.PodAntiAffinity).NotTo(BeNil())
				Expect(affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution).
					To(ContainElement(testAffinityTerm))
				Expect(affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).
					To(BeEquivalentTo([]corev1.WeightedPodAffinityTerm{testWeightedAffinityTerm}))
			})
			It("sets pod both affinity and anti-affinity correctly if passed and set to preferred", func() {
				config := apiv1.AffinityConfiguration{
					EnablePodAntiAffinity: pointerToBool(true),
					PodAntiAffinityType:   "preferred",
					AdditionalPodAffinity: &corev1.PodAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{testWeightedAffinityTerm},
						RequiredDuringSchedulingIgnoredDuringExecution:  []corev1.PodAffinityTerm{testAffinityTerm},
					},
					AdditionalPodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{testWeightedAffinityTerm},
						RequiredDuringSchedulingIgnoredDuringExecution:  []corev1.PodAffinityTerm{testAffinityTerm},
					},
				}
				affinity := CreateAffinitySection(clusterName, config)
				Expect(affinity).NotTo(BeNil())
				Expect(affinity.PodAffinity).NotTo(BeNil())
				Expect(affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution).
					To(BeEquivalentTo([]corev1.PodAffinityTerm{testAffinityTerm}))
				Expect(affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution).
					To(BeEquivalentTo([]corev1.WeightedPodAffinityTerm{testWeightedAffinityTerm}))
				Expect(affinity.PodAntiAffinity).NotTo(BeNil())
				Expect(affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution).
					To(BeEquivalentTo([]corev1.PodAffinityTerm{testAffinityTerm}))
				Expect(affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).
					To(ContainElement(testWeightedAffinityTerm))
			})
		})
		When("generated pod anti-affinity is disabled", func() {
			It("sets pod required anti-affinity correctly if passed", func() {
				config := apiv1.AffinityConfiguration{
					EnablePodAntiAffinity: pointerToBool(false),
					AdditionalPodAntiAffinity: &corev1.PodAntiAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{testAffinityTerm},
					},
				}
				affinity := CreateAffinitySection(clusterName, config)
				Expect(affinity).NotTo(BeNil())
				Expect(affinity.PodAntiAffinity).NotTo(BeNil())
				Expect(affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution).
					To(BeEquivalentTo([]corev1.PodAffinityTerm{testAffinityTerm}))
				Expect(affinity.PodAffinity).To(BeNil())
			})
			It("sets pod preferred anti-affinity correctly if passed", func() {
				config := apiv1.AffinityConfiguration{
					EnablePodAntiAffinity: pointerToBool(false),
					AdditionalPodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{testWeightedAffinityTerm},
					},
				}
				affinity := CreateAffinitySection(clusterName, config)
				Expect(affinity).NotTo(BeNil())
				Expect(affinity.PodAntiAffinity).NotTo(BeNil())
				Expect(affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).
					To(BeEquivalentTo([]corev1.WeightedPodAffinityTerm{testWeightedAffinityTerm}))
				Expect(affinity.PodAffinity).To(BeNil())
			})
			It("sets pod preferred affinity correctly if passed", func() {
				config := apiv1.AffinityConfiguration{
					EnablePodAntiAffinity: pointerToBool(false),
					AdditionalPodAffinity: &corev1.PodAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{testWeightedAffinityTerm},
					},
				}
				affinity := CreateAffinitySection(clusterName, config)
				Expect(affinity).NotTo(BeNil())
				Expect(affinity.PodAffinity).NotTo(BeNil())
				Expect(affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution).
					To(BeEquivalentTo([]corev1.WeightedPodAffinityTerm{testWeightedAffinityTerm}))
				Expect(affinity.PodAntiAffinity).To(BeNil())
			})
			It("sets pod required affinity correctly if passed", func() {
				config := apiv1.AffinityConfiguration{
					EnablePodAntiAffinity: pointerToBool(false),
					AdditionalPodAffinity: &corev1.PodAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{testAffinityTerm},
					},
				}
				affinity := CreateAffinitySection(clusterName, config)
				Expect(affinity).NotTo(BeNil())
				Expect(affinity.PodAffinity).NotTo(BeNil())
				Expect(affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution).
					To(BeEquivalentTo([]corev1.PodAffinityTerm{testAffinityTerm}))
				Expect(affinity.PodAntiAffinity).To(BeNil())
			})
			It("sets pod both affinity and anti-affinity correctly if passed", func() {
				config := apiv1.AffinityConfiguration{
					EnablePodAntiAffinity: pointerToBool(false),
					AdditionalPodAffinity: &corev1.PodAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{testWeightedAffinityTerm},
						RequiredDuringSchedulingIgnoredDuringExecution:  []corev1.PodAffinityTerm{testAffinityTerm},
					},
					AdditionalPodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{testWeightedAffinityTerm},
						RequiredDuringSchedulingIgnoredDuringExecution:  []corev1.PodAffinityTerm{testAffinityTerm},
					},
				}
				affinity := CreateAffinitySection(clusterName, config)
				Expect(affinity).NotTo(BeNil())
				Expect(affinity.PodAffinity).NotTo(BeNil())
				Expect(affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution).
					To(BeEquivalentTo([]corev1.PodAffinityTerm{testAffinityTerm}))
				Expect(affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution).
					To(BeEquivalentTo([]corev1.WeightedPodAffinityTerm{testWeightedAffinityTerm}))
				Expect(affinity.PodAntiAffinity).NotTo(BeNil())
				Expect(affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution).
					To(BeEquivalentTo([]corev1.PodAffinityTerm{testAffinityTerm}))
				Expect(affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).
					To(BeEquivalentTo([]corev1.WeightedPodAffinityTerm{testWeightedAffinityTerm}))
			})
		})
	})

	When("given node affinity config", func() {
		It("sets node affinity", func() {
			config := apiv1.AffinityConfiguration{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{testNodeSelectorTerm},
					},
				},
			}
			affinity := CreateAffinitySection(clusterName, config)
			Expect(affinity).NotTo(BeNil())
			Expect(affinity.NodeAffinity).NotTo(BeNil())
			Expect(affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
			Expect(affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms).
				To(BeEquivalentTo([]corev1.NodeSelectorTerm{testNodeSelectorTerm}))
		})
	})
})

var _ = Describe("EnvConfig", func() {
	Context("IsEnvEqual function", func() {
		It("returns true if the Env are equal", func() {
			cluster := apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test-ns",
				},
				Spec: apiv1.ClusterSpec{
					Env: []corev1.EnvVar{
						{
							Name:  "TEST_ENV",
							Value: "EXPECTED",
						},
					},
				},
			}
			envConfig := CreatePodEnvConfig(cluster, "test-1")

			container := corev1.Container{
				Env: []corev1.EnvVar{
					{
						Name:  "PGDATA",
						Value: PgDataPath,
					},
					{
						Name:  "POD_NAME",
						Value: "test-1",
					},
					{
						Name:  "NAMESPACE",
						Value: cluster.Namespace,
					},
					{
						Name:  "CLUSTER_NAME",
						Value: cluster.Name,
					},
					{
						Name:  "PSQL_HISTORY",
						Value: postgres.TemporaryDirectory + "/.psql_history",
					},
					{
						Name:  "PGPORT",
						Value: strconv.Itoa(postgres.ServerPort),
					},
					{
						Name:  "PGHOST",
						Value: postgres.SocketDirectory,
					},
					{
						Name:  "TMPDIR",
						Value: postgres.TemporaryDirectory,
					},
					{
						Name:  "TEST_ENV",
						Value: "EXPECTED",
					},
				},
			}

			Expect(envConfig.IsEnvEqual(container)).To(BeTrue())
		})

		It("returns false if the Env are different", func() {
			cluster := apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test-ns",
				},
			}
			envConfig := CreatePodEnvConfig(cluster, "test-1")

			container := corev1.Container{
				Env: []corev1.EnvVar{
					{
						Name:  "PGDATA",
						Value: PgDataPath,
					},
					{
						Name:  "POD_NAME",
						Value: "test-1",
					},
					{
						Name:  "NAMESPACE",
						Value: cluster.Namespace,
					},
					{
						Name:  "CLUSTER_NAME",
						Value: cluster.Name,
					},
					{
						Name:  "PGPORT",
						Value: strconv.Itoa(postgres.ServerPort),
					},
					{
						Name:  "PGHOST",
						Value: postgres.SocketDirectory,
					},
					{
						Name:  "TMPDIR",
						Value: postgres.TemporaryDirectory,
					},
					{
						Name:  "TEST_ENV",
						Value: "UNEXPECTED",
					},
				},
			}

			Expect(envConfig.IsEnvEqual(container)).To(BeFalse())
		})

		It("returns true if the EnvFrom are equal", func() {
			cluster := apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test-ns",
				},
				Spec: apiv1.ClusterSpec{
					EnvFrom: []corev1.EnvFromSource{
						{
							ConfigMapRef: &corev1.ConfigMapEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "sourceConfigMap",
								},
							},
						},
					},
				},
			}
			envConfig := CreatePodEnvConfig(cluster, "test-1")

			container := corev1.Container{
				Env: envConfig.EnvVars,
				EnvFrom: []corev1.EnvFromSource{
					{
						ConfigMapRef: &corev1.ConfigMapEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "sourceConfigMap",
							},
						},
					},
				},
			}

			Expect(envConfig.IsEnvEqual(container)).To(BeTrue())
		})

		It("returns false if the EnvFrom are different", func() {
			cluster := apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test-ns",
				},
				Spec: apiv1.ClusterSpec{
					EnvFrom: []corev1.EnvFromSource{
						{
							SecretRef: &corev1.SecretEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "sourceConfigMap",
								},
							},
						},
					},
				},
			}
			envConfig := CreatePodEnvConfig(cluster, "test-1")

			container := corev1.Container{
				Env: envConfig.EnvVars,
				EnvFrom: []corev1.EnvFromSource{
					{
						ConfigMapRef: &corev1.ConfigMapEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "sourceConfigMap",
							},
						},
					},
				},
			}

			Expect(envConfig.IsEnvEqual(container)).To(BeFalse())
		})
	})
})

var _ = Describe("PodSpec drift detection", func() {
	It("ignores order of volumes", func() {
		podSpec1 := corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "pgdata",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "test-1",
							ReadOnly:  false,
						},
					},
				},
				{
					Name: "scratch-data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "test-1",
							ReadOnly:  false,
						},
					},
				},
			},
		}
		reorderedPodSpec1 := corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "scratch-data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "test-1",
							ReadOnly:  false,
						},
					},
				},
				{
					Name: "pgdata",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "test-1",
							ReadOnly:  false,
						},
					},
				},
			},
		}

		specsMatch, diff := ComparePodSpecs(podSpec1, reorderedPodSpec1)
		Expect(diff).To(BeEmpty())
		Expect(specsMatch).To(BeTrue())
	})

	It("detects drift in content of the same element", func() {
		podSpec1 := corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "pgdata",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "test-1",
							ReadOnly:  false,
						},
					},
				},
			},
		}
		reorderedPodSpec1 := corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "pgdata",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "test-foo",
							ReadOnly:  false,
						},
					},
				},
			},
		}

		specsMatch, diff := ComparePodSpecs(podSpec1, reorderedPodSpec1)
		Expect(diff).To(Equal("volumes: element pgdata has differing value"))
		Expect(specsMatch).To(BeFalse())
	})

	It("detects drift on missing volumes", func() {
		podSpec1 := corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "scratch-data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "test-1",
							ReadOnly:  false,
						},
					},
				},
			},
		}
		podSpec2 := corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "scratch-data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "test-1",
							ReadOnly:  false,
						},
					},
				},
				{
					Name: "pgdata",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "test-1",
							ReadOnly:  false,
						},
					},
				},
			},
		}

		specsMatch, diff := ComparePodSpecs(podSpec1, podSpec2)
		Expect(diff).To(Equal("volumes: element pgdata has been added"))
		Expect(specsMatch).To(BeFalse())
	})

	It("ignores order of volume mounts in postgres container", func() {
		podSpec1 := corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "postgres",
					Image: "postgres:13.11",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:             "pgdata",
							ReadOnly:         false,
							MountPath:        "/var/lib/postgresql/data",
							SubPath:          "",
							MountPropagation: nil,
							SubPathExpr:      "",
						},
						{
							Name:             "tbs-bar",
							ReadOnly:         false,
							MountPath:        "/var/lib/postgresql/tablespaces/bar",
							SubPath:          "",
							MountPropagation: nil,
							SubPathExpr:      "",
						},
					},
				},
			},
		}
		reorderedPodSpec1 := corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "postgres",
					Image: "postgres:13.11",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:             "tbs-bar",
							ReadOnly:         false,
							MountPath:        "/var/lib/postgresql/tablespaces/bar",
							SubPath:          "",
							MountPropagation: nil,
							SubPathExpr:      "",
						},
						{
							Name:             "pgdata",
							ReadOnly:         false,
							MountPath:        "/var/lib/postgresql/data",
							SubPath:          "",
							MountPropagation: nil,
							SubPathExpr:      "",
						},
					},
				},
			},
		}

		specsMatch, diff := ComparePodSpecs(podSpec1, reorderedPodSpec1)
		Expect(diff).To(BeEmpty())
		Expect(specsMatch).To(BeTrue())
	})

	It("detects missing container", func() {
		podSpec1 := corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:         "postgres",
					Image:        "postgres:13.11",
					VolumeMounts: []corev1.VolumeMount{},
				},
				{
					Name:         "foo",
					Image:        "foobar",
					VolumeMounts: []corev1.VolumeMount{},
				},
			},
		}
		podSpec2 := corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:         "postgres",
					Image:        "postgres:13.11",
					VolumeMounts: []corev1.VolumeMount{},
				},
			},
		}

		specsMatch, diff := ComparePodSpecs(podSpec1, podSpec2)
		Expect(diff).To(ContainSubstring("containers: container foo has been removed"))
		Expect(specsMatch).To(BeFalse())
	})

	It("detects difference in generic field", func() {
		podSpec1 := corev1.PodSpec{
			ServiceAccountName: "foo",
			Containers:         []corev1.Container{},
		}
		podSpec2 := corev1.PodSpec{
			ServiceAccountName: "bar",
			Containers:         []corev1.Container{},
		}

		specsMatch, diff := ComparePodSpecs(podSpec1, podSpec2)
		Expect(diff).To(ContainSubstring("service-account-name"))
		Expect(specsMatch).To(BeFalse())
	})

	It("detects missing volume mounts in postgres container", func() {
		podSpec1 := corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "postgres",
					Image: "postgres:13.11",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:             "pgdata",
							ReadOnly:         false,
							MountPath:        "/var/lib/postgresql/data",
							SubPath:          "",
							MountPropagation: nil,
							SubPathExpr:      "",
						},
						{
							Name:             "tbs-bar",
							ReadOnly:         false,
							MountPath:        "/var/lib/postgresql/tablespaces/bar",
							SubPath:          "",
							MountPropagation: nil,
							SubPathExpr:      "",
						},
					},
				},
			},
		}
		podSpec2 := corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "postgres",
					Image: "postgres:13.11",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:             "tbs-bar",
							ReadOnly:         false,
							MountPath:        "/var/lib/postgresql/tablespaces/bar",
							SubPath:          "",
							MountPropagation: nil,
							SubPathExpr:      "",
						},
					},
				},
			},
		}

		specsMatch, diff := ComparePodSpecs(podSpec1, podSpec2)
		Expect(diff).To(ContainSubstring("containers:"))
		Expect(diff).To(ContainSubstring("container postgres differs in volume-mounts:"))
		Expect(diff).To(ContainSubstring("element pgdata has been removed"))
		Expect(specsMatch).To(BeFalse())
	})

	It("detects image mismatch on the postgres container", func() {
		podSpec1 := corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "postgres",
					Image: "postgres:13.11",
				},
			},
		}
		podSpec2 := corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "postgres",
					Image: "postgres:13.13",
				},
			},
		}

		specsMatch, diff := ComparePodSpecs(podSpec1, podSpec2)
		Expect(diff).To(ContainSubstring(
			"containers: container postgres differs in image"))
		Expect(specsMatch).To(BeFalse())
	})

	It("detects resource mismatch on the postgres container", func() {
		podSpec1 := corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "postgres",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{"storage": resource.MustParse("1Gi")},
						Limits: corev1.ResourceList{
							"cpu":    resource.MustParse("2"),
							"memory": resource.MustParse("1Gi"),
						},
					},
				},
			},
		}
		podSpec2 := corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "postgres",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{"storage": resource.MustParse("1Gi")},
						Limits: corev1.ResourceList{
							"cpu":    resource.MustParse("2"),
							"memory": resource.MustParse("3Gi"),
						},
					},
				},
			},
		}

		specsMatch, diff := ComparePodSpecs(podSpec1, podSpec2)
		Expect(diff).To(ContainSubstring(
			"containers: container postgres differs in resources"))
		Expect(specsMatch).To(BeFalse())
	})

	It("detects if resource quantities for containers are equivalent", func() {
		podSpec1 := `{
			"containers": [
				{
					"name": "postgres",
					"resources": {
						"limits": {
							"cpu": "1000m",
							"memory": "3Gi"
						},
						"requests": {
							"cpu": "850m",
							"memory": "3072Mi"
						}
					}
				}
			]
		}`
		var storedPodSpec1, podSpec2 corev1.PodSpec
		err := json.Unmarshal([]byte(podSpec1), &storedPodSpec1)
		Expect(err).NotTo(HaveOccurred())
		Expect(storedPodSpec1.Containers).To(HaveLen(1))

		podSpec2 = corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "postgres",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"cpu":    resource.MustParse("1"),
							"memory": resource.MustParse("3Gi"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("850m"),
							"memory": resource.MustParse("3Gi"),
						},
					},
				},
			},
		}

		// NOTE: the object representations of the specs are different, even
		// though they represent equivalent quantities
		// i.e. reflect.DeepEqual(podSpec2, storedPodSpec1) is likely false
		// Let's make sure the comparison function can recognize equivalent quantities
		specsMatch, diff := ComparePodSpecs(storedPodSpec1, podSpec2)
		Expect(diff).To(Equal(""))
		Expect(specsMatch).To(BeTrue())
	})

	It("detects if resource quantities for containers are equivalent if one is nil and one is empty", func() {
		// empty map
		podSpec1 := `{
			"containers": [
				{
					"name": "postgres",
					"resources": {
						"limits": {},
						"requests": {}
					}
				}
			]
		}`
		var storedPodSpec1, podSpec2 corev1.PodSpec
		err := json.Unmarshal([]byte(podSpec1), &storedPodSpec1)
		Expect(err).NotTo(HaveOccurred())
		Expect(storedPodSpec1.Containers).To(HaveLen(1))

		podSpec2 = corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "postgres",
					Resources: corev1.ResourceRequirements{
						Limits:   nil,
						Requests: nil,
					},
				},
			},
		}

		// NOTE: the object representations of the specs are different, even
		// though they represent equivalent quantities
		// i.e. reflect.DeepEqual(podSpec2, storedPodSpec1) is likely false
		// Let's make sure the comparison function can recognize equivalent quantities
		specsMatch, diff := ComparePodSpecs(storedPodSpec1, podSpec2)
		Expect(diff).To(Equal(""))
		Expect(specsMatch).To(BeTrue())
	})
})

var _ = Describe("Compute startup probe failure threshold", func() {
	It("should take the minimum value 1", func() {
		Expect(getFailureThreshold(5, StartupProbePeriod)).To(BeNumerically("==", 1))
		Expect(getFailureThreshold(5, LivenessProbePeriod)).To(BeNumerically("==", 1))
	})

	It("should take the value from 'startDelay / periodSeconds'", func() {
		Expect(getFailureThreshold(109, StartupProbePeriod)).To(BeNumerically("==", 11))
		Expect(getFailureThreshold(31, LivenessProbePeriod)).To(BeNumerically("==", 4))
	})
})

var _ = Describe("NewInstance", func() {
	It("applies JSON patch from annotation", func(ctx SpecContext) {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Annotations: map[string]string{
					utils.PodPatchAnnotationName: `[{"op": "replace", "path": "/spec/containers/0/image", "value": "new-image:latest"}]`, // nolint: lll
				},
			},
			Status: apiv1.ClusterStatus{
				Image: "test",
			},
		}

		pod, err := NewInstance(ctx, cluster, 1, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod).NotTo(BeNil())
		Expect(pod.Spec.Containers[0].Image).To(Equal("new-image:latest"))
	})

	It("returns error if JSON patch is invalid", func(ctx SpecContext) {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Annotations: map[string]string{
					utils.PodPatchAnnotationName: `invalid-json-patch`,
				},
			},
		}

		_, err := NewInstance(ctx, cluster, 1, true)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("while decoding JSON patch from annotation"))
	})
})
