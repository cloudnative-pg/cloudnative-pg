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

package v1

import (
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

// PodMonitoringConfiguration is a child map to generalize the management of PodMonitors
type PodMonitoringConfiguration struct {
	// The list of metric relabelings for the `PodMonitor`. Applied to samples before ingestion.
	// +optional
	PodMonitorMetricRelabelConfigs []*monitoringv1.RelabelConfig `json:"podMonitorMetricRelabelings,omitempty"`

	// The list of relabelings for the `PodMonitor`. Applied to samples before scraping.
	// +optional
	PodMonitorRelabelConfigs []*monitoringv1.RelabelConfig `json:"podMonitorRelabelings,omitempty"`

	// The scrape interval for the `PodMonitor`, if nil global Prometheus value will be used
	// +optional
	PodMonitorInterval *monitoringv1.Duration `json:"podMonitorInterval,omitempty"`

	// The scrape timeout for the `PodMonitor`, if nil global Prometheus value will be used
	// +optional
	PodMonitorScrapeTimeout *monitoringv1.Duration `json:"podMonitorScrapeTimeout,omitempty"`
}
