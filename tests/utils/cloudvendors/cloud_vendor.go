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

// Package cloudvendors provides the variables to define on which cloud vendor the e2e test is running
package cloudvendors

import (
	"fmt"
	"os"
)

// TestEnvVendor is the type of cloud vendor the e2e test is running on
type TestEnvVendor string

// testVendorEnvVarName holds the env variable name used externally to
// define a specific cloud vendor
const testVendorEnvVarName = "TEST_CLOUD_VENDOR"

// AKS azure cloud cluster
var AKS = TestEnvVendor("aks")

// EKS amazon elastic cloud cluster
var EKS = TestEnvVendor("eks")

// GKE google cloud cluster
var GKE = TestEnvVendor("gke")

// LOCAL kind cluster running locally
var LOCAL = TestEnvVendor("local")

// OCP openshift cloud cluster
var OCP = TestEnvVendor("ocp")

// K3D local k3d cluster
var K3D = TestEnvVendor("k3d")

var vendors = map[string]*TestEnvVendor{
	"aks":   &AKS,
	"eks":   &EKS,
	"gke":   &GKE,
	"local": &LOCAL,
	"k3d":   &K3D,
	"ocp":   &OCP,
}

// TestCloudVendor creates the environment for testing
func TestCloudVendor() (*TestEnvVendor, error) {
	vendorEnv, exists := os.LookupEnv(testVendorEnvVarName)
	if exists {
		if vendor, ok := vendors[vendorEnv]; ok {
			return vendor, nil
		}
		return nil, fmt.Errorf("unknown cloud vendor %s", vendorEnv)
	}

	// if none above, it is a local
	return &LOCAL, nil
}

// EnvProfile represents the capabilities of different cloud environments for testing
type EnvProfile interface {
	CanMovePVCAcrossNodes() bool
	IsLeaderElectionEnabled() bool
	CanRunAppArmor() bool
	UsesNodeDiskSpace() bool
}

// GetEnvProfile returns a cloud environment's capabilities envProfile
func GetEnvProfile(te TestEnvVendor) EnvProfile {
	profileMap := map[TestEnvVendor]EnvProfile{
		LOCAL: envProfile{
			isLeaderElectionEnabled: true,
			usesNodeDiskSpace:       true,
		},
		K3D: envProfile{
			isLeaderElectionEnabled: true,
			usesNodeDiskSpace:       true,
		},
		AKS: envProfile{
			canMovePVCAcrossNodes:   true,
			isLeaderElectionEnabled: true,
			canRunAppArmor:          true,
		},
		EKS: envProfile{
			isLeaderElectionEnabled: true,
		},
		GKE: envProfile{},
		OCP: envProfile{
			isLeaderElectionEnabled: true,
		},
	}

	profile, found := profileMap[te]
	if !found {
		return envProfile{}
	}

	return profile
}

type envProfile struct {
	canMovePVCAcrossNodes   bool
	isLeaderElectionEnabled bool
	canRunAppArmor          bool
	usesNodeDiskSpace       bool
}

func (p envProfile) CanMovePVCAcrossNodes() bool   { return p.canMovePVCAcrossNodes }
func (p envProfile) IsLeaderElectionEnabled() bool { return p.isLeaderElectionEnabled }
func (p envProfile) CanRunAppArmor() bool          { return p.canRunAppArmor }
func (p envProfile) UsesNodeDiskSpace() bool       { return p.usesNodeDiskSpace }
