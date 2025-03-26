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

package utils

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
			clusterMeta := metav1.ObjectMeta{
				Annotations: map[string]string{
					FencedInstanceAnnotation: jsonMarshal("cluster-example-1"),
				},
			}
			modified, err := removeFencedInstance("cluster-example-1", &clusterMeta)
			Expect(err).NotTo(HaveOccurred())
			Expect(modified).To(BeTrue())
			Expect(clusterMeta.Annotations).NotTo(HaveKey(FencedInstanceAnnotation))
		})
		It("should correctly remove only that instance when unfenced", func() {
			clusterMeta := metav1.ObjectMeta{
				Annotations: map[string]string{
					FencedInstanceAnnotation: jsonMarshal("cluster-example-1", "cluster-example-2"),
				},
			}
			modified, err := removeFencedInstance("cluster-example-1", &clusterMeta)
			Expect(err).NotTo(HaveOccurred())
			Expect(modified).To(BeTrue())
			Expect(clusterMeta.Annotations).
				ToNot(HaveKeyWithValue(FencedInstanceAnnotation, jsonMarshal("cluster-example-1")))
			Expect(clusterMeta.Annotations).
				To(HaveKeyWithValue(FencedInstanceAnnotation, jsonMarshal("cluster-example-2")))
		})

		It("should not return an error if tried to fence again", func() {
			clusterMeta := metav1.ObjectMeta{
				Annotations: map[string]string{
					FencedInstanceAnnotation: jsonMarshal("cluster-example-1", "cluster-example-2"),
				},
			}
			modified, err := AddFencedInstance("cluster-example-1", &clusterMeta)
			Expect(err).ToNot(HaveOccurred())
			Expect(modified).To(BeFalse())
			Expect(clusterMeta.Annotations).
				To(HaveKeyWithValue(FencedInstanceAnnotation, jsonMarshal("cluster-example-1", "cluster-example-2")))
		})
	})
	When("An instance is not yet fenced", func() {
		It("should correctly fence it", func() {
			clusterMeta := metav1.ObjectMeta{
				Annotations: map[string]string{},
			}
			modified, err := AddFencedInstance("cluster-example-1", &clusterMeta)
			Expect(err).NotTo(HaveOccurred())
			Expect(modified).To(BeTrue())
			Expect(clusterMeta.Annotations).
				To(HaveKeyWithValue(FencedInstanceAnnotation, jsonMarshal("cluster-example-1")))
		})
		It("should correctly add it to other fenced instances", func() {
			clusterMeta := metav1.ObjectMeta{
				Annotations: map[string]string{
					FencedInstanceAnnotation: jsonMarshal("cluster-example-2"),
				},
			}
			modified, err := AddFencedInstance("cluster-example-1", &clusterMeta)
			Expect(err).NotTo(HaveOccurred())
			Expect(modified).To(BeTrue())
			Expect(clusterMeta.Annotations).
				To(HaveKeyWithValue(FencedInstanceAnnotation, jsonMarshal("cluster-example-1", "cluster-example-2")))
		})
		It("should not return an error if tried to unfence again", func() {
			clusterMeta := metav1.ObjectMeta{
				Annotations: map[string]string{
					FencedInstanceAnnotation: jsonMarshal("cluster-example-2"),
				},
			}
			modified, err := removeFencedInstance("cluster-example-1", &clusterMeta)
			Expect(err).ToNot(HaveOccurred())
			Expect(modified).To(BeFalse())
			Expect(clusterMeta.Annotations).
				To(HaveKeyWithValue(FencedInstanceAnnotation, jsonMarshal("cluster-example-2")))
		})
	})
	When("An instance has no annotations", func() {
		It("should correctly fence it", func() {
			clusterMeta := metav1.ObjectMeta{}
			modified, err := AddFencedInstance("cluster-example-1", &clusterMeta)
			Expect(err).NotTo(HaveOccurred())
			Expect(modified).To(BeTrue())
			Expect(clusterMeta.Annotations).
				To(HaveKeyWithValue(FencedInstanceAnnotation, jsonMarshal("cluster-example-1")))
		})
	})
})
