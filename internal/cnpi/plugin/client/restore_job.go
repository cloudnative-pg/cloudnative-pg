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

	restore "github.com/cloudnative-pg/cnpg-i/pkg/restore/job"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrNoPluginSupportsRestoreJobHooksCapability is raised when no plugin supports the restore job hooks capability
var ErrNoPluginSupportsRestoreJobHooksCapability = errors.New("no plugin supports the restore job hooks capability")

type gvkEnsurer interface {
	EnsureGVKIsPresent()
	client.Object
}

func (data *data) Restore(
	ctx context.Context,
	cluster gvkEnsurer,
) (*restore.RestoreResponse, error) {
	r, err := data.innerRestore(ctx, cluster)
	return r, wrapAsPluginErrorIfNeeded(err)
}

func (data *data) innerRestore(
	ctx context.Context,
	cluster gvkEnsurer,
) (*restore.RestoreResponse, error) {
	cluster.EnsureGVKIsPresent()

	for idx := range data.plugins {
		plugin := data.plugins[idx]

		if !slices.Contains(plugin.RestoreJobHooksCapabilities(), restore.RestoreJobHooksCapability_KIND_RESTORE) {
			continue
		}

		clusterDefinition, err := json.Marshal(cluster)
		if err != nil {
			return nil, err
		}
		request := restore.RestoreRequest{
			ClusterDefinition: clusterDefinition,
		}
		res, err := plugin.RestoreJobHooksClient().Restore(ctx, &request)
		if err != nil {
			return nil, err
		}
		return res, nil
	}

	return nil, ErrNoPluginSupportsRestoreJobHooksCapability
}
