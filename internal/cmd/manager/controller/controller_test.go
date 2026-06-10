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

package controller

import "testing"

func TestGetWebhookConfigurationNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name               string
		prefix             string
		expectedMutating   string
		expectedValidating string
	}{
		{
			name:               "returns default names without prefix",
			prefix:             "",
			expectedMutating:   MutatingWebhookConfigurationName,
			expectedValidating: ValidatingWebhookConfigurationName,
		},
		{
			name:               "prepends configured prefix",
			prefix:             "my-prefix-",
			expectedMutating:   "my-prefix-" + MutatingWebhookConfigurationName,
			expectedValidating: "my-prefix-" + ValidatingWebhookConfigurationName,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mutating, validating := getWebhookConfigurationNames(tc.prefix)
			if mutating != tc.expectedMutating {
				t.Fatalf("unexpected mutating webhook name, got %q, want %q", mutating, tc.expectedMutating)
			}
			if validating != tc.expectedValidating {
				t.Fatalf("unexpected validating webhook name, got %q, want %q", validating, tc.expectedValidating)
			}
		})
	}
}
