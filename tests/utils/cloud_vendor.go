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

// LOCAL kind or k3d cluster running locally
var LOCAL = TestEnvVendor("local")

// OCP openshift cloud cluster
var OCP = TestEnvVendor("ocp")

var vendors = map[string]*TestEnvVendor{
	"aks":   &AKS,
	"eks":   &EKS,
	"gke":   &GKE,
	"local": &LOCAL,
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

// GetEnvProfile returns a cloud environment's capablities profile
func GetEnvProfile(te TestEnvVendor) EnvProfile {
	profileMap := map[TestEnvVendor]EnvProfile{
		LOCAL: localProfile{},
		AKS:   aksProfile{},
		EKS:   eksProfile{},
		GKE:   gkeProfile{},
		OCP:   ocpProfile{},
	}

	profile, found := profileMap[te]
	if !found {
		return localProfile{}
	}

	return profile
}

type localProfile struct{}

func (e localProfile) CanMovePVCAcrossNodes() bool   { return false }
func (e localProfile) IsLeaderElectionEnabled() bool { return true }
func (e localProfile) CanRunAppArmor() bool          { return false }
func (e localProfile) UsesNodeDiskSpace() bool       { return true }

type aksProfile struct{}

func (e aksProfile) CanMovePVCAcrossNodes() bool   { return true }
func (e aksProfile) IsLeaderElectionEnabled() bool { return true }
func (e aksProfile) CanRunAppArmor() bool          { return true }
func (e aksProfile) UsesNodeDiskSpace() bool       { return false }

type eksProfile struct{}

func (e eksProfile) CanMovePVCAcrossNodes() bool   { return false }
func (e eksProfile) IsLeaderElectionEnabled() bool { return true }
func (e eksProfile) CanRunAppArmor() bool          { return false }
func (e eksProfile) UsesNodeDiskSpace() bool       { return false }

type gkeProfile struct{}

func (e gkeProfile) CanMovePVCAcrossNodes() bool   { return true }
func (e gkeProfile) IsLeaderElectionEnabled() bool { return false }
func (e gkeProfile) CanRunAppArmor() bool          { return false }
func (e gkeProfile) UsesNodeDiskSpace() bool       { return false }

type ocpProfile struct{}

func (e ocpProfile) CanMovePVCAcrossNodes() bool   { return false }
func (e ocpProfile) IsLeaderElectionEnabled() bool { return true }
func (e ocpProfile) CanRunAppArmor() bool          { return false }
func (e ocpProfile) UsesNodeDiskSpace() bool       { return false }
