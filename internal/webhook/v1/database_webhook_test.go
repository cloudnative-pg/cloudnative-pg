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
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Database validation, prevents duplicate extension names", func() {
	var v *DatabaseCustomValidator

	BeforeEach(func() {
		v = &DatabaseCustomValidator{}
	})

	It("doesn't complain when extensions are null", func() {
		d := &apiv1.Database{
			Spec: apiv1.DatabaseSpec{
				Extensions: nil,
			},
		}

		Expect(v.validateExtensions(d)).To(BeEmpty())
	})

	It("doesn't complain if there are no duplicate extensions", func() {
		d := &apiv1.Database{
			Spec: apiv1.DatabaseSpec{
				Extensions: []apiv1.ExtensionSpec{
					{
						Name:   "postgis",
						Ensure: apiv1.EnsurePresent,
					},
				},
			},
		}

		Expect(v.validateExtensions(d)).To(BeEmpty())
	})

	It("complain if there are duplicate extensions", func() {
		d := &apiv1.Database{
			Spec: apiv1.DatabaseSpec{
				Extensions: []apiv1.ExtensionSpec{
					{
						Name:   "postgis",
						Ensure: apiv1.EnsurePresent,
					},
					{
						Name:   "postgis",
						Ensure: apiv1.EnsurePresent,
					},
					{
						Name:   "cube",
						Ensure: apiv1.EnsurePresent,
					},
				},
			},
		}

		Expect(v.validateExtensions(d)).To(HaveLen(1))
	})
})
