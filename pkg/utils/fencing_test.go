/*
Copyright 2019-2022 The CloudNativePG Contributors

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
