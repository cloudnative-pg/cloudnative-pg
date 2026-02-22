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

// GetSpec returns the Spec of the ImageCatalog
func (c *ImageCatalog) GetSpec() *ImageCatalogSpec {
	return &c.Spec
}

// CatalogIdentifier returns a human-readable Kind/Name identifier
// for a catalog, e.g. "ImageCatalog/my-catalog".
func CatalogIdentifier(catalog GenericImageCatalog) string {
	var kind string
	switch catalog.(type) {
	case *ImageCatalog:
		kind = ImageCatalogKind
	case *ClusterImageCatalog:
		kind = ClusterImageCatalogKind
	default:
		kind = "UnknownCatalog"
	}
	return kind + "/" + catalog.GetName()
}

// FindImageForMajor finds the correct image for the selected major version
func (spec *ImageCatalogSpec) FindImageForMajor(major int) (string, bool) {
	for _, entry := range spec.Images {
		if entry.Major == major {
			return entry.Image, true
		}
	}

	return "", false
}

// FindExtensionsForMajor finds the extensions for the selected major version
func (spec *ImageCatalogSpec) FindExtensionsForMajor(major int) ([]ExtensionConfiguration, bool) {
	for _, entry := range spec.Images {
		if entry.Major == major {
			return entry.Extensions, true
		}
	}

	return nil, false
}
