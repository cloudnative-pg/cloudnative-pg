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
)

// AllClusterPodsHaveLabels verifies if the labels defined in a map are included
// in all the pods of a cluster
func AllClusterPodsHaveLabels(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
	labels map[string]string,
) (bool, error) {
	cluster, err := GetCluster(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return false, err
	}
	podList, err := GetClusterPodList(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return false, err
	}
	if len(podList.Items) != cluster.Spec.Instances {
		return false, fmt.Errorf("%v found instances, %v expected", len(podList.Items), cluster.Spec.Instances)
	}
	for _, pod := range podList.Items {
		if !PodHasLabels(pod, labels) {
			return false, fmt.Errorf("%v found labels, expected %v", pod.Labels, labels)
		}
	}
	return true, nil
}

// AllClusterPodsHaveAnnotations verifies if the annotations defined in a map are included
// in all the pods of a cluster
func AllClusterPodsHaveAnnotations(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
	annotations map[string]string,
) (bool, error) {
	cluster, err := GetCluster(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return false, err
	}
	podList, err := GetClusterPodList(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return false, err
	}
	if len(podList.Items) != cluster.Spec.Instances {
		return false, fmt.Errorf("%v found instances, %v expected", len(podList.Items), cluster.Spec.Instances)
	}
	for _, pod := range podList.Items {
		if !PodHasAnnotations(pod, annotations) {
			return false, fmt.Errorf("%v found annotations, %v expected", pod.Annotations, annotations)
		}
	}
	return true, nil
}

// ClusterHasLabels verifies that the labels of a cluster contain a specified
// labels map
func ClusterHasLabels(
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

// ClusterHasAnnotations verifies that the annotations of a cluster contain a specified
// annotations map
func ClusterHasAnnotations(
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

// GetCluster gets a cluster given name and namespace
func GetCluster(
	ctx context.Context,
	crudClient client.Client,
	namespace, name string,
) (*apiv1.Cluster, error) {
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	cluster := &apiv1.Cluster{}
	err := objects.GetObject(ctx, crudClient, namespacedName, cluster)
	if err != nil {
		return nil, err
	}
	return cluster, nil
}

// GetClusterPodList gathers the current list of instance pods for a cluster in a namespace
func GetClusterPodList(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	err := objects.GetObjectList(ctx, crudClient, podList, client.InNamespace(namespace),
		client.MatchingLabels{
			utils.ClusterLabelName: clusterName,
			utils.PodRoleLabelName: "instance", // this ensures we are getting instance pods only
		},
	)
	return podList, err
}

// GetClusterPrimary gets the primary pod of a cluster
func GetClusterPrimary(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
) (*corev1.Pod, error) {
	podList := &corev1.PodList{}

	err := objects.GetObjectList(ctx, crudClient, podList, client.InNamespace(namespace),
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

// GetClusterReplicas gets a slice containing all the replica pods of a cluster
func GetClusterReplicas(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	err := objects.GetObjectList(ctx, crudClient, podList, client.InNamespace(namespace),
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

// ScaleClusterSize scales a cluster to the requested size
func ScaleClusterSize(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
	newClusterSize int,
) error {
	cluster, err := GetCluster(ctx, crudClient, namespace, clusterName)
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

// PodHasLabels verifies that the labels of a pod contain a specified
// labels map
func PodHasLabels(pod corev1.Pod, labels map[string]string) bool {
	podLabels := pod.Labels
	for k, v := range labels {
		val, ok := podLabels[k]
		if !ok || (v != val) {
			return false
		}
	}
	return true
}

// PodHasAnnotations verifies that the annotations of a pod contain a specified
// annotations map
func PodHasAnnotations(pod corev1.Pod, annotations map[string]string) bool {
	podAnnotations := pod.Annotations
	for k, v := range annotations {
		val, ok := podAnnotations[k]
		if !ok || (v != val) {
			return false
		}
	}
	return true
}

// PodHasCondition verifies that a pod has a specified condition
func PodHasCondition(pod *corev1.Pod, conditionType corev1.PodConditionType, status corev1.ConditionStatus) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == conditionType && cond.Status == status {
			return true
		}
	}
	return false
}
