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

package utils

import (
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/executablehash"
)

// haveSCC stores the result of the DetectSecurityContextConstraints check
var haveSCC bool

// haveVolumeSnapshot stores the result of the VolumeSnapshotExist function
var haveVolumeSnapshot bool

// olmPlatform specifies whether we are running on a platform with OLM support
var olmPlatform bool

// AvailableArchitecture is a struct containing info about an available architecture
type AvailableArchitecture struct {
	GoArch         string
	hash           string
	mx             sync.Mutex
	hashCalculator func(name string) (hash string, err error)
	binaryPath     string
}

func newAvailableArchitecture(goArch, binaryPath string) *AvailableArchitecture {
	return &AvailableArchitecture{
		GoArch:         goArch,
		hashCalculator: executablehash.GetByName,
		binaryPath:     binaryPath,
	}
}

// GetHash retrieves the hash for a given AvailableArchitecture
func (arch *AvailableArchitecture) GetHash() string {
	return arch.calculateHash()
}

// calculateHash calculates the hash for a given AvailableArchitecture
func (arch *AvailableArchitecture) calculateHash() string {
	arch.mx.Lock()
	defer arch.mx.Unlock()

	if arch.hash != "" {
		return arch.hash
	}

	hash, err := arch.hashCalculator(arch.binaryPath)
	if err != nil {
		panic(fmt.Errorf("while calculating architecture hash: %w", err))
	}

	arch.hash = hash
	return hash
}

// FileStream opens a stream reading from the manager's binary
func (arch *AvailableArchitecture) FileStream() (io.ReadCloser, error) {
	return executablehash.StreamByName(arch.binaryPath)
}

// availableArchitectures stores the result of DetectAvailableArchitectures function
var availableArchitectures []*AvailableArchitecture

// minorVersionRegexp is used to extract the minor version from
// the Kubernetes API server version. Some providers, like AWS,
// append a "+" to the Kubernetes minor version to presumably
// indicate that some maintenance patches have been back-ported
// beyond the standard end-of-life of the release.
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

func resourceExist(client discovery.DiscoveryInterface, groupVersion, kind string) (bool, error) {
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
func DetectSecurityContextConstraints(client discovery.DiscoveryInterface) (err error) {
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

// DetectVolumeSnapshotExist connects to the discovery API and find out if
// the VolumeSnapshot CRD exist in the cluster
func DetectVolumeSnapshotExist(client discovery.DiscoveryInterface) (err error) {
	haveVolumeSnapshot, err = resourceExist(client, "snapshot.storage.k8s.io/v1", "volumesnapshots")
	if err != nil {
		return err
	}

	return nil
}

// SetVolumeSnapshot set the haveVolumeSnapshot variable to a specific value for testing purposes
// IMPORTANT: use it only in the unit tests
func SetVolumeSnapshot(value bool) {
	haveVolumeSnapshot = value
}

// HaveVolumeSnapshot returns true if we're running under a system that implements
// having the VolumeSnapshot CRD
func HaveVolumeSnapshot() bool {
	return haveVolumeSnapshot
}

// PodMonitorExist tries to find the PodMonitor resource in the current cluster
func PodMonitorExist(client discovery.DiscoveryInterface) (bool, error) {
	exist, err := resourceExist(client, "monitoring.coreos.com/v1", "podmonitors")
	if err != nil {
		return false, err
	}

	return exist, nil
}

// extractK8sMinorVersion extracts and parses the Kubernetes minor version from
// the version info that's been  detected by discovery client
func extractK8sMinorVersion(info *version.Info) (int, error) {
	matches := minorVersionRegexp.FindStringSubmatch(info.Minor)
	if matches == nil {
		// we couldn't detect the minor version of Kubernetes
		return 0, fmt.Errorf("invalid Kubernetes minor version: %s", info.Minor)
	}

	return strconv.Atoi(matches[1])
}

// GetAvailableArchitectures returns the available instance's architectures
func GetAvailableArchitectures() []*AvailableArchitecture { return availableArchitectures }

// GetAvailableArchitecture returns an available architecture given its goArch
func GetAvailableArchitecture(goArch string) (*AvailableArchitecture, error) {
	for _, a := range availableArchitectures {
		if a.GoArch == goArch {
			return a, nil
		}
	}
	return nil, fmt.Errorf("invalid architecture: %s", goArch)
}

// detectAvailableArchitectures detects the architectures available in a given path
func detectAvailableArchitectures(filepathGlob string) error {
	binaries, err := filepath.Glob(filepathGlob)
	if err != nil {
		return err
	}
	for _, b := range binaries {
		goArch := strings.Split(filepath.Base(b), "manager_")[1]
		arch := newAvailableArchitecture(goArch, b)
		availableArchitectures = append(availableArchitectures, arch)
		go arch.calculateHash()
	}

	return err
}

// DetectAvailableArchitectures detects the architectures available in the cluster
func DetectAvailableArchitectures() error {
	return detectAvailableArchitectures("operator/manager_*")
}

// DetectOLM looks for the operators.coreos.com operators resource in the current
// Kubernetes cluster
func DetectOLM(client discovery.DiscoveryInterface) (err error) {
	olmPlatform = false
	olmPlatform, err = resourceExist(client, "operators.coreos.com/v1", "operators")
	return err
}

// RunningOnOLM returns if we're running over a Kubernetes cluster with OLM support
func RunningOnOLM() bool {
	return olmPlatform
}
