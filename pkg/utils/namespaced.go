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

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
)

// ValidateNamespacedConfiguration validates that when namespaced mode is enabled,
// the operator namespace and watch namespace must be equal and non-empty
func ValidateNamespacedConfiguration(conf *configuration.Data) error {
	if !conf.Namespaced {
		return nil
	}

	if conf.OperatorNamespace == "" {
		return fmt.Errorf("when namespaced is enabled, operator namespace cannot be empty")
	}

	if conf.WatchNamespace == "" {
		return fmt.Errorf("when namespaced is enabled, watch namespace cannot be empty")
	}

	if conf.OperatorNamespace != conf.WatchNamespace {
		return fmt.Errorf(
			"when namespaced is enabled, operator namespace (%s) and watch namespace (%s) must be equal",
			conf.OperatorNamespace,
			conf.WatchNamespace,
		)
	}

	return nil
}
