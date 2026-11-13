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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

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

var _ = Describe("ResizePod", func() {
	always := corev1.ContainerRestartPolicyAlways

	makeSpec := func(resources corev1.ResourceRequirements) *corev1.PodSpec {
		return &corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: PostgresContainerName, Image: "postgres:17.0", Resources: resources},
			},
		}
	}
	// resize calls ResizePod, returning the resized pod spec and the one
	// recorded in the pod spec annotation
	resize := func(live, stored, target *corev1.PodSpec) (resized, recorded *corev1.PodSpec, reason string) {
		pod, reason := ResizePod(
			&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}, Spec: *live},
			stored, target)
		if pod == nil {
			return nil, nil, reason
		}
		recorded = &corev1.PodSpec{}
		Expect(json.Unmarshal([]byte(pod.Annotations[utils.PodSpecAnnotationName]), recorded)).To(Succeed())
		return &pod.Spec, recorded, reason
	}
	limitsOnly := func(cpu, memory string) corev1.ResourceRequirements {
		resources := makeResources("", "", cpu, memory)
		resources.Requests = nil
		return resources
	}

	DescribeTable("resizes in place",
		func(live, stored, target, expected corev1.ResourceRequirements) {
			liveSpec, storedSpec := makeSpec(live), makeSpec(stored)
			resized, recorded, reason := resize(liveSpec, storedSpec, makeSpec(target))
			Expect(reason).To(BeEmpty())
			Expect(resized.Containers[0].Resources).To(BeComparableTo(expected))
			Expect(recorded.Containers[0].Resources).To(BeComparableTo(target))
			// the given specs are untouched
			Expect(liveSpec.Containers[0].Resources).To(BeComparableTo(live))
			Expect(storedSpec.Containers[0].Resources).To(BeComparableTo(stored))
		},
		Entry("a memory request decrease",
			makeResources("500m", "1500Mi", "500m", "2Gi"),
			makeResources("500m", "1500Mi", "500m", "2Gi"),
			makeResources("500m", "1Gi", "500m", "2Gi"),
			makeResources("500m", "1Gi", "500m", "2Gi")),
		Entry("a removed request that defaults to its limit",
			makeResources("500m", "1Gi", "1", "2Gi"),
			makeResources("500m", "1Gi", "1", "2Gi"),
			makeResources("", "1Gi", "1", "2Gi"),
			makeResources("1", "1Gi", "1", "2Gi")),
		Entry("keeping the entries added at admission time",
			makeResources("100m", "1Gi", "500m", "1Gi"),
			limitsOnly("", "1Gi"),
			limitsOnly("", "2Gi"),
			makeResources("100m", "2Gi", "500m", "2Gi")),
		Entry("cpu only, keeping the memory limit another actor raised",
			makeResources("500m", "1Gi", "1", "4Gi"),
			makeResources("500m", "1Gi", "1", "2Gi"),
			makeResources("500m", "1Gi", "2", "2Gi"),
			makeResources("500m", "1Gi", "2", "4Gi")),
		Entry("memory only, keeping the cpu another actor set",
			makeResources("700m", "1Gi", "3", "2Gi"),
			makeResources("500m", "1Gi", "1", "2Gi"),
			makeResources("500m", "1Gi", "1", "3Gi"),
			makeResources("700m", "1Gi", "3", "3Gi")),
	)

	DescribeTable("needs a new pod",
		func(live, stored, target corev1.ResourceRequirements, expectedReason string) {
			resized, _, reason := resize(makeSpec(live), makeSpec(stored), makeSpec(target))
			Expect(resized).To(BeNil())
			Expect(reason).To(ContainSubstring(expectedReason))
		},
		Entry("memory limit below the one someone else set on the live pod",
			makeResources("500m", "1Gi", "500m", "4Gi"),
			makeResources("500m", "1Gi", "500m", "2Gi"),
			makeResources("500m", "1Gi", "500m", "3Gi"),
			"memory limit"),
		Entry("entry replaced by another",
			makeResources("500m", "", "", ""),
			makeResources("500m", "", "", ""),
			makeResources("", "1Gi", "", ""),
			"only the cpu and memory values"),
		Entry("removed limit",
			makeResources("250m", "1Gi", "500m", "2Gi"),
			makeResources("250m", "1Gi", "500m", "2Gi"),
			makeResources("250m", "1Gi", "", "2Gi"),
			"only the cpu and memory values"),
		Entry("resource other than cpu and memory",
			corev1.ResourceRequirements{Limits: corev1.ResourceList{"hugepages-2Mi": resource.MustParse("256Mi")}},
			corev1.ResourceRequirements{Limits: corev1.ResourceList{"hugepages-2Mi": resource.MustParse("256Mi")}},
			corev1.ResourceRequirements{Limits: corev1.ResourceList{"hugepages-2Mi": resource.MustParse("512Mi")}},
			"only the cpu and memory values"),
		Entry("request lowered below its limit, leaving the Guaranteed QoS class",
			makeResources("1", "1Gi", "1", "1Gi"),
			makeResources("1", "1Gi", "1", "1Gi"),
			makeResources("500m", "1Gi", "1", "1Gi"),
			"QoS class"),
		Entry("request raised to its limit, entering the Guaranteed QoS class",
			makeResources("500m", "1Gi", "1", "1Gi"),
			makeResources("500m", "1Gi", "1", "1Gi"),
			makeResources("1", "1Gi", "1", "1Gi"),
			"QoS class"),
		Entry("claims",
			makeResources("500m", "1Gi", "500m", "1Gi"),
			makeResources("500m", "1Gi", "500m", "1Gi"),
			func() corev1.ResourceRequirements {
				resources := makeResources("500m", "1Gi", "500m", "1Gi")
				resources.Claims = []corev1.ResourceClaim{{Name: "gpu"}}
				return resources
			}(),
			"only the cpu and memory values"),
	)

	DescribeTable("compares a memory limit with the one the kubelet enforces",
		func(live, stored, enforced corev1.ResourceRequirements, target string, ok bool) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
				Spec:       *makeSpec(live),
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
					{Name: PostgresContainerName, Resources: &enforced},
					{Name: "sidecar", Resources: &live},
				}},
			}
			resized, _ := ResizePod(pod, makeSpec(stored), makeSpec(makeResources("500m", "1Gi", "500m", target)))
			Expect(resized != nil).To(Equal(ok))
		},
		Entry("a pending increase is reverted: the previous limit is still enforced",
			makeResources("500m", "1Gi", "500m", "8Gi"),
			makeResources("500m", "1Gi", "500m", "8Gi"),
			makeResources("500m", "1Gi", "500m", "2Gi"), "2Gi", true),
		Entry("a decrease when the status reports no memory limit",
			makeResources("500m", "1Gi", "500m", "2Gi"),
			makeResources("500m", "1Gi", "500m", "2Gi"),
			makeResources("500m", "1Gi", "", ""), "1500Mi", false),
	)

	It("finds the limit the kubelet enforces on a native sidecar", func() {
		spec := &corev1.PodSpec{InitContainers: []corev1.Container{
			{Name: "sidecar", RestartPolicy: &always, Resources: makeResources("500m", "1Gi", "500m", "8Gi")},
		}}
		enforced := makeResources("500m", "1Gi", "500m", "2Gi")
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
			Spec:       *spec,
			Status: corev1.PodStatus{InitContainerStatuses: []corev1.ContainerStatus{
				{Name: "sidecar", Resources: &enforced},
			}},
		}
		target := spec.DeepCopy()
		target.InitContainers[0].Resources = enforced
		resized, _ := ResizePod(pod, spec, target)
		Expect(resized).ToNot(BeNil())
	})

	It("ignores the resize policy of a container that does not change", func() {
		stored := makeSpec(makeResources("500m", "1Gi", "1", "2Gi"))
		stored.Containers = append(stored.Containers, corev1.Container{
			Name:         "other",
			ResizePolicy: []corev1.ContainerResizePolicy{{RestartPolicy: corev1.RestartContainer}},
		})
		target := stored.DeepCopy()
		target.Containers[0].Resources = makeResources("500m", "1Gi", "2", "2Gi")
		resized, _, _ := resize(stored, stored, target)
		Expect(resized).ToNot(BeNil())
	})

	It("needs a new pod when the container to resize is missing from the live pod", func() {
		stored := makeSpec(makeResources("500m", "1Gi", "1", "2Gi"))
		live := stored.DeepCopy()
		live.Containers[0].Name = "other"
		resized, _, _ := resize(live, stored, makeSpec(makeResources("500m", "1Gi", "2", "2Gi")))
		Expect(resized).To(BeNil())
	})

	DescribeTable("depends on the resize policy of the changed resources",
		func(policy corev1.ResourceName, target corev1.ResourceRequirements, resizable bool) {
			stored := makeSpec(makeResources("500m", "1Gi", "500m", "1Gi"))
			live := stored.DeepCopy()
			live.Containers[0].ResizePolicy = []corev1.ContainerResizePolicy{
				{ResourceName: policy, RestartPolicy: corev1.RestartContainer},
			}
			resized, _, reason := resize(live, stored, makeSpec(target))
			if resizable {
				Expect(resized).ToNot(BeNil())
				return
			}
			Expect(resized).To(BeNil())
			Expect(reason).To(ContainSubstring("resize policy"))
		},
		Entry("a cpu change with a cpu policy that restarts",
			corev1.ResourceCPU, makeResources("1", "1Gi", "1", "1Gi"), false),
		Entry("a memory change with a memory policy that restarts",
			corev1.ResourceMemory, makeResources("500m", "2Gi", "500m", "2Gi"), false),
		Entry("a cpu change with a memory policy that restarts",
			corev1.ResourceMemory, makeResources("1", "1Gi", "1", "1Gi"), true),
	)

	DescribeTable("restarts the container when only a request or only a limit changes",
		func(target corev1.ResourceRequirements) {
			stored := makeSpec(makeResources("500m", "1Gi", "1", "2Gi"))
			live := stored.DeepCopy()
			live.Containers[0].ResizePolicy = []corev1.ContainerResizePolicy{
				{ResourceName: corev1.ResourceCPU, RestartPolicy: corev1.RestartContainer},
			}
			resized, _, reason := resize(live, stored, makeSpec(target))
			Expect(resized).To(BeNil())
			Expect(reason).To(ContainSubstring("resize policy"))
		},
		Entry("cpu request", makeResources("600m", "1Gi", "1", "2Gi")),
		Entry("cpu limit", makeResources("500m", "1Gi", "2", "2Gi")),
	)

	It("ignores a container that does not change, even if the live pod lacks it", func() {
		stored := makeSpec(makeResources("500m", "1Gi", "1", "2Gi"))
		stored.Containers = append(stored.Containers,
			corev1.Container{Name: "unchanged", Resources: makeResources("100m", "", "", "")})
		live := stored.DeepCopy()
		live.Containers = live.Containers[:1]
		target := stored.DeepCopy()
		target.Containers[0].Resources = makeResources("500m", "1Gi", "2", "2Gi")
		resized, _, _ := resize(live, stored, target)
		Expect(resized).ToNot(BeNil())
	})

	It("resizes native sidecars, not run-once init containers nor the containers of other actors", func() {
		makeSidecarSpec := func(cpu string) *corev1.PodSpec {
			return &corev1.PodSpec{
				InitContainers: []corev1.Container{
					{Name: "init", Resources: makeResources(cpu, "", "", "")},
					{Name: "sidecar", RestartPolicy: &always, Resources: makeResources(cpu, "", "", "")},
				},
				Containers: []corev1.Container{
					{Name: PostgresContainerName, Resources: makeResources("1", "", "", "")},
				},
			}
		}
		stored := makeSidecarSpec("100m")
		live := stored.DeepCopy()
		live.Containers = append(live.Containers,
			corev1.Container{Name: "istio-proxy", Resources: makeResources("300m", "", "", "")})

		resized, recorded, reason := resize(live, stored, makeSidecarSpec("200m"))
		Expect(reason).To(BeEmpty())
		// the run-once init container is recorded, or the pod would look outdated forever
		Expect(recorded.InitContainers[0].Resources.Requests.Cpu().String()).To(Equal("200m"))
		Expect(resized.InitContainers[0].Resources.Requests.Cpu().String()).To(Equal("100m"))
		Expect(resized.InitContainers[1].Resources.Requests.Cpu().String()).To(Equal("200m"))
		Expect(resized.Containers).To(Equal(live.Containers))
	})
})
