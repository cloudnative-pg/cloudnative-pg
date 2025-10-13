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

package pgbouncer

import (
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// PoolerPodMonitorManager builds the PodMonitor for the pooler resource
type PoolerPodMonitorManager struct {
	pooler *apiv1.Pooler
}

// NewPoolerPodMonitorManager returns a new instance of PoolerPodMonitorManager
func NewPoolerPodMonitorManager(pooler *apiv1.Pooler) *PoolerPodMonitorManager {
	return &PoolerPodMonitorManager{pooler: pooler}
}

// IsPodMonitorEnabled returns a boolean indicating if the PodMonitor should exists or not
func (c PoolerPodMonitorManager) IsPodMonitorEnabled() bool {
	//nolint:staticcheck
	return c.pooler.Spec.Monitoring != nil && c.pooler.Spec.Monitoring.EnablePodMonitor
}

// BuildPodMonitor builds a new PodMonitor object
func (c PoolerPodMonitorManager) BuildPodMonitor() *monitoringv1.PodMonitor {
	meta := metav1.ObjectMeta{
		Namespace: c.pooler.Namespace,
		Name:      c.pooler.Name,
		Labels: map[string]string{
			utils.PgbouncerNameLabel: c.pooler.Name,
		},
	}

	utils.SetAsOwnedBy(&meta, c.pooler.ObjectMeta, c.pooler.TypeMeta)

	metricsPort := "metrics"
	endpoint := monitoringv1.PodMetricsEndpoint{
		Port: &metricsPort,
	}

	//nolint:staticcheck // Using deprecated fields during deprecation period
	if c.pooler.Spec.Monitoring != nil {
		endpoint.MetricRelabelConfigs = c.pooler.Spec.Monitoring.PodMonitorMetricRelabelConfigs
		endpoint.RelabelConfigs = c.pooler.Spec.Monitoring.PodMonitorRelabelConfigs
	}

	spec := monitoringv1.PodMonitorSpec{
		Selector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				utils.PgbouncerNameLabel: c.pooler.Name,
				utils.PodRoleLabelName:   string(utils.PodRolePooler),
			},
		},
		PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{endpoint},
	}
	return &monitoringv1.PodMonitor{
		ObjectMeta: meta,
		Spec:       spec,
	}
}
