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

package client

import (
	"context"
	"errors"
	"slices"

	restore "github.com/cloudnative-pg/cnpg-i/pkg/restore/job"
)

// ErrNoPluginSupportsRestoreJobHooksCapability is raised when no plugin supports the restore job hooks capability
var ErrNoPluginSupportsRestoreJobHooksCapability = errors.New("no plugin supports the restore job hooks capability")

func (data *data) Restore(ctx context.Context) (*restore.RestoreResponse, error) {
	for idx := range data.plugins {
		plugin := data.plugins[idx]

		if !slices.Contains(plugin.RestoreJobHooksCapabilities(), restore.RestoreJobHooksCapability_KIND_RESTORE) {
			continue
		}

		request := restore.RestoreRequest{}
		res, err := plugin.RestoreJobHooksClient().Restore(ctx, &request)
		if err != nil {
			return nil, err
		}
		return res, nil
	}

	return nil, ErrNoPluginSupportsRestoreJobHooksCapability
}
