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
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/strings/slices"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// offCommand represent the `hibernate off` command
type offCommand struct {
	ctx         context.Context
	clusterName string
}

// newOffCommand creates a new `hibernate off` command
func newOffCommand(ctx context.Context, clusterName string) *offCommand {
	contextLogger := log.FromContext(ctx).WithValues(
		"clusterName", clusterName)

	return &offCommand{
		ctx:         log.IntoContext(ctx, contextLogger),
		clusterName: clusterName,
	}
}

// execute executes the `hibernate off` command
func (off *offCommand) execute() error {
	off.printAdvancement("cluster reactivation starting")

	// Ensuring the cluster doesn't exist
	if err := off.ensureClusterDoesNotExistStep(); err != nil {
		return err
	}

	// Get the list of PVC from which we need to resume this cluster
	pvcGroup, err := getHibernatedPVCGroup(off.ctx, off.clusterName)
	if err != nil {
		return err
	}

	// Ensure the list of PVCs we have is correct
	if err := off.ensurePVCsArePartOfAPVCGroupStep(pvcGroup); err != nil {
		return err
	}

	// We recreate the cluster resource from the first PVC of the group,
	// and don't care of which PVC we select because we annotate
	// each PVC of a group with the same data.
	pvc := pvcGroup[0]

	// We get the original cluster resource from the annotation
	clusterFromPVC, err := getClusterFromPVCAnnotation(pvc)
	if err != nil {
		return err
	}

	// And recreate it into the Kubernetes cluster
	if err := off.createClusterWithoutRuntimeDataStep(clusterFromPVC); err != nil {
		return err
	}

	off.printAdvancement("cluster reactivation completed")

	return nil
}

// ensureClusterDoesNotExistStep checks if this cluster exist or not, ensuring
// that it is not present
func (off *offCommand) ensureClusterDoesNotExistStep() error {
	var cluster apiv1.Cluster
	err := plugin.Client.Get(
		off.ctx,
		types.NamespacedName{Name: off.clusterName, Namespace: plugin.Namespace},
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

// ensurePVCsArePartOfAPVCGroupStep check if the passed PVCs are really part of the same group
func (off *offCommand) ensurePVCsArePartOfAPVCGroupStep(pvcs []corev1.PersistentVolumeClaim) error {
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

// createClusterWithoutRuntimeDataStep recreate the original cluster back into Kubernetes
func (off *offCommand) createClusterWithoutRuntimeDataStep(clusterFromPVC apiv1.Cluster) error {
	cluster := clusterFromPVC.DeepCopy()
	// remove any runtime kubernetes metadata
	cluster.ObjectMeta.ResourceVersion = ""
	cluster.ObjectMeta.ManagedFields = nil
	cluster.ObjectMeta.UID = ""
	cluster.ObjectMeta.Generation = 0
	cluster.ObjectMeta.CreationTimestamp = metav1.Time{}
	// remove cluster status
	cluster.Status = apiv1.ClusterStatus{}

	// remove any runtime kubernetes annotations
	delete(cluster.Annotations, corev1.LastAppliedConfigAnnotation)

	// remove the cluster fencing
	delete(cluster.Annotations, utils.FencedInstanceAnnotation)

	// create cluster
	return plugin.Client.Create(off.ctx, cluster)
}

// ensureAnnotationsExists returns an error if the passed PVC is annotated with all the
// passed annotations names
func ensureAnnotationsExists(volume corev1.PersistentVolumeClaim, annotationNames ...string) error {
	for _, annotationName := range annotationNames {
		if _, ok := volume.Annotations[annotationName]; !ok {
			return fmt.Errorf("missing %s annotation, from pvcs: %s", annotationName, volume.Name)
		}
	}

	return nil
}

func (off *offCommand) printAdvancement(msg string) {
	fmt.Println(msg)
}
