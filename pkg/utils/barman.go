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
	"fmt"
	"strings"
)

// SetS3RegionEnv appends the AWS_DEFAULT_REGION environment variable to the
// provided env slice if the region string is not empty.
func SetS3RegionEnv(env []string, region string) []string {
	if region == "" {
		return env
	}

	// We don't want to override if already set, but we want to make sure it's present
	regionEnv := fmt.Sprintf("AWS_DEFAULT_REGION=%s", region)

	for _, e := range env {
		if strings.HasPrefix(e, "AWS_DEFAULT_REGION=") {
			return env
		}
	}

	return append(env, regionEnv)
}
