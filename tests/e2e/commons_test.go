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

package e2e

import (
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/cloudvendors"
)

func MustGetEnvProfile() cloudvendors.EnvProfile {
	return cloudvendors.GetEnvProfile(*testCloudVendorEnv)
}

// IsAKS checks if the running cluster is on AKS
func IsAKS() bool {
	return *testCloudVendorEnv == cloudvendors.AKS
}

// IsEKS checks if the running cluster is on EKS
func IsEKS() bool {
	return *testCloudVendorEnv == cloudvendors.EKS
}

// IsGKE checks if the running cluster is on GKE
func IsGKE() bool {
	return *testCloudVendorEnv == cloudvendors.GKE
}

// IsLocal checks if the running cluster is on local
func IsLocal() bool {
	return *testCloudVendorEnv == cloudvendors.LOCAL
}

// IsOpenshift checks if the running cluster is on OpenShift
func IsOpenshift() bool {
	return *testCloudVendorEnv == cloudvendors.OCP
}

// IsK3D checks if the running cluster is on K3D
func IsK3D() bool {
	return *testCloudVendorEnv == cloudvendors.K3D
}
