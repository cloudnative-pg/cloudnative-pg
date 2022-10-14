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
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// CreatePodMonitor create a new podmonitor for cluster
func CreatePodMonitor(cluster *apiv1.Cluster) *monitoringv1.PodMonitor {
	meta := metav1.ObjectMeta{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}
	utils.LabelClusterName(&meta, cluster.Name)

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
