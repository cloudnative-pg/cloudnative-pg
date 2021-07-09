/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	v1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

func pointerToBool(b bool) *bool {
	return &b
}

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
	})

	It("can not set pod affinity if pod anti affinity is disabled", func() {
		config := v1.AffinityConfiguration{
			EnablePodAntiAffinity: pointerToBool(false),
			TopologyKey:           "",
			NodeSelector:          nil,
			Tolerations:           nil,
			PodAntiAffinityType:   "preferred",
		}
		affinity := CreateAffinitySection(clusterName, config)
		Expect(affinity).To(BeNil())
	})

	It("can set pod anti affinity with 'preferred' pod anti-affinity type", func() {
		config := v1.AffinityConfiguration{
			EnablePodAntiAffinity: pointerToBool(true),
			TopologyKey:           "",
			NodeSelector:          nil,
			Tolerations:           nil,
			PodAntiAffinityType:   "preferred",
		}
		affinity := CreateAffinitySection(clusterName, config)
		Expect(affinity.PodAntiAffinity).ToNot(BeNil())
		Expect(affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).ToNot(BeNil())
	})

	It("can set pod anti affinity with 'required' pod anti-affinity type", func() {
		config := v1.AffinityConfiguration{
			EnablePodAntiAffinity: pointerToBool(true),
			TopologyKey:           "",
			NodeSelector:          nil,
			Tolerations:           nil,
			PodAntiAffinityType:   "required",
		}
		affinity := CreateAffinitySection(clusterName, config)
		Expect(affinity.PodAntiAffinity).ToNot(BeNil())
		Expect(affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution).ToNot(BeNil())
	})

	It("can not set pod anti affinity with invalid pod anti-affinity type", func() {
		config := v1.AffinityConfiguration{
			EnablePodAntiAffinity: pointerToBool(false),
			TopologyKey:           "",
			NodeSelector:          nil,
			Tolerations:           nil,
			PodAntiAffinityType:   "not-a-type",
		}
		affinity := CreateAffinitySection(clusterName, config)
		Expect(affinity).To(BeNil())
	})
})
