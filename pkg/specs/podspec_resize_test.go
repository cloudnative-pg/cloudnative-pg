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
	"k8s.io/apimachinery/pkg/api/equality"
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

var _ = Describe("ResizeInPlace", func() {
	always := corev1.ContainerRestartPolicyAlways

	makeSpec := func(resources corev1.ResourceRequirements) *corev1.PodSpec {
		return &corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: PostgresContainerName, Resources: resources},
			},
		}
	}

	// admit returns the pod spec as the API server stores it when the pod
	// is created, with the missing requests defaulted to the limits
	admit := func(spec *corev1.PodSpec) *corev1.PodSpec {
		live := spec.DeepCopy()
		for _, containers := range [][]corev1.Container{live.Containers, live.InitContainers} {
			for i := range containers {
				for name, limit := range containers[i].Resources.Limits {
					if _, found := containers[i].Resources.Requests[name]; !found {
						if containers[i].Resources.Requests == nil {
							containers[i].Resources.Requests = corev1.ResourceList{}
						}
						containers[i].Resources.Requests[name] = limit
					}
				}
			}
		}
		return live
	}

	expectResized := func(stored, target *corev1.PodSpec, expectedLive corev1.ResourceRequirements) {
		GinkgoHelper()
		live := admit(stored)
		Expect(ResizeInPlace(stored, live, target)).To(BeTrue())
		Expect(equality.Semantic.DeepEqual(stored, target)).To(BeTrue(), "the stored spec must match the target")
		Expect(equality.Semantic.DeepEqual(live.Containers[0].Resources, expectedLive)).To(BeTrue(),
			"unexpected live resources: %v", live.Containers[0].Resources)
	}

	DescribeTable("resizes the changes that a running pod supports",
		func(current, target, expectedLive corev1.ResourceRequirements) {
			expectResized(makeSpec(current), makeSpec(target), expectedLive)
		},
		Entry("cpu changes in both directions",
			makeResources("500m", "1Gi", "1", "1Gi"),
			makeResources("250m", "1Gi", "2", "1Gi"),
			makeResources("250m", "1Gi", "2", "1Gi")),
		Entry("memory increases",
			makeResources("500m", "1Gi", "500m", "1Gi"),
			makeResources("500m", "2Gi", "500m", "2Gi"),
			makeResources("500m", "2Gi", "500m", "2Gi")),
		Entry("memory request decreases",
			makeResources("500m", "2Gi", "500m", "2Gi"),
			makeResources("500m", "1Gi", "500m", "2Gi"),
			makeResources("500m", "1Gi", "500m", "2Gi")),
		Entry("limits whose requests are defaulted by the API server",
			makeResources("", "", "1", "1Gi"),
			makeResources("", "", "2", "1Gi"),
			makeResources("2", "1Gi", "2", "1Gi")),
		Entry("a request equal to its limit being dropped, with nothing to resize",
			makeResources("1", "1Gi", "1", "1Gi"),
			makeResources("", "", "1", "1Gi"),
			makeResources("1", "1Gi", "1", "1Gi")),
	)

	DescribeTable("refuses the changes that require recreating the pod",
		func(current, target corev1.ResourceRequirements) {
			stored := makeSpec(current)
			Expect(ResizeInPlace(stored, admit(stored), makeSpec(target))).To(BeFalse())
		},
		Entry("a memory limit decrease",
			makeResources("500m", "1Gi", "500m", "2Gi"),
			makeResources("500m", "1Gi", "500m", "1Gi")),
		Entry("an added request",
			makeResources("500m", "", "", ""),
			makeResources("500m", "1Gi", "", "")),
		Entry("an added limit",
			makeResources("500m", "1Gi", "", ""),
			makeResources("500m", "1Gi", "1", "")),
		Entry("a removed limit",
			makeResources("500m", "1Gi", "1", "1Gi"),
			makeResources("500m", "1Gi", "", "1Gi")),
		Entry("a request replaced by another one",
			makeResources("500m", "", "", ""),
			makeResources("", "1Gi", "", "")),
		Entry("all the resources being removed",
			makeResources("500m", "1Gi", "500m", "1Gi"),
			corev1.ResourceRequirements{}),
	)

	It("refuses changes to resources other than cpu and memory", func() {
		current := makeResources("500m", "1Gi", "500m", "1Gi")
		current.Limits["hugepages-2Mi"] = resource.MustParse("256Mi")
		target := makeResources("500m", "1Gi", "500m", "1Gi")
		target.Limits["hugepages-2Mi"] = resource.MustParse("512Mi")

		stored := makeSpec(current)
		Expect(ResizeInPlace(stored, admit(stored), makeSpec(target))).To(BeFalse())
	})

	It("resizes cpu and memory next to other unchanged resources", func() {
		current := makeResources("500m", "1Gi", "500m", "1Gi")
		current.Limits["hugepages-2Mi"] = resource.MustParse("256Mi")
		target := makeResources("1", "1Gi", "1", "1Gi")
		target.Limits["hugepages-2Mi"] = resource.MustParse("256Mi")

		expectedLive := *target.DeepCopy()
		expectedLive.Requests["hugepages-2Mi"] = resource.MustParse("256Mi")
		expectResized(makeSpec(current), makeSpec(target), expectedLive)
	})

	It("keeps the entries the live pod got at admission time", func() {
		stored := makeSpec(corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
		})
		target := makeSpec(corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
		})
		// a LimitRange defaulted the memory the cluster does not declare
		live := makeSpec(makeResources("1", "256Mi", "1", "512Mi"))

		Expect(ResizeInPlace(stored, live, target)).To(BeTrue())
		Expect(equality.Semantic.DeepEqual(live.Containers[0].Resources, makeResources("2", "256Mi", "2", "512Mi"))).
			To(BeTrue(), "unexpected live resources: %v", live.Containers[0].Resources)
	})

	It("leaves alone the containers whose resources did not change", func() {
		stored := &corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: PostgresContainerName, Resources: makeResources("500m", "1Gi", "500m", "1Gi")},
				{Name: "sidecar", Resources: makeResources("100m", "100Mi", "100m", "100Mi")},
			},
		}
		target := stored.DeepCopy()
		target.Containers[0].Resources = makeResources("1", "1Gi", "1", "1Gi")
		// another actor resized the sidecar, and injected a container
		live := stored.DeepCopy()
		live.Containers[1].Resources = makeResources("300m", "100Mi", "300m", "100Mi")
		live.Containers = append(live.Containers, corev1.Container{Name: "injected"})

		Expect(ResizeInPlace(stored, live, target)).To(BeTrue())
		Expect(equality.Semantic.DeepEqual(live.Containers[0].Resources, target.Containers[0].Resources)).To(BeTrue())
		Expect(equality.Semantic.DeepEqual(
			live.Containers[1].Resources, makeResources("300m", "100Mi", "300m", "100Mi"))).To(BeTrue())
		Expect(live.Containers[2].Resources).To(Equal(corev1.ResourceRequirements{}))
	})

	It("refuses the whole change when a single container cannot be resized", func() {
		stored := &corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "a", Resources: makeResources("500m", "1Gi", "500m", "1Gi")},
				{Name: "b", Resources: makeResources("500m", "1Gi", "500m", "2Gi")},
			},
		}
		target := stored.DeepCopy()
		target.Containers[0].Resources = makeResources("1", "1Gi", "1", "1Gi")
		target.Containers[1].Resources = makeResources("500m", "1Gi", "500m", "1Gi")

		Expect(ResizeInPlace(stored, admit(stored), target)).To(BeFalse())
	})

	It("resizes the native sidecars", func() {
		stored := &corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "sidecar", RestartPolicy: &always, Resources: makeResources("100m", "", "", "")},
			},
		}
		target := stored.DeepCopy()
		target.InitContainers[0].Resources = makeResources("200m", "", "", "")
		live := admit(stored)

		Expect(ResizeInPlace(stored, live, target)).To(BeTrue())
		Expect(live.InitContainers[0].Resources.Requests.Cpu().String()).To(Equal("200m"))
		Expect(equality.Semantic.DeepEqual(stored, target)).To(BeTrue())
	})

	It("records the resources of the run-once init containers without resizing them", func() {
		stored := &corev1.PodSpec{
			InitContainers: []corev1.Container{
				// not even a memory limit decrease matters for a container
				// that has already terminated
				{Name: "init", Resources: makeResources("100m", "1Gi", "100m", "2Gi")},
			},
		}
		target := stored.DeepCopy()
		target.InitContainers[0].Resources = makeResources("200m", "1Gi", "200m", "1Gi")
		live := admit(stored)

		Expect(ResizeInPlace(stored, live, target)).To(BeTrue())
		Expect(live.InitContainers[0].Resources.Requests.Cpu().String()).To(Equal("100m"))
		Expect(equality.Semantic.DeepEqual(stored, target)).To(BeTrue())
	})

	It("does not record a change other than requests and limits", func() {
		stored := makeSpec(makeResources("500m", "1Gi", "500m", "1Gi"))
		target := makeSpec(makeResources("1", "1Gi", "1", "1Gi"))
		target.Containers[0].Image = "postgres:18"
		target.Containers[0].Resources.Claims = []corev1.ResourceClaim{{Name: "gpu"}}

		Expect(ResizeInPlace(stored, admit(stored), target)).To(BeTrue())
		Expect(stored.Containers[0].Image).To(BeEmpty())
		Expect(stored.Containers[0].Resources.Claims).To(BeEmpty())
		match, _ := ComparePodSpecs(*stored, *target)
		Expect(match).To(BeFalse())
	})

	It("ignores the containers present on one side only", func() {
		stored := makeSpec(makeResources("500m", "1Gi", "500m", "1Gi"))
		target := stored.DeepCopy()
		target.Containers[0].Name = "renamed"
		target.Containers[0].Resources = makeResources("1", "1Gi", "1", "1Gi")
		live := admit(stored)

		Expect(ResizeInPlace(stored, live, target)).To(BeTrue())
		Expect(equality.Semantic.DeepEqual(live, admit(makeSpec(makeResources("500m", "1Gi", "500m", "1Gi"))))).
			To(BeTrue())
	})
})
