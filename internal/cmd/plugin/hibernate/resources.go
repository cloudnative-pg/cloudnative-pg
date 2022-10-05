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

package hibernate

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// errNoHibernatedPVCsFound indicates that no PVCs were found.
var errNoHibernatedPVCsFound = fmt.Errorf("no hibernated PVCs to reactivate found")

// getHibernatedPVCGroupStep gets the PVC group resulting from the hibernation process
func getHibernatedPVCGroup(ctx context.Context, clusterName string) ([]corev1.PersistentVolumeClaim, error) {
	// Get the list of PVCs belonging to this group
	var pvcList corev1.PersistentVolumeClaimList
	if err := plugin.Client.List(
		ctx,
		&pvcList,
		client.MatchingLabels{utils.ClusterLabelName: clusterName},
	); err != nil {
		return nil, err
	}
	if len(pvcList.Items) == 0 {
		return nil, errNoHibernatedPVCsFound
	}

	return pvcList.Items, nil
}
