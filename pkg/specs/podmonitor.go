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

package specs

import (
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// ClusterPodMonitorManager builds the PodMonitor for the cluster resource
type ClusterPodMonitorManager struct {
	cluster *apiv1.Cluster
}

// IsPodMonitorEnabled returns a boolean indicating if the PodMonitor should exists or not
func (c ClusterPodMonitorManager) IsPodMonitorEnabled() bool {
	return c.cluster.IsPodMonitorEnabled()
}

// BuildPodMonitor builds a new PodMonitor object
func (c ClusterPodMonitorManager) BuildPodMonitor() *monitoringv1.PodMonitor {
	meta := metav1.ObjectMeta{
		Namespace: c.cluster.Namespace,
		Name:      c.cluster.Name,
	}
	c.cluster.SetInheritedDataAndOwnership(&meta)

	spec := monitoringv1.PodMonitorSpec{
		Selector: metav1.LabelSelector{
			MatchLabels: meta.Labels,
		},
		PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{
			{
				Port: "metrics",
			},
		},
	}

	return &monitoringv1.PodMonitor{
		ObjectMeta: meta,
		Spec:       spec,
	}
}

// NewClusterPodMonitorManager returns a new instance of ClusterPodMonitorManager
func NewClusterPodMonitorManager(cluster *apiv1.Cluster) *ClusterPodMonitorManager {
	return &ClusterPodMonitorManager{cluster: cluster}
}
