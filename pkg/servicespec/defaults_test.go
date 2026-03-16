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

package servicespec

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PreserveKubernetesDefaults", func() {
	It("should preserve ClusterIP and ClusterIPs", func() {
		proposed := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		living := corev1.ServiceSpec{
			Type:       corev1.ServiceTypeClusterIP,
			Ports:      []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			ClusterIP:  "10.96.0.1",
			ClusterIPs: []string{"10.96.0.1"},
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.ClusterIP).To(Equal("10.96.0.1"))
		Expect(proposed.ClusterIPs).To(Equal([]string{"10.96.0.1"}))
	})

	It("should preserve IPFamilies and IPFamilyPolicy when not set", func() {
		proposed := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		singleStack := corev1.IPFamilyPolicySingleStack
		living := corev1.ServiceSpec{
			Type:           corev1.ServiceTypeClusterIP,
			Ports:          []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			IPFamilies:     []corev1.IPFamily{corev1.IPv4Protocol},
			IPFamilyPolicy: &singleStack,
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.IPFamilies).To(Equal([]corev1.IPFamily{corev1.IPv4Protocol}))
		Expect(proposed.IPFamilyPolicy).To(Equal(&singleStack))
	})

	It("should not override explicitly set IPFamilies", func() {
		dualStack := corev1.IPFamilyPolicyPreferDualStack
		proposed := corev1.ServiceSpec{
			Type:           corev1.ServiceTypeClusterIP,
			Ports:          []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			IPFamilies:     []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			IPFamilyPolicy: &dualStack,
		}
		singleStack := corev1.IPFamilyPolicySingleStack
		living := corev1.ServiceSpec{
			Type:           corev1.ServiceTypeClusterIP,
			Ports:          []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			IPFamilies:     []corev1.IPFamily{corev1.IPv4Protocol},
			IPFamilyPolicy: &singleStack,
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.IPFamilies).To(Equal([]corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol}))
		Expect(proposed.IPFamilyPolicy).To(Equal(&dualStack))
	})

	It("should preserve InternalTrafficPolicy when not set", func() {
		proposed := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		clusterPolicy := corev1.ServiceInternalTrafficPolicyCluster
		living := corev1.ServiceSpec{
			Type:                  corev1.ServiceTypeClusterIP,
			Ports:                 []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			InternalTrafficPolicy: &clusterPolicy,
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.InternalTrafficPolicy).To(Equal(&clusterPolicy))
	})

	It("should preserve SessionAffinity when not set", func() {
		proposed := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		living := corev1.ServiceSpec{
			Type:            corev1.ServiceTypeClusterIP,
			Ports:           []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			SessionAffinity: corev1.ServiceAffinityNone,
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.SessionAffinity).To(Equal(corev1.ServiceAffinityNone))
	})

	It("should not override explicitly set SessionAffinity", func() {
		proposed := corev1.ServiceSpec{
			Type:            corev1.ServiceTypeClusterIP,
			Ports:           []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			SessionAffinity: corev1.ServiceAffinityClientIP,
		}
		living := corev1.ServiceSpec{
			Type:            corev1.ServiceTypeClusterIP,
			Ports:           []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			SessionAffinity: corev1.ServiceAffinityNone,
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.SessionAffinity).To(Equal(corev1.ServiceAffinityClientIP))
	})

	It("should preserve SessionAffinityConfig when not set", func() {
		timeoutSeconds := int32(3600)
		proposed := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		living := corev1.ServiceSpec{
			Type:            corev1.ServiceTypeClusterIP,
			Ports:           []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			SessionAffinity: corev1.ServiceAffinityClientIP,
			SessionAffinityConfig: &corev1.SessionAffinityConfig{
				ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: &timeoutSeconds},
			},
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.SessionAffinityConfig).To(Equal(living.SessionAffinityConfig))
	})

	It("should not override explicitly set SessionAffinityConfig", func() {
		timeout1 := int32(1800)
		timeout2 := int32(3600)
		proposed := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			SessionAffinityConfig: &corev1.SessionAffinityConfig{
				ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: &timeout1},
			},
		}
		living := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			SessionAffinityConfig: &corev1.SessionAffinityConfig{
				ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: &timeout2},
			},
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.SessionAffinityConfig.ClientIP.TimeoutSeconds).To(Equal(&timeout1))
	})

	It("should preserve ExternalTrafficPolicy when not set", func() {
		proposed := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		living := corev1.ServiceSpec{
			Type:                  corev1.ServiceTypeLoadBalancer,
			Ports:                 []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyCluster,
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.ExternalTrafficPolicy).To(Equal(corev1.ServiceExternalTrafficPolicyCluster))
	})

	It("should preserve HealthCheckNodePort when not set", func() {
		proposed := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		living := corev1.ServiceSpec{
			Type:                corev1.ServiceTypeLoadBalancer,
			Ports:               []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			HealthCheckNodePort: 31000,
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.HealthCheckNodePort).To(Equal(int32(31000)))
	})

	It("should not override explicitly set HealthCheckNodePort", func() {
		proposed := corev1.ServiceSpec{
			Type:                corev1.ServiceTypeLoadBalancer,
			Ports:               []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			HealthCheckNodePort: 32000,
		}
		living := corev1.ServiceSpec{
			Type:                corev1.ServiceTypeLoadBalancer,
			Ports:               []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			HealthCheckNodePort: 31000,
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.HealthCheckNodePort).To(Equal(int32(32000)))
	})

	It("should preserve AllocateLoadBalancerNodePorts when not set", func() {
		alloc := true
		proposed := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		living := corev1.ServiceSpec{
			Type:                          corev1.ServiceTypeLoadBalancer,
			Ports:                         []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			AllocateLoadBalancerNodePorts: &alloc,
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.AllocateLoadBalancerNodePorts).To(Equal(&alloc))
	})

	It("should not override explicitly set AllocateLoadBalancerNodePorts", func() {
		allocTrue := true
		allocFalse := false
		proposed := corev1.ServiceSpec{
			Type:                          corev1.ServiceTypeLoadBalancer,
			Ports:                         []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			AllocateLoadBalancerNodePorts: &allocFalse,
		}
		living := corev1.ServiceSpec{
			Type:                          corev1.ServiceTypeLoadBalancer,
			Ports:                         []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			AllocateLoadBalancerNodePorts: &allocTrue,
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.AllocateLoadBalancerNodePorts).To(Equal(&allocFalse))
	})

	It("should preserve LoadbalancerClass when not set", func() {
		lbClass := "load-balancer-class"
		proposed := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		living := corev1.ServiceSpec{
			Type:               corev1.ServiceTypeLoadBalancer,
			Ports:             []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			LoadBalancerClass: &lbClass,
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.LoadBalancerClass).To(Equal(&lbClass))
	})

	It("should not override explicitly set LoadBalancerClass", func() {
		proposedLBClass := "proposed-load-balancer-class"
		livingLBClass := "living-load-balancer-class"
		proposed := corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			Ports:             []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			LoadBalancerClass: &proposedLBClass,
		}
		living := corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			Ports:             []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			LoadBalancerClass: &livingLBClass,
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.LoadBalancerClass).To(Equal(&proposedLBClass))
	})

	It("should preserve TrafficDistribution when not set", func() {
		dist := "PreferClose"
		proposed := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		living := corev1.ServiceSpec{
			Type:                corev1.ServiceTypeClusterIP,
			Ports:               []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			TrafficDistribution: &dist,
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.TrafficDistribution).To(Equal(&dist))
	})

	It("should not override explicitly set TrafficDistribution", func() {
		proposedDist := "PreferClose"
		livingDist := "other"
		proposed := corev1.ServiceSpec{
			Type:                corev1.ServiceTypeClusterIP,
			Ports:               []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			TrafficDistribution: &proposedDist,
		}
		living := corev1.ServiceSpec{
			Type:                corev1.ServiceTypeClusterIP,
			Ports:               []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
			TrafficDistribution: &livingDist,
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.TrafficDistribution).To(Equal(&proposedDist))
	})

	It("should match NodePorts by port and protocol, not by index", func() {
		proposed := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "metrics", Port: 9187, Protocol: corev1.ProtocolTCP},
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP},
			},
		}
		living := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP, NodePort: 30001},
				{Name: "metrics", Port: 9187, Protocol: corev1.ProtocolTCP, NodePort: 30002},
			},
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.Ports[0].NodePort).To(Equal(int32(30002)), "metrics should get NodePort 30002")
		Expect(proposed.Ports[1].NodePort).To(Equal(int32(30001)), "postgres should get NodePort 30001")
	})

	It("should not override explicitly set NodePorts", func() {
		proposed := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP, NodePort: 32000},
			},
		}
		living := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP, NodePort: 30001},
			},
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.Ports[0].NodePort).To(Equal(int32(32000)))
	})

	It("should handle new ports not present in living service", func() {
		proposed := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP},
				{Name: "metrics", Port: 9187, Protocol: corev1.ProtocolTCP},
			},
		}
		living := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP, NodePort: 30001},
			},
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.Ports[0].NodePort).To(Equal(int32(30001)))
		Expect(proposed.Ports[1].NodePort).To(Equal(int32(0)), "new port should have no NodePort")
	})

	It("should preserve Kubernetes-defaulted Protocol and TargetPort", func() {
		proposed := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "custom", Port: 8080},
			},
		}
		living := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "custom", Port: 8080, Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.FromInt32(8080), NodePort: 30001,
				},
			},
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
		Expect(proposed.Ports[0].TargetPort).To(Equal(intstr.FromInt32(8080)))
		Expect(proposed.Ports[0].NodePort).To(Equal(int32(30001)))
	})

	It("should not override explicitly set Protocol and TargetPort", func() {
		proposed := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "custom", Port: 8080, Protocol: corev1.ProtocolUDP,
					TargetPort: intstr.FromInt32(9090),
				},
			},
		}
		living := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "custom", Port: 8080, Protocol: corev1.ProtocolUDP,
					TargetPort: intstr.FromInt32(8080), NodePort: 30001,
				},
			},
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.Ports[0].Protocol).To(Equal(corev1.ProtocolUDP))
		Expect(proposed.Ports[0].TargetPort).To(Equal(intstr.FromInt32(9090)))
	})

	It("should not override explicitly set named string TargetPort", func() {
		proposed := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "custom", Port: 8080, TargetPort: intstr.FromString("http")},
			},
		}
		living := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "custom", Port: 8080, Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.FromInt32(8080), NodePort: 30001,
				},
			},
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.Ports[0].TargetPort).To(Equal(intstr.FromString("http")))
	})

	It("should match ports with empty protocol against TCP living ports", func() {
		proposed := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432},
			},
		}
		living := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP, NodePort: 30001},
			},
		}
		PreserveKubernetesDefaults(&proposed, &living)
		Expect(proposed.Ports[0].NodePort).To(Equal(int32(30001)))
		Expect(proposed.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
	})
})
