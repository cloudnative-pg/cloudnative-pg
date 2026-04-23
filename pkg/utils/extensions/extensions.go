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

	"github.com/cloudnative-pg/machinery/pkg/envmap"
	"github.com/cloudnative-pg/machinery/pkg/log"

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

// WithUpgradeTargetPrefix returns a deep copy of the extensions with each
// Name prefixed, so target-version entries can coexist with source-version
// ones in the major upgrade Job without volume/mount name collisions.
func WithUpgradeTargetPrefix(exts []apiv1.ExtensionConfiguration) []apiv1.ExtensionConfiguration {
	result := make([]apiv1.ExtensionConfiguration, len(exts))
	for i := range exts {
		result[i] = *exts[i].DeepCopy()
		result[i].Name = postgres.UpgradeTargetExtensionPrefix + exts[i].Name
	}
	return result
}

// dedicatedEnvVars names env vars whose values come from dedicated extension
// fields (LdLibraryPath, BinPath); custom Env entries targeting them are skipped.
var dedicatedEnvVars = map[string]bool{
	"PATH":            true,
	"LD_LIBRARY_PATH": true,
}

// SetEnvVars applies custom Env entries from the given extensions into envMap,
// expanding placeholders. Names reserved for operator use or covered by
// dedicated fields are skipped, and overrides are logged as warnings.
func SetEnvVars(extensionList []apiv1.ExtensionConfiguration, envMap envmap.EnvironmentMap) {
	setBy := make(map[string]string)

	for _, extension := range extensionList {
		for _, envVar := range extension.Env {
			if postgres.IsReservedEnvironmentVariable(envVar.Name) || dedicatedEnvVars[envVar.Name] {
				log.Warning("Skipping reserved environment variable from extension",
					"extension", extension.Name, "variable", envVar.Name)
				continue
			}

			if unknown := postgres.FindUnknownPlaceholders(envVar.Value); len(unknown) > 0 {
				log.Warning("Extension environment variable contains unknown placeholders",
					"extension", extension.Name, "variable", envVar.Name, "unknownPlaceholders", unknown)
			}

			if prev, ok := setBy[envVar.Name]; ok {
				log.Warning("Extension environment variable overrides value from a previous extension",
					"variable", envVar.Name, "extension", extension.Name, "previousExtension", prev)
			} else if _, exists := envMap[envVar.Name]; exists {
				log.Warning("Extension environment variable overrides a cluster-level value",
					"variable", envVar.Name, "extension", extension.Name)
			}

			envMap[envVar.Name] = postgres.ExpandEnvPlaceholders(envVar.Value, extension.Name)
			setBy[envVar.Name] = extension.Name
		}
	}
}

// AppendPaths returns existing (a colon-separated list, possibly empty)
// with extra appended. Returns existing unchanged when extra is empty.
func AppendPaths(existing string, extra []string) string {
	if len(extra) == 0 {
		return existing
	}
	if existing == "" {
		return strings.Join(extra, ":")
	}
	return existing + ":" + strings.Join(extra, ":")
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
