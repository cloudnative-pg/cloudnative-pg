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

import corev1 "k8s.io/api/core/v1"

// FirstEndpointIP returns the IP of first Address in the Endpoint
func FirstEndpointIP(endpoint *corev1.Endpoints) string {
	if endpoint == nil {
		return ""
	}
	if len(endpoint.Subsets) == 0 || len(endpoint.Subsets[0].Addresses) == 0 {
		return ""
	}
	return endpoint.Subsets[0].Addresses[0].IP
}
