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
)

// EnsureInstancePVCGroupIsDeleted ensures that all the expected pvc for a given instance are deleted
func EnsureInstancePVCGroupIsDeleted(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	name string,
	namespace string,
) error {
	contextLogger := log.FromContext(ctx)

	pvcs, err := GetInstancePVCs(ctx, c, name, namespace)
	if err != nil {
		return fmt.Errorf("getting instance PVCs to delete for %s: %w", name, err)
	}

	for i := range pvcs {
		pvc := &pvcs[i]
		contextLogger.Info("Deleting PVC", "pvc", pvc.Name)

		if err := c.Delete(ctx, pvc); err != nil {
			// Ignore if NotFound, otherwise report the error
			if !apierrs.IsNotFound(err) {
				return fmt.Errorf("deleting pvc %s for instance %s: %w", pvc.Name, name, err)
			}
		}
	}

	return nil
}
