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

package specs

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
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
)

var _ = Describe("The PostgreSQL security context", func() {
	securityContext := CreatePostgresSecurityContext(26, 26)

	It("allows the container to create its own PGDATA", func() {
		Expect(securityContext.RunAsUser).To(Equal(securityContext.FSGroup))
	})
})

var _ = Describe("Create affinity section", func() {
	clusterName := "cluster-test"

	It("enable preferred pod affinity everything default", func() {
		config := v1.AffinityConfiguration{
			PodAntiAffinityType: "preferred",
		}
		affinity := CreateAffinitySection(clusterName, config)
		Expect(affinity).NotTo(BeNil())
		Expect(affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
	})

	It("can not set pod affinity if pod anti-affinity is disabled", func() {
		config := v1.AffinityConfiguration{
			EnablePodAntiAffinity: pointerToBool(false),
		}
		affinity := CreateAffinitySection(clusterName, config)
		Expect(affinity).To(BeNil())
	})

	It("can set pod anti affinity with 'preferred' pod anti-affinity type", func() {
		config := v1.AffinityConfiguration{
			EnablePodAntiAffinity: pointerToBool(true),
			PodAntiAffinityType:   "preferred",
		}
		affinity := CreateAffinitySection(clusterName, config)
		Expect(affinity.PodAntiAffinity).NotTo(BeNil())
		Expect(affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
	})

	It("can set pod anti-affinity with 'required' pod anti-affinity type", func() {
		config := v1.AffinityConfiguration{
			EnablePodAntiAffinity: pointerToBool(true),
			PodAntiAffinityType:   "required",
		}
		affinity := CreateAffinitySection(clusterName, config)
		Expect(affinity.PodAntiAffinity).NotTo(BeNil())
		Expect(affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
		Expect(affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).To(BeNil())
	})
	It("does not set pod anti-affinity if provided an invalid type", func() {
		config := v1.AffinityConfiguration{
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
				config := v1.AffinityConfiguration{
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
				config := v1.AffinityConfiguration{
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
				config := v1.AffinityConfiguration{
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
				config := v1.AffinityConfiguration{
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
				config := v1.AffinityConfiguration{
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
				config := v1.AffinityConfiguration{
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
				config := v1.AffinityConfiguration{
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
})
