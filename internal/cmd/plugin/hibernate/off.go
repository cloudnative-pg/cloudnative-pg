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
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// errNoHibernatedPVCsFound indicates that no PVCs were found. This is also used by the status command
var errNoHibernatedPVCsFound = fmt.Errorf("no hibernated PVCs to reactivate found")

func hibernateOff(ctx context.Context, clusterName string) error {
	fmt.Println("cluster reactivation starting")
	if err := ensureClusterDoesNotExist(ctx, clusterName); err != nil {
		return err
	}

	pvcGroup, err := getHibernatedPVCGroup(ctx, clusterName)
	if err != nil {
		return err
	}

	if err := recreateClusterFromHibernatedPVC(ctx, pvcGroup[0]); err != nil {
		return err
	}

	fmt.Println("cluster reactivation completed")
	return nil
}

func recreateClusterFromHibernatedPVC(ctx context.Context, pvc corev1.PersistentVolumeClaim) error {
	clusterFromPVC, err := getClusterFromPVCAnnotation(pvc)
	if err != nil {
		return err
	}

	return createClusterWithoutRuntimeData(ctx, clusterFromPVC)
}

func getHibernatedPVCGroup(ctx context.Context, clusterName string) ([]corev1.PersistentVolumeClaim, error) {
	pvcs, err := getClusterPVCs(ctx, clusterName)
	if err != nil {
		return nil, err
	}
	if len(pvcs) == 0 {
		return nil, errNoHibernatedPVCsFound
	}
	if err := ensurePVCsArePartOfAPVCGroup(pvcs); err != nil {
		return nil, err
	}

	return pvcs, nil
}

func getClusterPVCs(ctx context.Context, clusterName string) ([]corev1.PersistentVolumeClaim, error) {
	var pvcList corev1.PersistentVolumeClaimList
	if err := plugin.Client.List(
		ctx,
		&pvcList,
		client.MatchingLabels{utils.ClusterLabelName: clusterName},
	); err != nil {
		return nil, err
	}

	return pvcList.Items, nil
}

func createClusterWithoutRuntimeData(ctx context.Context, clusterFromPVC v1.Cluster) error {
	cluster := clusterFromPVC.DeepCopy()
	// remove any runtime kubernetes metadata
	cluster.ObjectMeta.ResourceVersion = ""
	cluster.ObjectMeta.ManagedFields = nil
	cluster.ObjectMeta.UID = ""
	cluster.ObjectMeta.Generation = 0
	cluster.ObjectMeta.CreationTimestamp = metav1.Time{}
	// remove cluster status
	cluster.Status = v1.ClusterStatus{}

	// remove any runtime kubernetes annotations
	delete(cluster.Annotations, corev1.LastAppliedConfigAnnotation)

	// remove the cluster fencing
	delete(cluster.Annotations, utils.FencedInstanceAnnotation)

	// create cluster
	return plugin.Client.Create(ctx, cluster)

}

func getClusterFromPVCAnnotation(pvc corev1.PersistentVolumeClaim) (v1.Cluster, error) {
	var clusterFromPVC v1.Cluster
	// get the cluster manifest
	clusterJSON := pvc.Annotations[utils.HibernateClusterManifestAnnotationName]
	if err := json.Unmarshal([]byte(clusterJSON), &clusterFromPVC); err != nil {
		return v1.Cluster{}, err
	}
	return clusterFromPVC, nil
}

func ensurePVCsArePartOfAPVCGroup(pvcs []corev1.PersistentVolumeClaim) error {
	// ensure all the pvcs belong to the same node serial and are hibernated
	var nodeSerial []string
	for _, pvc := range pvcs {
		if err := ensureAnnotationsExists(
			pvc,
			utils.HibernateClusterManifestAnnotationName,
			utils.HibernatePgControlDataAnnotationName,
			specs.ClusterSerialAnnotationName,
		); err != nil {
			return err
		}

		serial := pvc.Annotations[specs.ClusterSerialAnnotationName]
		if !slices.Contains(nodeSerial, serial) {
			nodeSerial = append(nodeSerial, serial)
		}
	}
	if len(nodeSerial) != 1 {
		return fmt.Errorf("hibernate pvcs belong to different instances of the cluster, cannot proceed")
	}

	return nil
}

func ensureClusterDoesNotExist(ctx context.Context, clusterName string) error {
	var cluster v1.Cluster
	err := plugin.Client.Get(
		ctx,
		types.NamespacedName{Name: clusterName, Namespace: plugin.Namespace},
		&cluster,
	)
	if err == nil {
		return fmt.Errorf("cluster already exist, cannot proceed with reactivation")
	}
	if !apierrs.IsNotFound(err) {
		return err
	}
	return nil
}

func ensureAnnotationsExists(volume corev1.PersistentVolumeClaim, annotationNames ...string) error {
	for _, annotationName := range annotationNames {
		if _, ok := volume.Annotations[annotationName]; !ok {
			return fmt.Errorf("missing %s annotation, from pvcs: %s", annotationName, volume.Name)
		}
	}

	return nil
}
