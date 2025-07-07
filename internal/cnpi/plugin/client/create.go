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

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
)

// NewClient creates a new CNPI client
func NewClient(ctx context.Context, enabledPlugin *stringset.Data) (Client, error) {
	cli, err := innerNewClient(ctx, enabledPlugin)
	return cli, wrapAsPluginErrorIfNeeded(err)
}

func innerNewClient(ctx context.Context, enabledPlugin *stringset.Data) (Client, error) {
	contextLogger := log.FromContext(ctx)
	plugins := repository.New()

	// TODO: make che socketDir a parameter
	availablePluginNames, err := plugins.RegisterUnixSocketPluginsInPath(configuration.Current.PluginSocketDir)
	if err != nil {
		contextLogger.Error(err, "Error while loading local plugins")
		plugins.Close()
		return nil, err
	}

	availablePluginNamesSet := stringset.From(availablePluginNames)
	availableAndEnabled := stringset.From(availablePluginNamesSet.Intersect(enabledPlugin).ToList())
	return WithPlugins(
		ctx,
		plugins,
		availableAndEnabled.ToList()...,
	)
}
