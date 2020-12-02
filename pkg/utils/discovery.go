package utils

import (
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"

	"strings"
)

var (
	// This variable store the latest result of the openshift check, and is useful to avoid repeating
	// the queries everytime
	openshift *bool
)

// IsOpenShift connects to the discovery API and find out if
// we're running under an OpenShift deployment
func IsOpenShift() (bool, error) {
	if openshift != nil {
		return *openshift, nil
	}

	config, err := ctrl.GetConfig()
	if err != nil {
		return false, err
	}

	client, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return false, err
	}
	apiGroupList, err := client.ServerGroups()
	if err != nil {
		return false, err
	}
	for i := 0; i < len(apiGroupList.Groups); i++ {
		if strings.HasSuffix(apiGroupList.Groups[i].Name, ".openshift.io") {
			openshift = new(bool)
			*openshift = true
			return *openshift, nil
		}
	}

	openshift = new(bool)
	*openshift = false
	return *openshift, nil
}
