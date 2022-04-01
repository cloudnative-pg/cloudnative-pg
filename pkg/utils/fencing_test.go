/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package utils

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Fencing annotation handling", func() {
	jsonMarshal := func(l ...string) string {
		s, err := json.Marshal(l)
		Expect(err).NotTo(HaveOccurred())
		return string(s)
	}

	When("An instance is already fenced", func() {
		It("should correctly remove it when unfenced", func() {
			clusterAnnotations := map[string]string{
				FencedInstanceAnnotation: jsonMarshal("cluster-example-1"),
			}
			err := RemoveFencedInstance("cluster-example-1", clusterAnnotations)
			Expect(err).NotTo(HaveOccurred())
			Expect(clusterAnnotations).NotTo(HaveKey(FencedInstanceAnnotation))
		})
		It("should correctly remove only that instance when unfenced", func() {
			clusterAnnotations := map[string]string{
				FencedInstanceAnnotation: jsonMarshal("cluster-example-1", "cluster-example-2"),
			}
			err := RemoveFencedInstance("cluster-example-1", clusterAnnotations)
			Expect(err).NotTo(HaveOccurred())
			Expect(clusterAnnotations).To(HaveKeyWithValue(FencedInstanceAnnotation, jsonMarshal("cluster-example-2")))
		})
		It("should return an error if tried to fence again", func() {
			clusterAnnotations := map[string]string{
				FencedInstanceAnnotation: jsonMarshal("cluster-example-1", "cluster-example-2"),
			}
			err := AddFencedInstance("cluster-example-1", clusterAnnotations)
			Expect(err).To(HaveOccurred())
			Expect(clusterAnnotations).
				To(HaveKeyWithValue(FencedInstanceAnnotation, jsonMarshal("cluster-example-1", "cluster-example-2")))
		})
	})
	When("An instance is not yet fenced", func() {
		It("should correctly fence it", func() {
			clusterAnnotations := map[string]string{}
			err := AddFencedInstance("cluster-example-1", clusterAnnotations)
			Expect(err).NotTo(HaveOccurred())
			Expect(clusterAnnotations).To(HaveKeyWithValue(FencedInstanceAnnotation, jsonMarshal("cluster-example-1")))
		})
		It("should correctly add it to other fenced instances", func() {
			clusterAnnotations := map[string]string{
				FencedInstanceAnnotation: jsonMarshal("cluster-example-2"),
			}
			err := AddFencedInstance("cluster-example-1", clusterAnnotations)
			Expect(err).NotTo(HaveOccurred())
			Expect(clusterAnnotations).
				To(HaveKeyWithValue(FencedInstanceAnnotation, jsonMarshal("cluster-example-1", "cluster-example-2")))
		})
		It("should return an error if tried to unfence again", func() {
			clusterAnnotations := map[string]string{
				FencedInstanceAnnotation: jsonMarshal("cluster-example-2"),
			}
			err := RemoveFencedInstance("cluster-example-1", clusterAnnotations)
			Expect(err).To(HaveOccurred())
			Expect(clusterAnnotations).
				To(HaveKeyWithValue(FencedInstanceAnnotation, jsonMarshal("cluster-example-2")))
		})
	})
})
