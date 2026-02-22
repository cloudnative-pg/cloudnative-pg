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
	corev1 "k8s.io/api/core/v1"

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
				Extensions: []ExtensionConfiguration{
					{
						Name: "postgis",
						ImageVolumeSource: corev1.ImageVolumeSource{
							Reference: "postgis:latest",
						},
					},
					{
						Name: "pgvector",
						ImageVolumeSource: corev1.ImageVolumeSource{
							Reference: "pgvector:0.8.0",
						},
					},
				},
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

	It("looks up extensions given the major version", func() {
		extensions, ok := catalogSpec.FindExtensionsForMajor(16)
		Expect(ok).To(BeTrue())
		Expect(extensions).To(HaveLen(2))
		Expect(extensions[0].Name).To(Equal("postgis"))
		Expect(extensions[0].ImageVolumeSource.Reference).To(Equal("postgis:latest"))
		Expect(extensions[1].Name).To(Equal("pgvector"))
		Expect(extensions[1].ImageVolumeSource.Reference).To(Equal("pgvector:0.8.0"))
	})

	It("returns empty extensions when major version has no extensions", func() {
		extensions, ok := catalogSpec.FindExtensionsForMajor(15)
		Expect(ok).To(BeTrue())
		Expect(extensions).To(BeEmpty())
	})

	It("returns false when major version is not found", func() {
		extensions, ok := catalogSpec.FindExtensionsForMajor(13)
		Expect(ok).To(BeFalse())
		Expect(extensions).To(BeNil())
	})
})
