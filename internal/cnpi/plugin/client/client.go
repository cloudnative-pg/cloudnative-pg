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

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/stringset"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/connection"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
)

// data represent a new CNPI client collection
type data struct {
	repository repository.Interface
	plugins    []connection.Interface
}

func (data *data) getPlugin(pluginName string) (connection.Interface, error) {
	for idx := range data.plugins {
		plugin := data.plugins[idx]
		if plugin.Name() == pluginName {
			return plugin, nil
		}
	}

	return nil, ErrPluginNotLoaded
}

func (data *data) MetadataList() []connection.Metadata {
	result := make([]connection.Metadata, len(data.plugins))
	for i := range data.plugins {
		result[i] = data.plugins[i].Metadata()
	}

	return result
}

func (data *data) HasPlugin(pluginName string) bool {
	_, err := data.getPlugin(pluginName)
	return err == nil
}

func (data *data) Close(ctx context.Context) {
	contextLogger := log.FromContext(ctx)
	for i := range data.plugins {
		plugin := data.plugins[i]
		pluginLogger := contextLogger.WithValues("pluginName", plugin.Name())

		if err := plugin.Close(); err != nil {
			pluginLogger.Error(err, "while closing plugin connection")
		}
	}

	data.plugins = nil
}

// WithPlugins creates a new CNPG-I client for plugins in a certain repository,
// loading the plugins with the specified name
func WithPlugins(ctx context.Context, repository repository.Interface, names ...string) (Client, error) {
	result := &data{
		repository: repository,
	}

	load := func(names ...string) error {
		for _, name := range names {
			pluginData, err := result.repository.GetConnection(ctx, name)
			if err != nil {
				return err
			}

			result.plugins = append(result.plugins, pluginData)
		}
		return nil
	}

	// The following ensures that each plugin is loaded just one
	// time, even when the same plugin has been requested multiple
	// times.
	loadingPlugins := stringset.From(names)
	uniqueSortedPluginName := loadingPlugins.ToSortedList()

	if err := load(uniqueSortedPluginName...); err != nil {
		result.Close(ctx)
		return nil, err
	}

	return result, nil
}
