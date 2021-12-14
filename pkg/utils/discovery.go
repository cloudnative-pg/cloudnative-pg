/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
)

// This variable store the result of the DetectSecurityContextConstraints check
var haveSCC bool

// GetDiscoveryClient creates a discovery client or return error
func GetDiscoveryClient() (*discovery.DiscoveryClient, error) {
	config, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}

	return discoveryClient, nil
}

func resourceExist(client *discovery.DiscoveryClient, groupVersion, kind string) (bool, error) {
	apiResourceList, err := client.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	for _, resource := range apiResourceList.APIResources {
		if resource.Name == kind {
			return true, nil
		}
	}

	return false, nil
}

// DetectSecurityContextConstraints connects to the discovery API and find out if
// we're running under a system that implements OpenShift Security Context Constraints
func DetectSecurityContextConstraints(client *discovery.DiscoveryClient) (err error) {
	haveSCC, err = resourceExist(client, "security.openshift.io/v1", "securitycontextconstraints")
	if err != nil {
		return err
	}

	return nil
}

// HaveSecurityContextConstraints returns true if we're running under a system that implements
// OpenShift Security Context Constraints
// It panics if called before DetectSecurityContextConstraints
func HaveSecurityContextConstraints() bool {
	return haveSCC
}

// PodMonitorExist tries to find the PodMonitor resource in the current cluster
func PodMonitorExist(client *discovery.DiscoveryClient) (bool, error) {
	exist, err := resourceExist(client, "monitoring.coreos.com/v1", "podmonitors")
	if err != nil {
		return false, err
	}

	return exist, nil
}
