/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"fmt"

	v1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

// AllClusterPodsHaveLabels verifies if the labels defined in a map are included
// in all the pods of a cluster
func AllClusterPodsHaveLabels(
	env *TestingEnvironment,
	namespace, clusterName string,
	labels map[string]string,
) (bool, error) {
	cluster, err := env.GetCluster(namespace, clusterName)
	if err != nil {
		return false, err
	}
	podList, err := env.GetClusterPodList(namespace, clusterName)
	if err != nil {
		return false, err
	}
	if len(podList.Items) != int(cluster.Spec.Instances) {
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
	env *TestingEnvironment,
	namespace, clusterName string,
	annotations map[string]string,
) (bool, error) {
	cluster, err := env.GetCluster(namespace, clusterName)
	if err != nil {
		return false, err
	}
	podList, err := env.GetClusterPodList(namespace, clusterName)
	if err != nil {
		return false, err
	}
	if len(podList.Items) != int(cluster.Spec.Instances) {
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
	cluster *v1.Cluster,
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
	cluster *v1.Cluster,
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
