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

var vendors = map[string]*TestEnvVendor{
	"aks":   &AKS,
	"eks":   &EKS,
	"gke":   &GKE,
	"local": &LOCAL,
}

// TestCloudVendor creates the environment for testing
func TestCloudVendor() (*TestEnvVendor, error) {
	vendorEnv, exists := os.LookupEnv(testVendorEnvVarName)
	if exists {
		if vendor, ok := vendors[vendorEnv]; ok {
			return vendor, nil
		}
		return nil, fmt.Errorf("unknow cloud vendor %s", vendorEnv)
	}
	// if the env variable doesn't exist, fall back to using the old of detecting
	// the current env and print a warning
	env, err := NewTestingEnvironment()
	if err != nil {
		return nil, err
	}
	isAKS, err := env.IsAKS()
	if err != nil {
		return nil, err
	}
	if isAKS {
		return &AKS, nil
	}

	isGKE, err := env.IsGKE()
	if err != nil {
		return nil, err
	}
	if isGKE {
		return &GKE, nil
	}

	isEKS, err := env.IsEKS()
	if err != nil {
		return nil, err
	}
	if isEKS {
		return &EKS, nil
	}
	// if none above, it is a local
	return &LOCAL, nil
}
