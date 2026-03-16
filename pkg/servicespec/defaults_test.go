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
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fullyPopulatedServiceSpec returns a ServiceSpec with every field set to a
// non-zero value. This is used by reflection-based tests to verify that
// ApplyProposedChanges handles ALL fields correctly, including any that may
// be added in future Kubernetes versions.
func fullyPopulatedServiceSpec() corev1.ServiceSpec {
	allocTrue := true
	lbClass := "test-lb-class"
	dist := "PreferClose"
	singleStack := corev1.IPFamilyPolicySingleStack
	internalPolicy := corev1.ServiceInternalTrafficPolicyCluster
	timeoutSeconds := int32(3600)

	return corev1.ServiceSpec{
		Ports: []corev1.ServicePort{
			{
				Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP,
				TargetPort: intstr.FromInt32(5432), NodePort: 30001,
			},
			{
				Name: "metrics", Port: 9187, Protocol: corev1.ProtocolTCP,
				TargetPort: intstr.FromInt32(9187), NodePort: 30002,
			},
		},
		Selector:                 map[string]string{"app": "postgres", "role": "primary"},
		ClusterIP:                "10.96.0.1",
		ClusterIPs:               []string{"10.96.0.1"},
		Type:                     corev1.ServiceTypeLoadBalancer,
		ExternalIPs:              []string{"203.0.113.1"},
		SessionAffinity:          corev1.ServiceAffinityClientIP,
		LoadBalancerIP:           "198.51.100.1",
		LoadBalancerSourceRanges: []string{"10.0.0.0/8", "172.16.0.0/12"},
		ExternalName:             "my.external.service",
		ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyLocal,
		HealthCheckNodePort:      31000,
		PublishNotReadyAddresses: true,
		SessionAffinityConfig: &corev1.SessionAffinityConfig{
			ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: &timeoutSeconds},
		},
		IPFamilies:                    []corev1.IPFamily{corev1.IPv4Protocol},
		IPFamilyPolicy:                &singleStack,
		AllocateLoadBalancerNodePorts: &allocTrue,
		LoadBalancerClass:             &lbClass,
		InternalTrafficPolicy:         &internalPolicy,
		TrafficDistribution:           &dist,
	}
}

var _ = Describe("ApplyProposedChanges", func() {
	It("should preserve every non-zero target field when proposed is empty", func() {
		target := fullyPopulatedServiceSpec()
		proposed := corev1.ServiceSpec{}
		original := target.DeepCopy()

		ApplyProposedChanges(&target, &proposed, nil)

		tv := reflect.ValueOf(target)
		ov := reflect.ValueOf(*original)
		st := reflect.TypeOf(target)

		for i := range tv.NumField() {
			field := st.Field(i)
			tf := tv.Field(i)
			of := ov.Field(i)

			if field.Name == "Ports" {
				Expect(target.Ports).To(Equal(original.Ports),
					fmt.Sprintf("field %s should be preserved", field.Name))
				continue
			}

			if tf.Kind() == reflect.Bool {
				Expect(tf.Bool()).To(BeFalse(),
					fmt.Sprintf("bool field %s should be copied from proposed (false)", field.Name))
				continue
			}

			Expect(tf.Interface()).To(Equal(of.Interface()),
				fmt.Sprintf("field %s should be preserved when proposed is empty", field.Name))
		}
	})

	It("should apply every non-zero proposed field onto empty target", func() {
		target := corev1.ServiceSpec{}
		proposed := fullyPopulatedServiceSpec()

		ApplyProposedChanges(&target, &proposed, nil)

		tv := reflect.ValueOf(target)
		pv := reflect.ValueOf(proposed)
		st := reflect.TypeOf(target)

		for i := range tv.NumField() {
			field := st.Field(i)
			tf := tv.Field(i)
			pf := pv.Field(i)

			if field.Name == "Ports" {
				Expect(target.Ports).To(HaveLen(len(proposed.Ports)),
					"all proposed ports should be applied")
				continue
			}

			Expect(tf.Interface()).To(Equal(pf.Interface()),
				fmt.Sprintf("field %s should be applied from proposed", field.Name))
		}
	})

	It("should be idempotent when proposed equals target", func() {
		target := fullyPopulatedServiceSpec()
		proposed := fullyPopulatedServiceSpec()
		original := target.DeepCopy()

		ApplyProposedChanges(&target, &proposed, nil)

		tv := reflect.ValueOf(target)
		ov := reflect.ValueOf(*original)
		st := reflect.TypeOf(target)

		for i := range tv.NumField() {
			field := st.Field(i)
			Expect(tv.Field(i).Interface()).To(Equal(ov.Field(i).Interface()),
				fmt.Sprintf("field %s should remain unchanged", field.Name))
		}
	})

	It("should confirm fullyPopulatedServiceSpec covers all fields", func() {
		spec := fullyPopulatedServiceSpec()
		sv := reflect.ValueOf(spec)
		st := reflect.TypeOf(spec)

		for i := range sv.NumField() {
			field := st.Field(i)
			Expect(sv.Field(i).IsZero()).To(BeFalse(),
				fmt.Sprintf("fullyPopulatedServiceSpec must set field %s to a non-zero value; "+
					"if this fails after a Kubernetes upgrade, add the new field to the helper", field.Name))
		}
	})

	It("should match NodePorts by port and protocol, not by index", func() {
		target := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP, NodePort: 30001},
				{Name: "metrics", Port: 9187, Protocol: corev1.ProtocolTCP, NodePort: 30002},
			},
		}
		proposed := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "metrics", Port: 9187, Protocol: corev1.ProtocolTCP},
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP},
			},
		}
		ApplyProposedChanges(&target, &proposed, nil)
		Expect(target.Ports[0].NodePort).To(Equal(int32(30002)), "metrics should get NodePort 30002")
		Expect(target.Ports[1].NodePort).To(Equal(int32(30001)), "postgres should get NodePort 30001")
	})

	It("should not override explicitly set NodePorts", func() {
		target := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP, NodePort: 30001},
			},
		}
		proposed := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP, NodePort: 32000},
			},
		}
		ApplyProposedChanges(&target, &proposed, nil)
		Expect(target.Ports[0].NodePort).To(Equal(int32(32000)))
	})

	It("should handle new ports not present in living", func() {
		target := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP, NodePort: 30001},
			},
		}
		proposed := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP},
				{Name: "metrics", Port: 9187, Protocol: corev1.ProtocolTCP},
			},
		}
		ApplyProposedChanges(&target, &proposed, nil)
		Expect(target.Ports[0].NodePort).To(Equal(int32(30001)))
		Expect(target.Ports[1].NodePort).To(Equal(int32(0)), "new port should have no NodePort")
	})

	It("should preserve Kubernetes-defaulted Protocol and TargetPort", func() {
		target := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "custom", Port: 8080, Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.FromInt32(8080), NodePort: 30001,
				},
			},
		}
		proposed := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "custom", Port: 8080},
			},
		}
		ApplyProposedChanges(&target, &proposed, nil)
		Expect(target.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
		Expect(target.Ports[0].TargetPort).To(Equal(intstr.FromInt32(8080)))
		Expect(target.Ports[0].NodePort).To(Equal(int32(30001)))
	})

	It("should not override explicitly set Protocol and TargetPort", func() {
		target := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "custom", Port: 8080, Protocol: corev1.ProtocolUDP,
					TargetPort: intstr.FromInt32(8080), NodePort: 30001,
				},
			},
		}
		proposed := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "custom", Port: 8080, Protocol: corev1.ProtocolUDP,
					TargetPort: intstr.FromInt32(9090),
				},
			},
		}
		ApplyProposedChanges(&target, &proposed, nil)
		Expect(target.Ports[0].Protocol).To(Equal(corev1.ProtocolUDP))
		Expect(target.Ports[0].TargetPort).To(Equal(intstr.FromInt32(9090)))
	})

	It("should not override explicitly set named string TargetPort", func() {
		target := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "custom", Port: 8080, Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.FromInt32(8080), NodePort: 30001,
				},
			},
		}
		proposed := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "custom", Port: 8080, TargetPort: intstr.FromString("http")},
			},
		}
		ApplyProposedChanges(&target, &proposed, nil)
		Expect(target.Ports[0].TargetPort).To(Equal(intstr.FromString("http")))
	})

	It("should match ports with empty protocol against TCP living ports", func() {
		target := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP, NodePort: 30001},
			},
		}
		proposed := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432},
			},
		}
		ApplyProposedChanges(&target, &proposed, nil)
		Expect(target.Ports[0].NodePort).To(Equal(int32(30001)))
		Expect(target.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
	})

	It("should handle port removal (proposed has fewer ports)", func() {
		target := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP, NodePort: 30001},
				{Name: "metrics", Port: 9187, Protocol: corev1.ProtocolTCP, NodePort: 30002},
			},
		}
		proposed := corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP},
			},
		}
		ApplyProposedChanges(&target, &proposed, nil)
		Expect(target.Ports).To(HaveLen(1))
		Expect(target.Ports[0].Name).To(Equal("postgres"))
		Expect(target.Ports[0].NodePort).To(Equal(int32(30001)))
	})

	It("should remove a field when lastApplied had it but proposed does not", func() {
		target := corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeLoadBalancer,
			LoadBalancerSourceRanges: []string{"10.0.0.0/8"},
			Ports:                    []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		lastApplied := corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeLoadBalancer,
			LoadBalancerSourceRanges: []string{"10.0.0.0/8"},
			Ports:                    []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		proposed := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		ApplyProposedChanges(&target, &proposed, &lastApplied)
		Expect(target.LoadBalancerSourceRanges).To(BeNil(),
			"should be cleared because lastApplied had it but proposed doesn't")
	})

	It("should preserve provider field when neither lastApplied nor proposed set it", func() {
		lbClass := "cloud-provider-class"
		target := corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
			Ports: []corev1.ServicePort{{
				Port: 5432, Name: "postgres", Protocol: corev1.ProtocolTCP, NodePort: 30001,
			}},
		}
		lastApplied := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		proposed := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		ApplyProposedChanges(&target, &proposed, &lastApplied)
		Expect(target.LoadBalancerClass).To(Equal(&lbClass),
			"should be preserved because neither lastApplied nor proposed set it")
	})

	It("should detect removal via lastApplied using reflection for all fields", func() {
		lastApplied := fullyPopulatedServiceSpec()
		proposed := corev1.ServiceSpec{}
		target := fullyPopulatedServiceSpec()

		ApplyProposedChanges(&target, &proposed, &lastApplied)

		tv := reflect.ValueOf(target)
		st := reflect.TypeOf(target)

		for i := range tv.NumField() {
			field := st.Field(i)
			tf := tv.Field(i)

			if field.Name == "Ports" {
				Expect(target.Ports).To(BeNil(),
					fmt.Sprintf("field %s should be cleared (was in lastApplied, not in proposed)", field.Name))
				continue
			}

			if tf.Kind() == reflect.Bool {
				Expect(tf.Bool()).To(BeFalse(),
					fmt.Sprintf("bool field %s should be false (from proposed)", field.Name))
				continue
			}

			Expect(tf.IsZero()).To(BeTrue(),
				fmt.Sprintf("field %s should be cleared (was in lastApplied, not in proposed)", field.Name))
		}
	})

	It("should NOT accidentally remove a provider field when lastApplied is stale", func() {
		lbClass := "provider-set-after-last-apply"
		target := corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
			ClusterIP:         "10.96.0.1",
		}
		lastApplied := corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
		}
		proposed := corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
		}
		ApplyProposedChanges(&target, &proposed, &lastApplied)
		Expect(target.LoadBalancerClass).To(Equal(&lbClass),
			"provider-set field must survive: neither lastApplied nor proposed touched it")
		Expect(target.ClusterIP).To(Equal("10.96.0.1"))
	})

	It("should remove a field even if living was externally modified", func() {
		target := corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeLoadBalancer,
			LoadBalancerSourceRanges: []string{"EXTERNALLY-MODIFIED"},
		}
		lastApplied := corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeLoadBalancer,
			LoadBalancerSourceRanges: []string{"10.0.0.0/8"},
		}
		proposed := corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
		}
		ApplyProposedChanges(&target, &proposed, &lastApplied)
		Expect(target.LoadBalancerSourceRanges).To(BeNil(),
			"we owned this field (in lastApplied), so removing from proposed should clear it")
	})

	It("should not corrupt target when lastApplied has partial data", func() {
		lbClass := "living-class"
		internalPolicy := corev1.ServiceInternalTrafficPolicyCluster
		target := corev1.ServiceSpec{
			Type:                  corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass:     &lbClass,
			InternalTrafficPolicy: &internalPolicy,
			ClusterIP:             "10.96.0.1",
			HealthCheckNodePort:   31000,
			Ports:                 []corev1.ServicePort{{Port: 5432, Name: "postgres", NodePort: 30001}},
		}
		lastApplied := corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
		}
		proposed := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		ApplyProposedChanges(&target, &proposed, &lastApplied)
		Expect(target.LoadBalancerClass).To(Equal(&lbClass))
		Expect(target.InternalTrafficPolicy).To(Equal(&internalPolicy))
		Expect(target.ClusterIP).To(Equal("10.96.0.1"))
		Expect(target.HealthCheckNodePort).To(Equal(int32(31000)))
		Expect(target.Ports[0].NodePort).To(Equal(int32(30001)))
	})

	It("should handle multi-reconciliation cycle: add → update → remove", func() {
		living := corev1.ServiceSpec{
			Type:      corev1.ServiceTypeLoadBalancer,
			ClusterIP: "10.96.0.1",
			Ports:     []corev1.ServicePort{{Port: 5432, Name: "postgres", NodePort: 30001}},
		}
		proposed1 := corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeLoadBalancer,
			LoadBalancerSourceRanges: []string{"10.0.0.0/8"},
			Ports:                    []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}

		target1 := living.DeepCopy()
		ApplyProposedChanges(target1, &proposed1, nil)
		Expect(target1.LoadBalancerSourceRanges).To(Equal([]string{"10.0.0.0/8"}))
		Expect(target1.ClusterIP).To(Equal("10.96.0.1"))
		Expect(target1.Ports[0].NodePort).To(Equal(int32(30001)))

		proposed2 := corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeLoadBalancer,
			LoadBalancerSourceRanges: []string{"172.16.0.0/12"},
			Ports:                    []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		target2 := target1.DeepCopy()
		ApplyProposedChanges(target2, &proposed2, &proposed1)
		Expect(target2.LoadBalancerSourceRanges).To(Equal([]string{"172.16.0.0/12"}))
		Expect(target2.ClusterIP).To(Equal("10.96.0.1"))

		proposed3 := corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 5432, Name: "postgres"}},
		}
		target3 := target2.DeepCopy()
		ApplyProposedChanges(target3, &proposed3, &proposed2)
		Expect(target3.LoadBalancerSourceRanges).To(BeNil(),
			"ranges should be cleared after being removed from proposed")
		Expect(target3.ClusterIP).To(Equal("10.96.0.1"),
			"ClusterIP should survive all cycles")
		Expect(target3.Ports[0].NodePort).To(Equal(int32(30001)),
			"NodePort should survive all cycles")
	})
})

var _ = Describe("GetLastApplied / SetLastApplied", func() {
	It("should round-trip a service spec through annotations", func() {
		spec := fullyPopulatedServiceSpec()
		meta := metav1.ObjectMeta{}

		SetLastApplied(&meta, &spec)

		result := GetLastApplied(meta.Annotations)
		Expect(result).NotTo(BeNil())
		Expect(result.Type).To(Equal(spec.Type))
		Expect(result.LoadBalancerSourceRanges).To(Equal(spec.LoadBalancerSourceRanges))
		Expect(result.Ports).To(HaveLen(len(spec.Ports)))
		Expect(result.LoadBalancerClass).To(Equal(spec.LoadBalancerClass))
	})

	It("should return nil for missing annotation", func() {
		annotations := make(map[string]string)
		Expect(GetLastApplied(annotations)).To(BeNil())
	})

	It("should return nil for invalid JSON", func() {
		annotations := map[string]string{
			"cnpg.io/lastAppliedSpec": "not-json",
		}
		Expect(GetLastApplied(annotations)).To(BeNil())
	})
})
