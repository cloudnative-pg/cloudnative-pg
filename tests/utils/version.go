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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

// BumpPostgresImageMajorVersion returns a postgresImage incrementing the major version of the argument (if available)
func BumpPostgresImageMajorVersion(postgresImage string) (string, error) {
	imageReference := apiv1.NewReference(postgresImage)

	postgresImageVersion, err := apiv1.GetPostgresVersionFromTag(imageReference.Tag)
	if err != nil {
		return "", err
	}

	targetPostgresImageVersionInt := postgresImageVersion + 1_00_00

	defaultImageVersion, err := apiv1.GetPostgresVersionFromTag(apiv1.GetImageTag(versions.DefaultImageName))
	if err != nil {
		return "", err
	}

	if targetPostgresImageVersionInt >= defaultImageVersion {
		return postgresImage, nil
	}

	imageReference.Tag = fmt.Sprintf("%d", postgresImageVersion/10000+1)

	return imageReference.GetNormalizedName(), nil
}
