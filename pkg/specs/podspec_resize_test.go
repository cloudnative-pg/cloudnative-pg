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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func makeResources(cpuRequest, memoryRequest, cpuLimit, memoryLimit string) corev1.ResourceRequirements {
	requirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}
	if cpuRequest != "" {
		requirements.Requests[corev1.ResourceCPU] = resource.MustParse(cpuRequest)
	}
	if memoryRequest != "" {
		requirements.Requests[corev1.ResourceMemory] = resource.MustParse(memoryRequest)
	}
	if cpuLimit != "" {
		requirements.Limits[corev1.ResourceCPU] = resource.MustParse(cpuLimit)
	}
	if memoryLimit != "" {
		requirements.Limits[corev1.ResourceMemory] = resource.MustParse(memoryLimit)
	}
	return requirements
}

var _ = Describe("ComparePodSpecsIgnoringContainerResources", func() {
	basePodSpec := func() corev1.PodSpec {
		return corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name:      "init",
					Image:     "init:1",
					Resources: makeResources("100m", "100Mi", "100m", "100Mi"),
				},
			},
			Containers: []corev1.Container{
				{
					Name:      PostgresContainerName,
					Image:     "postgres:17.0",
					Resources: makeResources("500m", "1Gi", "500m", "1Gi"),
				},
			},
		}
	}

	It("matches specs that differ only in container resources", func() {
		current := basePodSpec()
		target := basePodSpec()
		target.Containers[0].Resources = makeResources("1", "2Gi", "1", "2Gi")
		target.InitContainers[0].Resources = makeResources("200m", "100Mi", "200m", "100Mi")

		match, _ := ComparePodSpecsIgnoringContainerResources(current, target)
		Expect(match).To(BeTrue())
	})

	It("detects any other difference", func() {
		current := basePodSpec()
		target := basePodSpec()
		target.Containers[0].Image = "postgres:17.1"
		target.Containers[0].Resources = makeResources("1", "2Gi", "1", "2Gi")

		match, diff := ComparePodSpecsIgnoringContainerResources(current, target)
		Expect(match).To(BeFalse())
		Expect(diff).To(ContainSubstring("image"))
	})

	It("does not modify the compared specs", func() {
		current := basePodSpec()
		target := basePodSpec()

		match, _ := ComparePodSpecsIgnoringContainerResources(current, target)
		Expect(match).To(BeTrue())
		Expect(current.Containers[0].Resources.Requests).ToNot(BeEmpty())
		Expect(target.Containers[0].Resources.Requests).ToNot(BeEmpty())
	})
})

var _ = Describe("GetContainerResourceDrifts", func() {
	It("returns nothing when the resources are semantically equal", func() {
		current := corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "a", Resources: makeResources("1", "1Gi", "1", "1Gi")},
			},
		}
		target := corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "a", Resources: makeResources("1000m", "1024Mi", "1000m", "1024Mi")},
			},
		}

		Expect(GetContainerResourceDrifts(&current, &target)).To(BeEmpty())
	})

	It("treats nil and empty resource lists as equal", func() {
		current := corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "a"},
			},
		}
		target := corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "a", Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{},
					Limits:   corev1.ResourceList{},
				}},
			},
		}

		Expect(GetContainerResourceDrifts(&current, &target)).To(BeEmpty())
	})

	It("reports drifted containers and init containers", func() {
		current := corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "sidecar", Resources: makeResources("100m", "100Mi", "100m", "100Mi")},
			},
			Containers: []corev1.Container{
				{Name: "a", Resources: makeResources("500m", "1Gi", "500m", "1Gi")},
				{Name: "b", Resources: makeResources("500m", "1Gi", "500m", "1Gi")},
			},
		}
		target := corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "sidecar", Resources: makeResources("200m", "100Mi", "200m", "100Mi")},
			},
			Containers: []corev1.Container{
				{Name: "a", Resources: makeResources("1", "1Gi", "1", "1Gi")},
				{Name: "b", Resources: makeResources("500m", "1Gi", "500m", "1Gi")},
			},
		}

		drifts := GetContainerResourceDrifts(&current, &target)
		Expect(drifts).To(HaveLen(2))
		names := make(map[string]bool)
		for _, drift := range drifts {
			names[drift.Name] = drift.InitContainer
		}
		Expect(names).To(HaveKeyWithValue("a", false))
		Expect(names).To(HaveKeyWithValue("sidecar", true))
	})

	It("ignores containers present on one side only", func() {
		current := corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "a", Resources: makeResources("500m", "1Gi", "500m", "1Gi")},
			},
		}
		target := corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "a", Resources: makeResources("500m", "1Gi", "500m", "1Gi")},
				{Name: "extra", Resources: makeResources("1", "1Gi", "1", "1Gi")},
			},
		}

		Expect(GetContainerResourceDrifts(&current, &target)).To(BeEmpty())
	})
})

var _ = Describe("CanResizeInPlace", func() {
	always := corev1.ContainerRestartPolicyAlways

	makeSpecs := func(currentResources, targetResources corev1.ResourceRequirements) (corev1.PodSpec, corev1.PodSpec) {
		current := corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: PostgresContainerName, Resources: currentResources},
			},
		}
		target := corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: PostgresContainerName, Resources: targetResources},
			},
		}
		return current, target
	}

	It("accepts an empty drift", func() {
		current, target := makeSpecs(
			makeResources("500m", "1Gi", "500m", "1Gi"),
			makeResources("500m", "1Gi", "500m", "1Gi"))
		ok, _ := CanResizeInPlace(&current, &current, &target)
		Expect(ok).To(BeTrue())
	})

	It("accepts cpu changes in both directions", func() {
		current, target := makeSpecs(
			makeResources("500m", "1Gi", "1", "1Gi"),
			makeResources("250m", "1Gi", "2", "1Gi"))
		ok, _ := CanResizeInPlace(&current, &current, &target)
		Expect(ok).To(BeTrue())
	})

	It("accepts memory increases", func() {
		current, target := makeSpecs(
			makeResources("500m", "1Gi", "500m", "1Gi"),
			makeResources("500m", "2Gi", "500m", "2Gi"))
		ok, _ := CanResizeInPlace(&current, &current, &target)
		Expect(ok).To(BeTrue())
	})

	It("refuses memory limit decreases", func() {
		current, target := makeSpecs(
			makeResources("500m", "1Gi", "500m", "2Gi"),
			makeResources("500m", "1Gi", "500m", "1Gi"))
		ok, reason := CanResizeInPlace(&current, &current, &target)
		Expect(ok).To(BeFalse())
		Expect(reason).To(ContainSubstring("memory limit"))
	})

	It("accepts memory request decreases", func() {
		current, target := makeSpecs(
			makeResources("500m", "2Gi", "500m", "2Gi"),
			makeResources("500m", "1Gi", "500m", "2Gi"))
		ok, _ := CanResizeInPlace(&current, &current, &target)
		Expect(ok).To(BeTrue())
	})

	It("refuses added resource entries", func() {
		current := corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: PostgresContainerName,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("500m"),
						},
					},
				},
			},
		}
		target := corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: PostgresContainerName,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			},
		}
		ok, _ := CanResizeInPlace(&current, &current, &target)
		Expect(ok).To(BeFalse())
	})

	It("refuses removed resource entries", func() {
		current, target := makeSpecs(
			makeResources("500m", "1Gi", "500m", "1Gi"),
			corev1.ResourceRequirements{})
		ok, _ := CanResizeInPlace(&current, &current, &target)
		Expect(ok).To(BeFalse())
	})

	It("refuses changes to resources other than cpu and memory", func() {
		currentResources := makeResources("500m", "1Gi", "500m", "1Gi")
		currentResources.Limits["hugepages-2Mi"] = resource.MustParse("256Mi")
		targetResources := makeResources("500m", "1Gi", "500m", "1Gi")
		targetResources.Limits["hugepages-2Mi"] = resource.MustParse("512Mi")

		current, target := makeSpecs(currentResources, targetResources)
		ok, reason := CanResizeInPlace(&current, &current, &target)
		Expect(ok).To(BeFalse())
		Expect(reason).To(ContainSubstring("hugepages-2Mi"))
	})

	It("ignores run-once init container changes, deferring them to the next recreation", func() {
		current := corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "init", Resources: makeResources("100m", "", "", "")},
			},
		}
		target := corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "init", Resources: makeResources("200m", "", "", "")},
			},
		}

		Expect(GetResizableContainerResourceDrifts(&current, &target)).To(BeEmpty())
		// the drift is still visible to the full detection
		Expect(GetContainerResourceDrifts(&current, &target)).To(HaveLen(1))

		ok, _ := CanResizeInPlace(&current, &current, &target)
		Expect(ok).To(BeTrue())
	})

	It("accepts native sidecar changes", func() {
		current := corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "sidecar", RestartPolicy: &always, Resources: makeResources("100m", "", "", "")},
			},
		}
		target := corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "sidecar", RestartPolicy: &always, Resources: makeResources("200m", "", "", "")},
			},
		}
		ok, _ := CanResizeInPlace(&current, &current, &target)
		Expect(ok).To(BeTrue())
	})

	It("evaluates every drifted container", func() {
		current := corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "a", Resources: makeResources("500m", "1Gi", "500m", "1Gi")},
				{Name: "b", Resources: makeResources("500m", "1Gi", "500m", "2Gi")},
			},
		}
		target := corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "a", Resources: makeResources("1", "1Gi", "1", "1Gi")},
				{Name: "b", Resources: makeResources("500m", "1Gi", "500m", "1Gi")},
			},
		}
		ok, _ := CanResizeInPlace(&current, &current, &target)
		Expect(ok).To(BeFalse())
	})

	It("accepts dropping a request equal to its limit", func() {
		stored, target := makeSpecs(
			makeResources("1", "1Gi", "1", "1Gi"),
			makeResources("", "", "1", "1Gi"))
		ok, _ := CanResizeInPlace(&stored, &stored, &target)
		Expect(ok).To(BeTrue())
	})

	It("refuses an entry removed from the stored spec even if admission defaulted it", func() {
		stored, target := makeSpecs(
			makeResources("500m", "1Gi", "1", "1Gi"),
			makeResources("500m", "1Gi", "", "1Gi"))
		// a LimitRange default brings back the cpu limit in the live pod
		live := *stored.DeepCopy()
		ok, reason := CanResizeInPlace(&live, &stored, &target)
		Expect(ok).To(BeFalse())
		Expect(reason).To(ContainSubstring("removed"))
	})

	It("refuses a memory limit decrease against a live spec resized by someone else", func() {
		stored, target := makeSpecs(
			makeResources("500m", "1Gi", "500m", "2Gi"),
			makeResources("500m", "1Gi", "500m", "3Gi"))
		live, _ := makeSpecs(makeResources("500m", "1Gi", "500m", "4Gi"), corev1.ResourceRequirements{})
		ok, reason := CanResizeInPlace(&live, &stored, &target)
		Expect(ok).To(BeFalse())
		Expect(reason).To(ContainSubstring("memory limit"))
	})
})

var _ = Describe("GetLiveContainerResourceDrifts", func() {
	makeSpec := func(resources corev1.ResourceRequirements) corev1.PodSpec {
		return corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: PostgresContainerName, Resources: resources},
			},
		}
	}

	It("ignores the requests the API server defaults from the limits", func() {
		live := makeSpec(makeResources("1", "1Gi", "1", "1Gi"))
		target := makeSpec(makeResources("", "", "1", "1Gi"))
		Expect(GetLiveContainerResourceDrifts(&live, &target)).To(BeEmpty())
	})

	It("ignores the entries added at admission time", func() {
		// a LimitRange defaulted the resources of a container declaring none
		live := makeSpec(makeResources("100m", "256Mi", "500m", "512Mi"))
		target := makeSpec(corev1.ResourceRequirements{})
		Expect(GetLiveContainerResourceDrifts(&live, &target)).To(BeEmpty())
	})

	It("overlays the target over the live resources", func() {
		live := makeSpec(makeResources("1", "256Mi", "1", "512Mi"))
		target := makeSpec(corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
		})

		drifts := GetLiveContainerResourceDrifts(&live, &target)
		Expect(drifts).To(HaveLen(1))
		Expect(drifts[0].Target.Requests.Cpu().String()).To(Equal("2"))
		Expect(drifts[0].Target.Limits.Cpu().String()).To(Equal("2"))
		Expect(drifts[0].Target.Requests.Memory().String()).To(Equal("256Mi"))
		Expect(drifts[0].Target.Limits.Memory().String()).To(Equal("512Mi"))
		// the live spec is untouched
		Expect(live.Containers[0].Resources.Limits.Cpu().String()).To(Equal("1"))
	})
})
