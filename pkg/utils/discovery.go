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
	"regexp"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
)

// This variable stores the result of the DetectSecurityContextConstraints check
var haveSCC bool

// This variable specifies whether we should set the SeccompProfile or not in the pods
var supportSeccomp bool

// minorVersionRegexp is used to extract the minor version from
// the Kubernetes API server version. Some providers, like AWS,
// append a "+" to the Kubernetes minor version indicated that
// there's some maintenance backport patch over the standard
// release beyond its end-of-life.
var minorVersionRegexp = regexp.MustCompile(`^([0-9]+)\+?$`)

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

// HaveSeccompSupport returns true if Seccomp is supported. If it is, we should
// set the SeccompProfile in the pods
func HaveSeccompSupport() bool {
	return supportSeccomp
}

// extractK8sMinorVersion extracts and parse the Kubernetes minor version from
// the version info as detected by discovery client
func extractK8sMinorVersion(info *version.Info) (int, error) {
	matches := minorVersionRegexp.FindStringSubmatch(info.Minor)
	if matches == nil {
		// we couldn't detect the minor version of Kubernetes
		return 0, fmt.Errorf("invalid Kubernetes minor version: %s", info.Minor)
	}

	return strconv.Atoi(matches[1])
}

// DetectSeccompSupport checks the version of Kubernetes in the cluster to determine
// whether Seccomp is supported
func DetectSeccompSupport(client *discovery.DiscoveryClient) (err error) {
	supportSeccomp = false
	kubernetesVersion, err := client.ServerVersion()
	if err != nil {
		return err
	}

	minor, err := extractK8sMinorVersion(kubernetesVersion)
	if err != nil {
		return err
	}

	if minor >= 24 {
		supportSeccomp = true
	}

	return
}
