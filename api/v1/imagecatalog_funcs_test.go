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

package v1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("image catalog", func() {
	catalogSpec := ImageCatalogSpec{
		Images: []CatalogImage{
			{
				Image: "test:15",
				Major: 15,
			},
			{
				Image: "test:16",
				Major: 16,
			},
		},
	}

	It("looks up an image given the major version", func() {
		image, ok := catalogSpec.FindImageForMajor(16)
		Expect(image).To(Equal("test:16"))
		Expect(ok).To(BeTrue())
	})

	It("complains whether the requested image is not specified", func() {
		image, ok := catalogSpec.FindImageForMajor(13)
		Expect(image).To(BeEmpty())
		Expect(ok).To(BeFalse())
	})
})
