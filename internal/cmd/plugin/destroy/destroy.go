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

// Package destroy implements a command to destroy an instances of a cluster and its associated PVC
package destroy

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/controller"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Destroy implements destroy subcommand
func Destroy(ctx context.Context, clusterName, instanceName string, keepPVC bool) error {
	if err := ensurePodIsDeleted(ctx, instanceName, clusterName); err != nil {
		return err
	}

	var jobList batchv1.JobList
	if err := plugin.Client.List(
		ctx,
		&jobList,
		client.InNamespace(plugin.Namespace),
		client.MatchingLabels{
			utils.InstanceNameLabelName: instanceName,
		},
	); err != nil {
		return err
	}
	for idx := range jobList.Items {
		if err := plugin.Client.Delete(
			ctx,
			&jobList.Items[idx],
			client.PropagationPolicy(metav1.DeletePropagationBackground),
		); err != nil && !apierrs.IsNotFound(err) {
			return fmt.Errorf("deleting job %s: %w", jobList.Items[idx].Name, err)
		}
	}

	pvcs, err := persistentvolumeclaim.GetInstancePVCs(ctx, plugin.Client, instanceName, plugin.Namespace)
	if err != nil {
		return err
	}

	if keepPVC {
		// we remove the ownership from the pvcs if present
		for i := range pvcs {
			if _, isOwned := controller.IsOwnedByCluster(&pvcs[i]); !isOwned {
				continue
			}

			pvcs[i].OwnerReferences = removeOwnerReference(pvcs[i].OwnerReferences, clusterName)
			pvcs[i].Annotations[utils.PVCStatusAnnotationName] = persistentvolumeclaim.StatusDetached
			pvcs[i].Labels[utils.InstanceNameLabelName] = instanceName
			err = plugin.Client.Update(ctx, &pvcs[i])
			if err != nil {
				return fmt.Errorf("error updating metadata for persistent volume claim %s: %v",
					clusterName, err)
			}
		}
		fmt.Printf("Instance %s of cluster %s has been destroyed and the PVC was kept\n",
			instanceName,
			clusterName,
		)
		return nil
	}

	for i := range pvcs {
		if pvcs[i].Labels == nil {
			pvcs[i].Labels = map[string]string{}
		}

		_, isOwned := controller.IsOwnedByCluster(&pvcs[i])
		// if it is requested for deletion and it is owned by the cluster, we delete it. If it is not owned by the cluster
		// but it does have the instance label and the detached annotation then we can still delete it
		// We will only skip the iteration and not delete the pvc if it is not owned by the cluster, and it does not have
		// the annotation or label
		if isOwned ||
			(pvcs[i].Annotations[utils.PVCStatusAnnotationName] == persistentvolumeclaim.StatusDetached &&
				pvcs[i].Labels[utils.InstanceNameLabelName] == instanceName) {
			if err = plugin.Client.Delete(ctx, &pvcs[i]); err != nil {
				return fmt.Errorf("error deleting pvc %s: %v", pvcs[i].Name, err)
			}
		}
	}

	fmt.Printf("Instance %s of cluster %s is destroyed\n", instanceName, clusterName)

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
		// The Pod doesn't exist, so we already did our job
		return nil
	}
	if err != nil {
		return err
	}

	if _, isOwned := controller.IsOwnedByCluster(&pod); !isOwned {
		return fmt.Errorf("instance %s is not owned by cluster %s", pod.Name, clusterName)
	}

	return plugin.Client.Delete(ctx, &pod)
}

// removeOwnerReference removes the owner reference to the cluster
func removeOwnerReference(references []metav1.OwnerReference, clusterName string) []metav1.OwnerReference {
	for i := range references {
		if references[i].Name == clusterName && references[i].Kind == "Cluster" {
			references = append(references[:i], references[i+1:]...)
			break
		}
	}
	return references
}
