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

package status

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// PatchConditionsWithOptimisticLock will update a particular condition in cluster status.
// This function may update the conditions in the passed cluster
// with the latest ones that were found from the API server.
// This function is needed because Kubernetes still doesn't support strategic merge
// for CRDs (see https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/).
func PatchConditionsWithOptimisticLock(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	conditions ...metav1.Condition,
) error {
	if cluster == nil || len(conditions) == 0 {
		return nil
	}

	updatedCluster, err := PatchObjectWithOptimisticLock(ctx, c, cluster,
		func(cluster *apiv1.Cluster) {
			for _, condition := range conditions {
				meta.SetStatusCondition(&cluster.Status.Conditions, condition)
			}
		})
	if err != nil {
		return fmt.Errorf("while patching conditions: %w", err)
	}
	if updatedCluster != nil {
		cluster.Status.Conditions = updatedCluster.Status.Conditions
	}

	return nil
}
