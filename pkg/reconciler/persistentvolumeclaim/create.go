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

package persistentvolumeclaim

import (
	"context"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

func createIfNotExists(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	configuration *CreateConfiguration,
) error {
	contextLogger := log.FromContext(ctx)

	pvc, err := Build(cluster, configuration)
	if err != nil {
		if err == ErrorInvalidSize {
			// This error should have been caught by the validating
			// webhook, but since we are here the user must have disabled server-side
			// validation, and we must react.
			contextLogger.Info("The size specified for the cluster is not valid",
				"size",
				configuration.Storage.Size)
			return utils.ErrNextLoop
		}
		return fmt.Errorf(
			"unable to create a PVC spec for node with serial %v: %w",
			configuration.NodeSerial,
			err,
		)
	}

	err = c.Create(ctx, pvc)
	if err == nil {
		return nil
	}
	if !apierrs.IsAlreadyExists(err) {
		return fmt.Errorf("unable to create a PVC: %s for this node (nodeSerial: %d): %w",
			pvc.Name,
			configuration.NodeSerial,
			err,
		)
	}

	// The PVC already exists, but it may still be terminating: a previous
	// instance's PVC that has not finished being garbage-collected yet. Treating
	// that as success would bind the instance to a volume about to vanish,
	// leaving its Pod unschedulable (see #10985). Wait for it to be removed
	// instead of swallowing the error.
	return waitForTerminatingPVC(ctx, c, pvc, configuration.NodeSerial)
}

// waitForTerminatingPVC inspects an already-existing PVC and returns
// utils.ErrNextLoop when it is still terminating (or has just been removed), so
// the caller retries once the name is free again. It returns nil when the PVC
// exists and is not terminating.
func waitForTerminatingPVC(
	ctx context.Context,
	c client.Client,
	pvc *corev1.PersistentVolumeClaim,
	nodeSerial int,
) error {
	contextLogger := log.FromContext(ctx)

	var existing corev1.PersistentVolumeClaim
	if err := c.Get(ctx, client.ObjectKeyFromObject(pvc), &existing); err != nil {
		if apierrs.IsNotFound(err) {
			// It finished terminating between Create and Get: retry next loop.
			return utils.ErrNextLoop
		}
		return fmt.Errorf("unable to verify existing PVC %s (nodeSerial: %d): %w", pvc.Name, nodeSerial, err)
	}

	if existing.DeletionTimestamp != nil {
		contextLogger.Info(
			"Waiting for the previous PVC to be removed before recreating it",
			"pvc", pvc.Name,
			"nodeSerial", nodeSerial,
		)
		return utils.ErrNextLoop
	}

	return nil
}
