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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Difference of values of maps", func() {
	p1 := make(map[string]string, 2)
	const testString = "test"
	p1["t"] = testString
	p1["r"] = "rest"
	It("is nil when maps contains same key/value pairs", func() {
		p2 := make(map[string]string, 2)
		p2["t"] = testString
		p2["r"] = "rest"
		Expect(CollectDifferencesFromMaps(p1, p2)).To(BeNil())
	})

	It("is a list of string with difference when maps contains different key/value pairs", func() {
		p2 := make(map[string]string, 2)
		p2["t"] = testString
		p2["a"] = "apple"
		res := CollectDifferencesFromMaps(p1, p2)
		Expect(len(res)).To(BeEquivalentTo(2))
	})
})

var _ = Describe("Set relationship between maps", func() {
	It("An empty map is subset of every possible map", func() {
		Expect(IsMapSubset(nil, nil)).To(BeTrue())
		Expect(IsMapSubset(map[string]string{"one": "1"}, nil)).To(BeTrue())

		Expect(IsMapSubset(nil, map[string]string{"one": "1"})).To(BeFalse())
	})

	It("Two maps containing different elements are not subsets", func() {
		Expect(IsMapSubset(map[string]string{"one": "1"}, map[string]string{"two": "2"})).To(BeFalse())
		Expect(IsMapSubset(map[string]string{"two": "2"}, map[string]string{"one": "1"})).To(BeFalse())
	})

	It("The subset relationship is not invertible", func() {
		Expect(IsMapSubset(map[string]string{"one": "1", "two": "2"}, map[string]string{"two": "2"})).To(BeTrue())
		Expect(IsMapSubset(map[string]string{"two": "2"}, map[string]string{"one": "1", "two": "2"})).To(BeFalse())
	})

	It("Two equal maps are subsets", func() {
		Expect(IsMapSubset(map[string]string{"one": "1"}, map[string]string{"one": "1"})).To(BeTrue())
	})
})

var _ = Describe("Testing Annotations and labels subset", func() {
	const environment = "environment"
	const department = "finance"
	subSet := map[string]string{
		environment: "test",
		department:  "finance",
	}
	set := map[string]string{
		environment:   "test",
		"application": "game-history",
	}

	It("should make sure that a contained annotations subset is recognized", func() {
		ctrl := &fakeInhericanceController{
			annotations: []string{environment},
		}
		isSubset := IsAnnotationSubset(set, subSet, nil, ctrl)
		Expect(isSubset).To(BeTrue())
	})

	It("should make sure that a annotations non-subset is recognized", func() {
		ctrl := &fakeInhericanceController{
			annotations: []string{environment, department},
		}
		isSubset := IsAnnotationSubset(set, subSet, nil, ctrl)
		Expect(isSubset).To(BeFalse())
	})

	It("should make sure fixed annotation is considered in subset", func() {
		ctrl := &fakeInhericanceController{
			annotations: []string{environment},
		}
		isSubset := IsAnnotationSubset(set, subSet,
			map[string]string{"application": "game-history"}, ctrl)
		Expect(isSubset).To(BeTrue())
	})

	It("should make sure fixed annotation is considered in non-subset", func() {
		ctrl := &fakeInhericanceController{
			annotations: []string{environment},
		}
		isSubset := IsAnnotationSubset(set, subSet,
			map[string]string{department: "finance"}, ctrl)
		Expect(isSubset).To(BeFalse())
	})

	It("should make sure that a contained labels subset is recognized", func() {
		ctrl := &fakeInhericanceController{
			labels: []string{environment},
		}
		isSubset := IsLabelSubset(set, subSet, nil, ctrl)
		Expect(isSubset).To(BeTrue())
	})

	It("should make sure that a labels non-subset is recognized", func() {
		ctrl := &fakeInhericanceController{
			labels: []string{environment, department},
		}
		isSubset := IsLabelSubset(set, subSet, nil, ctrl)
		Expect(isSubset).To(BeFalse())
	})

	It("should make sure fixed label is considered in subset", func() {
		ctrl := &fakeInhericanceController{
			labels: []string{environment},
		}
		isSubset := IsLabelSubset(set, subSet,
			map[string]string{"application": "game-history"}, ctrl)
		Expect(isSubset).To(BeTrue())
	})

	It("should make sure fixed label is considered in non-subset", func() {
		ctrl := &fakeInhericanceController{
			labels: []string{environment},
		}
		isSubset := IsLabelSubset(set, subSet,
			map[string]string{department: "finance"}, ctrl)
		Expect(isSubset).To(BeFalse())
	})
})
