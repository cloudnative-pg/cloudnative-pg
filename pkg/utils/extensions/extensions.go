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

// Package extensions provides utilities for resolving PostgreSQL extension configurations
// from image catalogs and cluster specifications
package extensions

import (
	"fmt"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// ResolveFromCatalog returns a list of requested Extensions from a Catalog for a given
// Postgres major version.
// If present, extension entries present in the catalog are always used as a starting base.
// If additional configuration is passed via `cluster.Spec.PostgresConfiguration.Extensions`
// for an extension that's already defined in the catalog, the values defined in the Cluster
// take precedence.
func ResolveFromCatalog(
	cluster *apiv1.Cluster,
	catalog apiv1.GenericImageCatalog,
	requestedMajorVersion int,
) ([]apiv1.ExtensionConfiguration, error) {
	requestedExtensions := cluster.Spec.PostgresConfiguration.Extensions
	resolvedExtensions := make([]apiv1.ExtensionConfiguration, 0, len(requestedExtensions))

	// Build a map of extensions coming from the catalog
	var catalogExtensionsMap map[string]apiv1.ExtensionConfiguration
	if extensions, ok := catalog.GetSpec().FindExtensionsForMajor(requestedMajorVersion); ok {
		catalogExtensionsMap = make(map[string]apiv1.ExtensionConfiguration, len(extensions))
		for _, extension := range extensions {
			catalogExtensionsMap[extension.Name] = extension
		}
	}

	// Resolve extensions
	for _, extension := range requestedExtensions {
		catalogExtension, found := catalogExtensionsMap[extension.Name]

		// Validate that the ImageVolumeSource.Reference is properly defined.
		// We want to allow overriding each field of an extension defined in a catalog,
		// meaning that even the ImageVolumeSource.Reference is defined as an optional field,
		// although it must be defined either in the catalog or in the Cluster Spec.

		// Case 1. This case is also covered by the validateExtensions cluster webhook, but it doesn't
		// hurt to have it here as well.
		if !found && extension.ImageVolumeSource.Reference == "" {
			return []apiv1.ExtensionConfiguration{}, fmt.Errorf(
				"extension %q found in the Cluster Spec but no ImageVolumeSource.Reference is defined", extension.Name)
		}

		// Case 2. This case must be handled here because we don't have a validation webhook for the catalog,
		// but it could be moved there if we decide to add one.
		if found && catalogExtension.ImageVolumeSource.Reference == "" && extension.ImageVolumeSource.Reference == "" {
			return []apiv1.ExtensionConfiguration{}, fmt.Errorf(
				"extension %q found in image catalog %s but no ImageVolumeSource.Reference is defined "+
					"in both the image catalog and the Cluster Spec",
				extension.Name, apiv1.CatalogIdentifier(catalog),
			)
		}

		if !found {
			// No catalog entry, rely fully on the Cluster Spec
			resolvedExtensions = append(resolvedExtensions, extension)
			continue
		}

		// Found the extension in the catalog, so let's use the catalog entry as a base
		// and eventually override it with Cluster Spec values
		resultExtension := catalogExtension

		// Apply the Cluster Spec overrides
		if extension.ImageVolumeSource.Reference != "" {
			resultExtension.ImageVolumeSource.Reference = extension.ImageVolumeSource.Reference
		}
		if extension.ImageVolumeSource.PullPolicy != "" {
			resultExtension.ImageVolumeSource.PullPolicy = extension.ImageVolumeSource.PullPolicy
		}
		if len(extension.ExtensionControlPath) > 0 {
			resultExtension.ExtensionControlPath = extension.ExtensionControlPath
		}
		if len(extension.DynamicLibraryPath) > 0 {
			resultExtension.DynamicLibraryPath = extension.DynamicLibraryPath
		}
		if len(extension.LdLibraryPath) > 0 {
			resultExtension.LdLibraryPath = extension.LdLibraryPath
		}
		if len(extension.BinPath) > 0 {
			resultExtension.BinPath = extension.BinPath
		}
		if len(extension.Env) > 0 {
			resultExtension.Env = extension.Env
		}

		resolvedExtensions = append(resolvedExtensions, resultExtension)
	}

	return resolvedExtensions, nil
}

// ValidateWithoutCatalog returns extensions when cluster uses imageName directly.
// In this case, all extensions must be fully specified in the cluster spec.
func ValidateWithoutCatalog(cluster *apiv1.Cluster) ([]apiv1.ExtensionConfiguration, error) {
	extensions := cluster.Spec.PostgresConfiguration.Extensions

	// Validate that all extensions have ImageVolumeSource.Reference defined
	for _, extension := range extensions {
		if extension.ImageVolumeSource.Reference == "" {
			return nil, fmt.Errorf(
				"extension %q requires ImageVolumeSource.Reference when not using image catalog",
				extension.Name)
		}
	}

	return extensions, nil
}

// CollectLibraryPaths returns a list of paths which should be added to LD_LIBRARY_PATH
// given a list of extensions.
// NOTE: filepath.Join normalizes user-supplied paths (e.g. leading "/", "./" or
// trailing "/" are cleaned), so "/lib", "./lib", and "lib" all resolve to the
// same directory under the extension mount point.
func CollectLibraryPaths(extensionList []apiv1.ExtensionConfiguration) []string {
	capacity := 0
	for _, ext := range extensionList {
		capacity += len(ext.LdLibraryPath)
	}
	result := make([]string, 0, capacity)

	for _, extension := range extensionList {
		for _, libraryPath := range extension.LdLibraryPath {
			result = append(
				result,
				filepath.Join(postgres.ExtensionsBaseDirectory, extension.Name, libraryPath),
			)
		}
	}

	return result
}

// CollectBinPaths returns a list of paths which should be added to PATH
// given a list of extensions.
// NOTE: filepath.Join normalizes user-supplied paths (e.g. leading "/", "./" or
// trailing "/" are cleaned), so "/bin", "./bin", and "bin" all resolve to the
// same directory under the extension mount point.
func CollectBinPaths(extensionList []apiv1.ExtensionConfiguration) []string {
	capacity := 0
	for _, ext := range extensionList {
		capacity += len(ext.BinPath)
	}
	result := make([]string, 0, capacity)

	for _, extension := range extensionList {
		for _, binPath := range extension.BinPath {
			result = append(
				result,
				filepath.Join(postgres.ExtensionsBaseDirectory, extension.Name, binPath),
			)
		}
	}

	return result
}


// GetExtensionEnvVars returns a list of environment variables
// derived from the given extensions' library and binary paths.
func GetExtensionEnvVars(extList []apiv1.ExtensionConfiguration) []corev1.EnvVar {
	var result []corev1.EnvVar

	libraryPaths := CollectLibraryPaths(extList)
	if len(libraryPaths) > 0 {
		result = append(result, corev1.EnvVar{
			Name:  "LD_LIBRARY_PATH",
			Value: strings.Join(libraryPaths, ":"),
		})
	}

	binPaths := CollectBinPaths(extList)
	if len(binPaths) > 0 {
		result = append(result, corev1.EnvVar{
			Name:  "PATH",
			Value: strings.Join(binPaths, ":"),
		})
	}

	return result
}
