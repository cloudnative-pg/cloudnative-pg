/*
Copyright The CloudNativePG Contributors

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

package v1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Base type mappings for secrets", func() {
	It("correctly map nil values", func() {
		Expect(SecretKeySelectorToCore(nil)).To(BeNil())
	})

	It("correctly map non-nil values", func() {
		selector := SecretKeySelector{
			LocalObjectReference: LocalObjectReference{
				Name: "thisName",
			},
			Key: "thisKey",
		}

		Expect(selector.Name).To(Equal("thisName"))
		Expect(selector.Key).To(Equal("thisKey"))
	})
})

var _ = Describe("Base type mappings for configmaps", func() {
	It("correctly map nil values", func() {
		Expect(ConfigMapKeySelectorToCore(nil)).To(BeNil())
	})

	It("correctly map non-nil values", func() {
		selector := ConfigMapKeySelector{
			LocalObjectReference: LocalObjectReference{
				Name: "thisName",
			},
			Key: "thisKey",
		}

		Expect(selector.Name).To(Equal("thisName"))
		Expect(selector.Key).To(Equal("thisKey"))
	})
})
