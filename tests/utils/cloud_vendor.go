package utils

import (
	"fmt"
	"os"
)

// TestEnvVendor is the type for cloud vendor e2e test is running
type TestEnvVendor string

// testVendorEnvVarName is the environment variable set external for different cloud vendor (for k8s only)
const testVendorEnvVarName = "TEST_CLOUD_VENDOR"

// AKS azure cloud cluster
var AKS = TestEnvVendor("aks")

// EKS amazon elastic cloud cluster
var EKS = TestEnvVendor("eks")

// GKE google cloud cluster
var GKE = TestEnvVendor("gke")

// LOCAL kind or k3d cluster run on local
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
	// if the env variable is not existed, we backport to use the old way to detect but print a warning here
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
