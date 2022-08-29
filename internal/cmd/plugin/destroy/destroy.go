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

// Package destroy implements a command to destroy an instances of a cluster and its associated PVC
package destroy

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Destroy implements the destroy subcommand
func Destroy(ctx context.Context, clusterName, instanceID string, keepPVC bool) error {
	instanceName := fmt.Sprintf("%s-%s", clusterName, instanceID)

	if err := ensurePodIsDeleted(ctx, instanceName, clusterName); err != nil {
		return fmt.Errorf("error deleting instance %s: %v", instanceName, err)
	}

	pvcs, err := getExpectedPVCs(ctx, clusterName, instanceName)
	if err != nil {
		return err
	}

	if keepPVC {
		// we remove the ownership from the pvcs if present
		for _, pvc := range pvcs {
			pvc := pvc
			if !isOwnedByCluster(clusterName, pvc.OwnerReferences) {
				continue
			}

			pvc.OwnerReferences = removeOwnerReference(pvc.OwnerReferences, clusterName)
			pvc.Annotations["cnpg.io/pvcStatus"] = "detached"
			pvc.Labels[utils.InstanceNameLabelName] = instanceName
			err = plugin.Client.Update(ctx, &pvc)
			if err != nil {
				return fmt.Errorf("error updating metadata for persistent volume claim %s: %v",
					clusterName, err)
			}
		}
		return nil
	}

	for _, pvc := range pvcs {
		pvc := pvc
		if pvc.Labels == nil {
			pvc.Labels = map[string]string{}
		}
		if !isOwnedByCluster(clusterName, pvc.OwnerReferences) &&
			pvc.Labels[utils.InstanceNameLabelName] != instanceName {
			continue
		}

		err = plugin.Client.Delete(ctx, &pvc)
		if err != nil {
			return fmt.Errorf("error deleting pvc %s: %v", pvc.Name, err)
		}
	}

	return nil
}

func ensurePodIsDeleted(ctx context.Context, instanceName, clusterName string) error {
	// Check if the Pod exist
	var pod corev1.Pod
	err := plugin.Client.Get(ctx, client.ObjectKey{
		Namespace: plugin.Namespace,
		Name:      instanceName,
	}, &pod)
	if apierrs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if !isOwnedByCluster(clusterName, pod.OwnerReferences) {
		return fmt.Errorf("instance %s is not owned by cluster %s", pod.Name, clusterName)
	}

	return plugin.Client.Delete(ctx, &pod)
}

func getExpectedPVCs(
	ctx context.Context,
	clusterName string,
	instanceName string,
) ([]corev1.PersistentVolumeClaim, error) {
	var cluster apiv1.Cluster
	if err := plugin.Client.Get(
		ctx,
		types.NamespacedName{
			Name:      clusterName,
			Namespace: plugin.Namespace,
		},
		&cluster,
	); err != nil {
		return nil, err
	}

	var pvcs []corev1.PersistentVolumeClaim

	pgDataName := specs.GetPVCName(cluster, instanceName, utils.PVCRolePgData)
	pgData, err := getPVC(ctx, pgDataName)
	if err != nil {
		return nil, err
	}
	if pgData != nil {
		pvcs = append(pvcs, *pgData)
	}

	pgWalName := specs.GetPVCName(cluster, instanceName, utils.PVCRolePgWal)
	pgWal, err := getPVC(ctx, pgWalName)
	if err != nil {
		return nil, err
	}
	if pgWal != nil {
		pvcs = append(pvcs, *pgWal)
	}

	return pvcs, nil
}

// getPVC returns the pvc if found or any error that isn't apierrs.IsNotFound
func getPVC(ctx context.Context, name string) (*corev1.PersistentVolumeClaim, error) {
	var pvc corev1.PersistentVolumeClaim
	err := plugin.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: plugin.Namespace}, &pvc)
	if apierrs.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &pvc, nil
}

func removeOwnerReference(references []metav1.OwnerReference, clusterName string) []metav1.OwnerReference {
	for i := range references {
		if references[i].Name == clusterName && references[i].Kind == "Cluster" {
			references = append(references[:i], references[i+1:]...)
			break
		}
	}
	return references
}

// isOwnedByCluster returns true if the owner reference is owned by the cluster
func isOwnedByCluster(clusterName string, ownerReferences []metav1.OwnerReference) bool {
	// TODO: there should be an existing function for this perhaps that we could reuse, remove hardcoded string
	for _, ownerReference := range ownerReferences {
		if ownerReference.Name == clusterName && ownerReference.APIVersion == "postgresql.cnpg.io/v1" {
			return true
		}
	}
	return false
}
