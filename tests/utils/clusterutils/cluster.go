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

// Package clusterutils provides functions to handle cluster actions
package clusterutils

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
)

// AllPodsHaveLabels verifies if the labels defined in a map are included
// in all the pods of a cluster
func AllPodsHaveLabels(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
	labels map[string]string,
) (bool, error) {
	cluster, err := Get(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return false, err
	}
	podList, err := ListPods(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return false, err
	}
	if len(podList.Items) != cluster.Spec.Instances {
		return false, fmt.Errorf("%v found instances, %v expected", len(podList.Items), cluster.Spec.Instances)
	}
	for _, pod := range podList.Items {
		if !pods.HasLabels(pod, labels) {
			return false, fmt.Errorf("%v found labels, expected %v", pod.Labels, labels)
		}
	}
	return true, nil
}

// AllPodsHaveAnnotations verifies if the annotations defined in a map are included
// in all the pods of a cluster
func AllPodsHaveAnnotations(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
	annotations map[string]string,
) (bool, error) {
	cluster, err := Get(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return false, err
	}
	podList, err := ListPods(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return false, err
	}
	if len(podList.Items) != cluster.Spec.Instances {
		return false, fmt.Errorf("%v found instances, %v expected", len(podList.Items), cluster.Spec.Instances)
	}
	for _, pod := range podList.Items {
		if !pods.HasAnnotations(pod, annotations) {
			return false, fmt.Errorf("%v found annotations, %v expected", pod.Annotations, annotations)
		}
	}
	return true, nil
}

// HasLabels verifies that the labels of a cluster contain a specified
// labels map
func HasLabels(
	cluster *apiv1.Cluster,
	labels map[string]string,
) bool {
	clusterLabels := cluster.Labels
	for k, v := range labels {
		val, ok := clusterLabels[k]
		if !ok || (v != val) {
			return false
		}
	}
	return true
}

// HasAnnotations verifies that the annotations of a cluster contain a specified
// annotations map
func HasAnnotations(
	cluster *apiv1.Cluster,
	annotations map[string]string,
) bool {
	clusterAnnotations := cluster.Annotations
	for k, v := range annotations {
		val, ok := clusterAnnotations[k]
		if !ok || (v != val) {
			return false
		}
	}
	return true
}

// Get gets a cluster given name and namespace
func Get(
	ctx context.Context,
	crudClient client.Client,
	namespace, name string,
) (*apiv1.Cluster, error) {
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	cluster := &apiv1.Cluster{}
	err := objects.Get(ctx, crudClient, namespacedName, cluster)
	if err != nil {
		return nil, err
	}
	return cluster, nil
}

// ListPods gathers the current list of instance pods for a cluster in a namespace
func ListPods(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	err := objects.List(ctx, crudClient, podList, client.InNamespace(namespace),
		client.MatchingLabels{
			utils.ClusterLabelName: clusterName,
			utils.PodRoleLabelName: "instance", // this ensures we are getting instance pods only
		},
	)
	return podList, err
}

// GetPrimary gets the primary pod of a cluster
func GetPrimary(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
) (*corev1.Pod, error) {
	podList := &corev1.PodList{}

	err := objects.List(ctx, crudClient, podList, client.InNamespace(namespace),
		client.MatchingLabels{
			utils.ClusterLabelName:             clusterName,
			utils.ClusterInstanceRoleLabelName: specs.ClusterRoleLabelPrimary,
		},
	)
	if err != nil {
		return &corev1.Pod{}, err
	}
	if len(podList.Items) > 0 {
		// if there are multiple, get the one without deletion timestamp
		for _, pod := range podList.Items {
			if pod.DeletionTimestamp == nil {
				return &pod, nil
			}
		}
		err = fmt.Errorf("all pod with primary role has deletion timestamp")
		return &(podList.Items[0]), err
	}
	err = fmt.Errorf("no primary found")
	return &corev1.Pod{}, err
}

// GetReplicas gets a slice containing all the replica pods of a cluster
func GetReplicas(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	err := objects.List(ctx, crudClient, podList, client.InNamespace(namespace),
		client.MatchingLabels{
			utils.ClusterLabelName:             clusterName,
			utils.ClusterInstanceRoleLabelName: specs.ClusterRoleLabelReplica,
		},
	)
	if err != nil {
		return podList, err
	}
	if len(podList.Items) > 0 {
		return podList, nil
	}
	err = fmt.Errorf("no replicas found")
	return podList, err
}

// GetFirstReplica gets the first replica pod from a cluster
func GetFirstReplica(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
) (*corev1.Pod, error) {
	podList, err := GetReplicas(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no replicas found")
	}
	return &podList.Items[0], nil
}

// ScaleSize scales a cluster to the requested size
func ScaleSize(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
	newClusterSize int,
) error {
	cluster, err := Get(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return err
	}
	originalCluster := cluster.DeepCopy()
	cluster.Spec.Instances = newClusterSize
	err = crudClient.Patch(ctx, cluster, client.MergeFrom(originalCluster))
	if err != nil {
		return err
	}
	return nil
}
