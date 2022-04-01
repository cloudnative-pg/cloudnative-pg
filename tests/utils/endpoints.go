/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
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
