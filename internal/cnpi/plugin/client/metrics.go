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

package client

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/cloudnative-pg/cnpg-i/pkg/metrics"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MetricsCapabilities defines the interface for plugins that can provide metrics capabilities.
type MetricsCapabilities interface {
	// GetMetricsDefinitions retrieves the definitions of the metrics that will be collected from the plugins.
	GetMetricsDefinitions(ctx context.Context, cluster client.Object) (PluginMetricDefinitions, error)
	// CollectMetrics collects the metrics from the plugins.
	CollectMetrics(ctx context.Context, cluster client.Object) ([]*metrics.CollectMetric, error)
}

// PluginMetricDefinitions is a slice of PluginMetricDefinition, representing the metrics definitions returned
// by plugins.
type PluginMetricDefinitions []PluginMetricDefinition

// Get returns the PluginMetricDefinition with the given fully qualified name (FqName), returns nil if not found.
func (p PluginMetricDefinitions) Get(fqName string) *PluginMetricDefinition {
	for _, metric := range p {
		if metric.FqName == fqName {
			return &metric
		}
	}

	return nil
}

// PluginMetricDefinition represents a metric definition returned by a plugin.
type PluginMetricDefinition struct {
	FqName    string
	ValueType prometheus.ValueType
	Desc      *prometheus.Desc
}

func (data *data) GetMetricsDefinitions(
	ctx context.Context,
	cluster client.Object,
) (PluginMetricDefinitions, error) {
	contextLogger := log.FromContext(ctx).WithName("plugin_metrics_definitions")

	clusterDefinition, marshalErr := json.Marshal(cluster)
	if marshalErr != nil {
		return nil, marshalErr
	}

	var results PluginMetricDefinitions

	for idx := range data.plugins {
		plugin := data.plugins[idx]
		if !slices.Contains(plugin.MetricsCapabilities(), metrics.MetricsCapability_RPC_TYPE_METRICS) {
			contextLogger.Debug("skipping plugin", "plugin", plugin.Name())
			continue
		}

		res, err := plugin.MetricsClient().Define(ctx, &metrics.DefineMetricsRequest{ClusterDefinition: clusterDefinition})
		if err != nil {
			contextLogger.Error(err, "failed to get metrics definitions from plugin", "plugin", plugin.Name())
			return nil, err
		}
		if res == nil {
			err := errors.New("plugin returned nil metrics definitions while having metrics capability")
			contextLogger.Error(err, "while invoking metrics definitions", "plugin", plugin.Name())
			return nil, err
		}

		contextLogger.Debug("plugin returned metrics definitions", "plugin", plugin.Name(), "metrics", res.Metrics)
		for _, element := range res.Metrics {
			desc := prometheus.NewDesc(element.FqName, element.Help, element.VariableLabels, element.ConstLabels)
			results = append(results, PluginMetricDefinition{
				FqName:    element.FqName,
				Desc:      desc,
				ValueType: prometheus.ValueType(element.ValueType.Type),
			})
		}
	}

	return results, nil
}

func (data *data) CollectMetrics(
	ctx context.Context,
	cluster client.Object,
) ([]*metrics.CollectMetric, error) {
	contextLogger := log.FromContext(ctx).WithName("plugin_metrics_collect")

	clusterDefinition, marshalErr := json.Marshal(cluster)
	if marshalErr != nil {
		return nil, marshalErr
	}

	var results []*metrics.CollectMetric

	for idx := range data.plugins {
		plugin := data.plugins[idx]
		if !slices.Contains(plugin.MetricsCapabilities(), metrics.MetricsCapability_RPC_TYPE_METRICS) {
			contextLogger.Debug("skipping plugin", "plugin", plugin.Name())
			continue
		}

		res, err := plugin.MetricsClient().Collect(ctx, &metrics.CollectMetricsRequest{ClusterDefinition: clusterDefinition})
		if err != nil {
			contextLogger.Error(err, "failed to collect metrics from plugin", "plugin", plugin.Name())
			return nil, err
		}
		if res == nil {
			err := errors.New("plugin returned nil metrics while having metrics capability")
			contextLogger.Error(err, "while invoking metrics collection", "plugin", plugin.Name())
			return nil, err
		}

		contextLogger.Debug("plugin returned metrics", "plugin", plugin.Name(), "metrics", res.Metrics)
		results = append(results, res.Metrics...)
	}

	return results, nil
}
