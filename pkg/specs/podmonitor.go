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

package specs

import (
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
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

	metricsPort := "metrics"
	endpoint := monitoringv1.PodMetricsEndpoint{
		Port: &metricsPort,
	}

	if c.cluster.IsMetricsTLSEnabled() {
		endpoint.Scheme = ptr.To(monitoringv1.SchemeHTTPS)
		endpoint.TLSConfig = &monitoringv1.SafeTLSConfig{
			CA: monitoringv1.SecretOrConfigMap{
				Secret: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: c.cluster.GetServerCASecretName(),
					},
					Key: certs.CACertKey,
				},
			},
			ServerName: ptr.To(c.cluster.GetServiceReadWriteName()),
			// InsecureSkipVerify needs to be set to match the ssl_mode=verify-ca
			// used by postgres when connecting to the other instances.
			InsecureSkipVerify: ptr.To(true),
		}
	}

	if c.cluster.Spec.Monitoring != nil {
		//nolint:staticcheck // Using deprecated fields during deprecation period
		endpoint.MetricRelabelConfigs = c.cluster.Spec.Monitoring.PodMonitorMetricRelabelConfigs
		//nolint:staticcheck // Using deprecated fields during deprecation period
		endpoint.RelabelConfigs = c.cluster.Spec.Monitoring.PodMonitorRelabelConfigs
	}

	spec := monitoringv1.PodMonitorSpec{
		Selector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				utils.ClusterLabelName: c.cluster.Name,
				utils.PodRoleLabelName: string(utils.PodRoleInstance),
			},
		},
		PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{endpoint},
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
