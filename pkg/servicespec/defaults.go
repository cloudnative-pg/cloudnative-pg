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
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// PreserveKubernetesDefaults copies Kubernetes-managed fields from the living
// service spec into the proposed one, so that a DeepEqual comparison only
// detects changes the operator actually controls.
func PreserveKubernetesDefaults(proposed, living *corev1.ServiceSpec) {
	// Always assigned by Kubernetes
	proposed.ClusterIP = living.ClusterIP
	proposed.ClusterIPs = slices.Clone(living.ClusterIPs)

	// Defaulted at creation time
	if len(proposed.IPFamilies) == 0 {
		proposed.IPFamilies = slices.Clone(living.IPFamilies)
	}
	if proposed.IPFamilyPolicy == nil {
		proposed.IPFamilyPolicy = living.IPFamilyPolicy
	}
	if proposed.InternalTrafficPolicy == nil {
		proposed.InternalTrafficPolicy = living.InternalTrafficPolicy
	}
	if proposed.SessionAffinity == "" {
		proposed.SessionAffinity = living.SessionAffinity
	}
	if proposed.SessionAffinityConfig == nil {
		proposed.SessionAffinityConfig = living.SessionAffinityConfig
	}
	if proposed.ExternalTrafficPolicy == "" {
		proposed.ExternalTrafficPolicy = living.ExternalTrafficPolicy
	}
	if proposed.HealthCheckNodePort == 0 {
		proposed.HealthCheckNodePort = living.HealthCheckNodePort
	}
	if proposed.AllocateLoadBalancerNodePorts == nil {
		proposed.AllocateLoadBalancerNodePorts = living.AllocateLoadBalancerNodePorts
	}
	if proposed.LoadBalancerClass == nil {
		proposed.LoadBalancerClass = living.LoadBalancerClass
	}
	if proposed.TrafficDistribution == nil {
		proposed.TrafficDistribution = living.TrafficDistribution
	}

	preservePortDefaults(proposed.Ports, living.Ports)
}

// preservePortDefaults preserves Kubernetes-defaulted and Kubernetes-assigned
// fields in service ports, matching by the strategic merge key (Port, Protocol).
func preservePortDefaults(proposed, living []corev1.ServicePort) {
	type portKey struct {
		port     int32
		protocol corev1.Protocol
	}
	key := func(p corev1.ServicePort) portKey {
		protocol := p.Protocol
		if protocol == "" {
			protocol = corev1.ProtocolTCP
		}
		return portKey{port: p.Port, protocol: protocol}
	}

	livingPorts := make(map[portKey]*corev1.ServicePort, len(living))
	for i := range living {
		livingPorts[key(living[i])] = &living[i]
	}

	for i := range proposed {
		lp, ok := livingPorts[key(proposed[i])]
		if !ok {
			continue
		}
		if proposed[i].Protocol == "" {
			proposed[i].Protocol = lp.Protocol
		}
		if proposed[i].TargetPort == (intstr.IntOrString{}) {
			proposed[i].TargetPort = lp.TargetPort
		}
		if proposed[i].NodePort == 0 {
			proposed[i].NodePort = lp.NodePort
		}
	}
}
