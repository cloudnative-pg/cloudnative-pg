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
	"context"
	"fmt"

	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FirstEndpointSliceIP returns the IP of the first Address in the EndpointSlice
func FirstEndpointSliceIP(endpoint *discoveryv1.EndpointSlice) string {
	if endpoint == nil {
		return ""
	}
	if len(endpoint.Endpoints) == 0 || len(endpoint.Endpoints[0].Addresses) == 0 {
		return ""
	}
	return endpoint.Endpoints[0].Addresses[0]
}

// GetEndpointSliceByServiceName returns the EndpointSlice for a given service name in a given namespace
func GetEndpointSliceByServiceName(
	ctx context.Context,
	crudClient client.Client,
	namespace, serviceName string,
) (*discoveryv1.EndpointSlice, error) {
	endpointSliceList := &discoveryv1.EndpointSliceList{}

	if err := crudClient.List(
		ctx,
		endpointSliceList,
		client.InNamespace(namespace),
		client.MatchingLabels{"kubernetes.io/service-name": serviceName},
	); err != nil {
		return nil, err
	}

	if len(endpointSliceList.Items) == 0 {
		return nil, fmt.Errorf("no endpointslice found for service %s in namespace %s", serviceName, namespace)
	}

	if len(endpointSliceList.Items) > 1 {
		return nil, fmt.Errorf("multiple endpointslice found for service %s in namespace %s", serviceName, namespace)
	}

	return &endpointSliceList.Items[0], nil
}
