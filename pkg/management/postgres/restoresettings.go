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

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"

	barmanCredentials "github.com/cloudnative-pg/barman-cloud/pkg/credentials"
	"github.com/cloudnative-pg/machinery/pkg/envmap"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

type restoreSettings struct {
	envs   []string
	config string
}

type getRestoreSettingsExecutor func(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
) (*restoreSettings, error)

func getRestoreSettings(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
	executors ...getRestoreSettingsExecutor,
) (*restoreSettings, error) {
	for _, cb := range executors {
		settings, err := cb(ctx, cli, cluster)
		if err != nil {
			continue
		}
		if settings != nil {
			return settings, nil
		}
	}

	return nil, fmt.Errorf("no restore settings found")
}

func (info *InitInfo) getRestoreSettingsFromInTreeSnapshot(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
) (*restoreSettings, error) {
	backup, server, err := info.createBackupObjectForSnapshotRestore(ctx, cluster)
	if err != nil {
		return nil, err
	}

	envs, err := barmanCredentials.EnvSetRestoreCloudCredentials(
		ctx,
		cli,
		cluster.Namespace,
		server.BarmanObjectStore,
		os.Environ())
	if err != nil {
		return nil, fmt.Errorf("error while setting the environment: %w", err)
	}

	config, err := getRestoreWalConfig(ctx, backup)
	if err != nil {
		return nil, err
	}

	settings := &restoreSettings{
		envs:   envs,
		config: config,
	}
	return settings, nil
}

func (info *InitInfo) getRestoreSettingsFromInTreeBarman(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
) (*restoreSettings, error) {
	if err := info.checkBackupDestination(ctx, cli, cluster); err != nil {
		return nil, err
	}

	backup, envs, err := info.loadBackup(ctx, cli, cluster)
	if err != nil {
		return nil, err
	}

	if err := info.ensureArchiveContainsLastCheckpointRedoWAL(ctx, cluster, envs, backup); err != nil {
		return nil, err
	}

	if err := info.restoreDataDir(ctx, backup, envs); err != nil {
		return nil, err
	}

	if _, err := info.restoreCustomWalDir(ctx); err != nil {
		return nil, err
	}

	config, err := getRestoreWalConfig(ctx, backup)
	if err != nil {
		return nil, err
	}

	settings := &restoreSettings{
		envs:   envs,
		config: config,
	}
	return settings, nil
}

func getRestoreSettingsFromPlugin(
	ctx context.Context,
	_ client.Client,
	cluster *apiv1.Cluster,
) (*restoreSettings, error) {
	contextLogger := log.FromContext(ctx)

	pluginConfiguration := cluster.GetRecoverySourcePlugin()
	if pluginConfiguration == nil {
		return nil, nil
	}

	contextLogger.Info("Restore through plugin detected, proceeding...")
	res, err := restoreViaPlugin(ctx, cluster, pluginConfiguration)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, errors.New("empty response from restoreViaPlugin, programmatic error")
	}

	processEnvironment, err := envmap.ParseEnviron()
	if err != nil {
		return nil, fmt.Errorf("error while parsing the process environment: %w", err)
	}

	pluginEnvironment, err := envmap.Parse(res.Envs)
	if err != nil {
		return nil, fmt.Errorf("error while parsing the plugin environment: %w", err)
	}

	settings := &restoreSettings{
		envs:   envmap.Merge(processEnvironment, pluginEnvironment).StringSlice(),
		config: res.RestoreConfig,
	}
	return settings, nil
}
