/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
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

	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/logpipe"
)

// InstanceManagerPath is the location of the instance manager executable
const InstanceManagerPath = "/controller/manager"

// InstanceManagerPathTemp is the temporary file used during the upgrade of the instance manager executable
const InstanceManagerPathTemp = InstanceManagerPath + ".new"

// ErrorInvalidInstanceManagerBinary is raised when upgrading to an instance manager
// which has not the correct hash
var ErrorInvalidInstanceManagerBinary = errors.New("invalid instance manager binary")

// FromReader updates in place the binary of the instance manager, replacing itself
// with a new version with the new binary
func FromReader(typedClient client.Client, instance *postgres.Instance, r io.Reader) error {
	// Mark this instance manager as upgrading
	instance.InstanceManagerIsUpgrading = true
	defer func() {
		// Something didn't work if we reach this point, as this process
		// should have already been replaced by a new one with the new binary
		instance.InstanceManagerIsUpgrading = false
	}()

	// Read the new instance manager version
	newHash, err := updateInstanceManagerBinary(r)
	if err != nil {
		return fmt.Errorf("while reading new instance manager binary: %w", err)
	}
	log.Info("Received new version of the instance manager", "hashCode", newHash)

	// Validate the hash of this instance manager
	var binaryValid bool
	binaryValid, err = validateInstanceManagerHash(typedClient, instance, newHash)
	if err != nil {
		return fmt.Errorf("while validating instance manager binary: %w", err)
	}
	if !binaryValid {
		return ErrorInvalidInstanceManagerBinary
	}

	// Replace the new instance manager with the new one
	if err := os.Rename(InstanceManagerPathTemp, InstanceManagerPath); err != nil {
		return fmt.Errorf("while replacing instance manager binary: %w", err)
	}

	// Reload the instance manager
	err = reloadInstanceManager()
	if err != nil {
		return fmt.Errorf("while replacing instance manager process: %w", err)
	}

	return nil
}

// updateInstanceManagerBinary updates the binary of the new version of
// the instance manager, returning the new hash when it's done
func updateInstanceManagerBinary(r io.Reader) (string, error) {
	var err error
	var newInstanceManager *os.File

	// Get the binary stream from the request and store is inside our temporary folder
	newInstanceManager, err = os.OpenFile(
		InstanceManagerPathTemp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o755) // #nosec
	if err != nil {
		return "", err
	}

	defer func() {
		errClose := newInstanceManager.Close()
		if err == nil {
			err = errClose
		}
	}()

	encoder := sha256.New()
	_, err = io.Copy(newInstanceManager, io.TeeReader(r, encoder))
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", encoder.Sum(nil)), err
}

// validateInstanceManagerHash compares the new version of the instance manager
// within the passed hash code with the one listed in the cluster status.
// It returns true if everything is fine, false otherwise
func validateInstanceManagerHash(
	typedClient client.Client, instance *postgres.Instance, hashCode string) (bool, error) {
	var cluster apiv1.Cluster

	ctx := context.Background()
	err := typedClient.Get(ctx, client.ObjectKey{
		Namespace: instance.Namespace,
		Name:      instance.ClusterName,
	}, &cluster)
	if err != nil {
		return false, err
	}

	return cluster.Status.OperatorHash == hashCode, nil
}

// reloadInstanceManager gracefully stops the log collection process and then
// replace this process with a new one executing the new binary.
// This function never return in case of success.
func reloadInstanceManager() error {
	log.Info("Stopping log processing")
	logpipe.Stop()

	log.Info("Replacing current instance")
	err := syscall.Exec(os.Args[0], os.Args, os.Environ()) // #nosec
	if err != nil {
		return err
	}

	// This point is not really reachable as this process as been replaced
	// with the new one
	return nil
}
