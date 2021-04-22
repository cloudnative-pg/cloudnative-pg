/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	// This variable store the result of the DetectSecurityContextConstraints check
	haveSCC bool
)

// DetectSecurityContextConstraints connects to the discovery API and find out if
// we're running under a system that implements OpenShift Security Context Constraints
func DetectSecurityContextConstraints() error {
	config, err := ctrl.GetConfig()
	if err != nil {
		return err
	}

	client, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return err
	}

	groupFound := false

	apiGroupList, err := client.ServerGroups()
	if err != nil {
		return err
	}
	for i := 0; i < len(apiGroupList.Groups); i++ {
		if apiGroupList.Groups[i].Name == "security.openshift.io" {
			groupFound = true
			break
		}
	}

	if !groupFound {
		haveSCC = false
		return nil
	}

	apiResourceList, err := client.ServerResourcesForGroupVersion("security.openshift.io/v1")
	if err != nil {
		return err
	}

	for i := 0; i < len(apiResourceList.APIResources); i++ {
		if apiResourceList.APIResources[i].Name == "securitycontextconstraints" {
			haveSCC = true
			break
		}
	}

	return nil
}

// HaveSecurityContextConstraints returns true if we're running under a system that implements
// OpenShift Security Context Constraints
// It panics if called before DetectSecurityContextConstraints
func HaveSecurityContextConstraints() bool {
	return haveSCC
}
