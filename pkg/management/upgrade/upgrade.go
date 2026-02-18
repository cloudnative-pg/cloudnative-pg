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

// Package upgrade manages the in-place upgrade of the instance manager
package upgrade

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// UploadFolder is the folder where the new version of the instance manager
// will be uploaded
const UploadFolder = "/controller"

// InstanceManagerPath is the location of the instance manager executable
const InstanceManagerPath = "/controller/manager"

// ErrorInvalidInstanceManagerBinary is raised when upgrading to an instance manager
// which has not the correct hash
var ErrorInvalidInstanceManagerBinary = errors.New("invalid instance manager binary")

// FromReader updates in place the binary of the instance manager, replacing itself
// with a new version with the new binary
func FromReader(
	cancelFunc context.CancelFunc,
	exitedCondition concurrency.MultipleExecuted,
	typedClient client.Client,
	instance *postgres.Instance,
	r io.Reader,
) error {
	// Create a temporary file to host the new instance manager binary
	updatedInstanceManager, err := os.CreateTemp(UploadFolder, "manager_*.new")
	if err != nil {
		return fmt.Errorf(
			"while creating a temporary file to host the new version of the instance manager: %w", err)
	}
	defer func() {
		// This code is executed only if the instance manager has not been updated, and
		// this is the only condition we have a temporary file to remove
		removeError := os.Remove(updatedInstanceManager.Name()) //nolint:gosec // path from our own temp file
		if removeError != nil {
			log.Warning("Error while removing temporary instance manager upload file",
				"name", updatedInstanceManager.Name(), "err", err)
		}
	}()

	// Read the new instance manager version
	newHash, err := downloadAndCloseInstanceManagerBinary(updatedInstanceManager, r)
	if err != nil {
		return fmt.Errorf("while reading new instance manager binary: %w", err)
	}

	// Validate the hash of this instance manager
	if err := validateInstanceManagerHash(typedClient,
		instance.GetClusterName(), instance.GetNamespaceName(),
		instance.GetArchitecture(), newHash); err != nil {
		return fmt.Errorf("while validating instance manager binary: %w", err)
	}

	log.Info("Received new version of the instance manager",
		"hashCode", newHash,
		"temporaryName", updatedInstanceManager.Name())

	// Grant the executable bit to the new file
	err = os.Chmod(updatedInstanceManager.Name(), 0o755) // #nosec
	if err != nil {
		return fmt.Errorf("while granting the executable bit to the instance manager binary: %w", err)
	}

	// Replace the new instance manager with the new one
	//nolint:gosec // path from our own temp file
	if err := os.Rename(updatedInstanceManager.Name(), InstanceManagerPath); err != nil {
		return fmt.Errorf("while replacing instance manager binary: %w", err)
	}

	// We are ready to reload the instance manager
	// First we are going to cancel the context, this will trigger the shutdown of all
	// the Runnables handled by the manager.
	// Only the postgres process will not be actually killed, as the postgres lifecycle
	// manager will not kill it if InstanceManagerIsUpgrading is set to true.
	cancelFunc()
	log.Info("Waiting for log goroutines to exit before proceeding")

	// We have to wait for all the necessary component to exit gracefully first
	exitedCondition.Wait()
	log.Info("All log goroutines exited, will reload the instance manager")

	// Now we are actually ready to reload the instance manager
	err = reloadInstanceManager()
	if err != nil {
		return fmt.Errorf("while replacing instance manager process: %w", err)
	}

	return nil
}

// downloadAndCloseInstanceManagerBinary updates the binary of the new version of
// the instance manager, returning the new hash when it's done
func downloadAndCloseInstanceManagerBinary(dst *os.File, src io.Reader) (string, error) {
	var err error

	// Get the binary stream from the request and store is inside our temporary folder
	defer func() {
		errClose := dst.Close()
		if err == nil {
			err = errClose
		}
	}()

	// we calculate the instanceManager hash while copying the contents from the reader
	encoder := sha256.New()
	_, err = io.Copy(dst, io.TeeReader(src, encoder))
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", encoder.Sum(nil)), err
}

// validateInstanceManagerHash compares the new version of the instance manager
// within the passed hash code with the one listed in the cluster status.
// It returns any errors during execution, or nil if the hash is fine
func validateInstanceManagerHash(
	typedClient client.Client,
	clusterName,
	namespace,
	instanceArch,
	newHashCode string,
) error {
	var cluster apiv1.Cluster

	ctx := context.Background()
	if err := typedClient.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      clusterName,
	}, &cluster); err != nil {
		return err
	}

	availableArch := cluster.Status.GetAvailableArchitecture(instanceArch)
	if availableArch == nil {
		return fmt.Errorf("missing architecture %s", instanceArch)
	}

	if availableArch.Hash != newHashCode {
		log.Warning("Received invalid version of the instance manager",
			"hashCode", newHashCode)
		return ErrorInvalidInstanceManagerBinary
	}

	return nil
}

// reloadInstanceManager gracefully stops the log collection process and then
// replace this process with a new one executing the new binary.
// This function never returns in case of success.
func reloadInstanceManager() error {
	log.Info("Replacing current instance")
	err := syscall.Exec(InstanceManagerPath, os.Args, os.Environ()) //nolint:gosec // execing our own binary with os.Args
	if err != nil {
		return err
	}

	// This point is not really reachable as this process as been replaced
	// with the new one
	return nil
}
