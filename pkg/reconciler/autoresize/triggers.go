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

package autoresize

import (
	"k8s.io/apimachinery/pkg/api/resource"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// ShouldResize evaluates if a PVC should be resized based on the provided triggers.
// Returns true if either:
// - usedPercent is strictly greater than (not equal to) the usage threshold, OR
// - availableBytes is strictly less than the minAvailable threshold
func ShouldResize(usedPercent float64, availableBytes int64, triggers *apiv1.ResizeTriggers) bool {
	if triggers == nil {
		return false
	}

	// Check usage threshold trigger
	usageThreshold := 80 // default
	if triggers.UsageThreshold != nil {
		usageThreshold = *triggers.UsageThreshold
	}

	if usedPercent > float64(usageThreshold) {
		return true
	}

	// Check minimum available space trigger
	if triggers.MinAvailable != "" {
		minAvailableQty, err := resource.ParseQuantity(triggers.MinAvailable)
		if err != nil {
			autoresizeLog.Info("invalid minAvailable, using percentage trigger only",
				"minAvailable", triggers.MinAvailable, "error", err.Error())
			return usedPercent > float64(usageThreshold)
		}

		minAvailableBytes := minAvailableQty.Value()
		if availableBytes < minAvailableBytes {
			return true
		}
	}

	return false
}
