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

	if err = c.Create(ctx, pvc); err != nil && !apierrs.IsAlreadyExists(err) {
		return fmt.Errorf("unable to create a PVC: %s for this node (nodeSerial: %d): %w",
			pvc.Name,
			configuration.NodeSerial,
			err,
		)
	}

	return nil
}
