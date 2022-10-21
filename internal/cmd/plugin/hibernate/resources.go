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
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
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
		client.InNamespace(plugin.Namespace),
	); err != nil {
		return nil, err
	}
	if len(pvcList.Items) == 0 {
		return nil, errNoHibernatedPVCsFound
	}

	return pvcList.Items, nil
}

// getClusterFromPVCAnnotation reads the original cluster resource from the chosen PVC
func getClusterFromPVCAnnotation(pvc corev1.PersistentVolumeClaim) (apiv1.Cluster, error) {
	var clusterFromPVC apiv1.Cluster
	// get the cluster manifest
	clusterJSON := pvc.Annotations[utils.HibernateClusterManifestAnnotationName]
	if err := json.Unmarshal([]byte(clusterJSON), &clusterFromPVC); err != nil {
		return apiv1.Cluster{}, err
	}
	return clusterFromPVC, nil
}
