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

import "regexp"

// conditionReasonRegexp is the regular expression that is used inside the Kubernetes API
// to validate the condition reason.
// Reference:
// https://github.com/kubernetes/apimachinery/blob/e74e8a90/pkg/apis/meta/v1/types.go#L1501
var conditionReasonRegexp = regexp.MustCompile(`^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$`)

// IsConditionReasonValid checks if a certain condition reason is valid or not given the
// Kubernetes API requirements
func IsConditionReasonValid(conditionReason string) bool {
	return conditionReasonRegexp.Match([]byte(conditionReason))
}
