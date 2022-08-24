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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Destroy implements the destroy subcommand
func Destroy(ctx context.Context, clusterName, instanceID string, keepPVC bool) error {
	// Check if the Pod exist
	var pod v1.Pod
	instanceName := fmt.Sprintf("%s-%s", clusterName, instanceID)

	err := plugin.Client.Get(ctx, client.ObjectKey{
		Namespace: plugin.Namespace,
		Name:      instanceName,
	}, &pod)

	var volumes []v1.Volume
	if err != nil {
		if keepPVC {
			return fmt.Errorf("instance %s not found in namespace %s", instanceName, plugin.Namespace)
		}
		return deletePVCsMatchingLabel(ctx, instanceName)
	}
	volumes = pod.Spec.Volumes

	// for the pod to be deleted it must either be owned by the cluster
	if isOwnedByCluster(clusterName, pod.OwnerReferences) {
		// Delete the Pod
		err = plugin.Client.Delete(ctx, &pod)
		if err != nil {
			return fmt.Errorf("error deleting instance %s: %v", instanceName, err)
		}
	} else {
		return fmt.Errorf("instance %s is not owned by cluster %s", instanceName, clusterName)
	}

	if keepPVC {
		// get the pvc and remove the owner reference
		pvcs, err := getPVCSOwnedByInstance(ctx, volumes, clusterName)
		if err != nil {
			return err
		}
		for i := range pvcs.Items {
			pvcs.Items[i].OwnerReferences = removeOwnerReference(pvcs.Items[i].OwnerReferences, clusterName)
			pvcs.Items[i].Annotations["cnpg.io/pvcStatus"] = "detached"
			pvcs.Items[i].Labels[utils.InstanceLabelName] = instanceName
			err = plugin.Client.Update(ctx, &pvcs.Items[i])
			if err != nil {
				return fmt.Errorf("error updating metadata for persistent volume claim %s: %v",
					clusterName, err)
			}
		}
		return nil
	}
	// we delete every volume attached to the pod that is owned by the cluster
	pvcs, err := getPVCSOwnedByInstance(ctx, volumes, clusterName)
	if err != nil {
		return err
	}
	for i := range pvcs.Items {
		err = plugin.Client.Delete(ctx, &pvcs.Items[i])
		if err != nil {
			return fmt.Errorf("error deleting pvc %s: %v",
				volumes[i].PersistentVolumeClaim.ClaimName, err)
		}
	}

	return nil
}

func deletePVCsMatchingLabel(ctx context.Context, instanceName string) error {
	var pvcs v1.PersistentVolumeClaimList
	err := plugin.Client.List(ctx, &pvcs, client.InNamespace(plugin.Namespace),
		client.MatchingLabels{utils.InstanceLabelName: instanceName})
	if err != nil {
		return fmt.Errorf("error getting pvcs for instance %s: %v", instanceName, err)
	}
	for i := range pvcs.Items {
		err = plugin.Client.Delete(ctx, &pvcs.Items[i])
		if err != nil {
			fmt.Printf("error deleting pvc %s: %v", pvcs.Items[i].Name, err)
		}
	}
	return nil
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

// getPVCSOwnedByInstance returns a list of pvcs that are owned by the instance
func getPVCSOwnedByInstance(ctx context.Context, volumes []v1.Volume, clusterName string) (v1.PersistentVolumeClaimList,
	error,
) {
	var pvcs v1.PersistentVolumeClaimList
	for i := range volumes {
		if volumes[i].PersistentVolumeClaim == nil {
			continue
		}

		var pvc v1.PersistentVolumeClaim
		err := plugin.Client.Get(ctx, client.ObjectKey{
			Namespace: plugin.Namespace,
			Name:      volumes[i].PersistentVolumeClaim.ClaimName,
		}, &pvc)
		if err != nil {
			return v1.PersistentVolumeClaimList{},
				fmt.Errorf("error getting pvc %s: %v", volumes[i].PersistentVolumeClaim.ClaimName, err)
		}

		if isOwnedByCluster(clusterName, pvc.OwnerReferences) {
			pvcs.Items = append(pvcs.Items, pvc)
		}
	}
	return pvcs, nil
}

// isOwnedByCluster returns true if the owner reference is owned by the cluster
func isOwnedByCluster(clusterName string, ownerReferences []metav1.OwnerReference) bool {
	for _, ownerReference := range ownerReferences {
		if ownerReference.Name == clusterName {
			return true
		}
	}
	return false
}
