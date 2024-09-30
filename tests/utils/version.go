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

package utils

import (
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/image/reference"
	"github.com/cloudnative-pg/machinery/pkg/postgres/version"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

// BumpPostgresImageMajorVersion returns a postgresImage incrementing the major version of the argument (if available)
func BumpPostgresImageMajorVersion(postgresImage string) (string, error) {
	imageReference := reference.New(postgresImage)

	postgresImageVersion, err := version.FromTag(imageReference.Tag)
	if err != nil {
		return "", err
	}

	targetPostgresImageMajorVersionInt := postgresImageVersion.Major() + 1

	defaultImageVersion, err := version.FromTag(reference.GetImageTag(versions.DefaultImageName))
	if err != nil {
		return "", err
	}

	if targetPostgresImageMajorVersionInt >= defaultImageVersion.Major() {
		return postgresImage, nil
	}

	imageReference.Tag = fmt.Sprintf("%d", postgresImageVersion.Major()+1)

	return imageReference.GetNormalizedName(), nil
}
