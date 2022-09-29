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

package conditions

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// Update will allow update a particular condition in cluster status.
func Update(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	condition *metav1.Condition,
) error {
	if cluster == nil || condition == nil {
		return nil
	}
	existingCluster := cluster.DeepCopy()
	meta.SetStatusCondition(&cluster.Status.Conditions, *condition)

	if !reflect.DeepEqual(existingCluster.Status.Conditions, cluster.Status.Conditions) {
		// To avoid conflict using patch instead of update
		if err := c.Status().Patch(ctx, cluster, client.MergeFrom(existingCluster)); err != nil {
			return err
		}
	}

	return nil
}
