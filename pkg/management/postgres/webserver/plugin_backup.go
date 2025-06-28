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

package webserver

import (
	"context"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	pluginClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/status"
)

// PluginBackupCommand represent a backup command that is being executed
type PluginBackupCommand struct {
	Cluster  *apiv1.Cluster
	Backup   *apiv1.Backup
	Client   client.Client
	Recorder record.EventRecorder
}

// NewPluginBackupCommand initializes a BackupCommand object, taking a physical
// backup using Barman Cloud
func NewPluginBackupCommand(
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	client client.Client,
	recorder record.EventRecorder,
) *PluginBackupCommand {
	backup.EnsureGVKIsPresent()

	return &PluginBackupCommand{
		Cluster:  cluster,
		Backup:   backup,
		Client:   client,
		Recorder: recorder,
	}
}

// Start starts a backup using the Plugin
func (b *PluginBackupCommand) Start(ctx context.Context) {
	go b.invokeStart(ctx)
}

func (b *PluginBackupCommand) invokeStart(ctx context.Context) {
	contextLogger := log.FromContext(ctx).WithValues(
		"pluginConfiguration", b.Backup.Spec.PluginConfiguration,
		"backupName", b.Backup.Name,
		"backupNamespace", b.Backup.Name)

	plugins := repository.New()
	defer plugins.Close()

	enabledPluginNamesSet := stringset.New()
	enabledPluginNamesSet.Put(b.Backup.Spec.PluginConfiguration.Name)
	cli, err := pluginClient.NewClient(
		ctx,
		enabledPluginNamesSet,
	)
	if err != nil {
		b.markBackupAsFailed(ctx, err)
		return
	}
	defer cli.Close(ctx)

	if !cli.HasPlugin(b.Backup.Spec.PluginConfiguration.Name) {
		b.markBackupAsFailed(
			ctx,
			fmt.Errorf("requested plugin is not available: %s", b.Backup.Spec.PluginConfiguration.Name),
		)
		return
	}

	// record the backup beginning
	contextLogger.Info("Plugin backup started")
	b.Recorder.Event(b.Backup, "Normal", "Starting", "Backup started")

	// Update backup status in cluster conditions on startup
	if err := b.retryWithRefreshedCluster(ctx, func() error {
		return status.PatchConditionsWithOptimisticLock(ctx, b.Client, b.Cluster, apiv1.BackupStartingCondition)
	}); err != nil {
		contextLogger.Error(err, "Error changing backup condition (backup started)")
		// We do not terminate here because we could still have a good backup
		// even if we are unable to communicate with the Kubernetes API server
	}

	response, err := cli.Backup(
		ctx,
		b.Cluster,
		b.Backup,
		b.Backup.Spec.PluginConfiguration.Name,
		b.Backup.Spec.PluginConfiguration.Parameters)
	if err != nil {
		b.markBackupAsFailed(ctx, err)
		return
	}

	contextLogger.Info("Backup completed")
	b.Recorder.Event(b.Backup, "Normal", "Completed", "Backup completed")

	// Set the status to completed
	b.Backup.Status.SetAsCompleted()

	// Fill the backup status from the plugin
	// Note: the InstanceID field is set by the operator backup controller
	b.Backup.Status.BackupID = response.BackupID
	b.Backup.Status.BackupName = response.BackupName
	b.Backup.Status.BeginWal = response.BeginWal
	b.Backup.Status.EndWal = response.EndWal
	b.Backup.Status.BeginLSN = response.BeginLsn
	b.Backup.Status.EndLSN = response.EndLsn
	b.Backup.Status.BackupLabelFile = response.BackupLabelFile
	b.Backup.Status.TablespaceMapFile = response.TablespaceMapFile
	b.Backup.Status.Online = ptr.To(response.Online)
	b.Backup.Status.PluginMetadata = response.Metadata

	if !response.StartedAt.IsZero() {
		b.Backup.Status.StartedAt = ptr.To(metav1.NewTime(response.StartedAt))
	}
	if !response.StoppedAt.IsZero() {
		b.Backup.Status.StoppedAt = ptr.To(metav1.NewTime(response.StoppedAt))
	}

	if err := postgres.PatchBackupStatusAndRetry(ctx, b.Client, b.Backup); err != nil {
		contextLogger.Error(err, "Can't set backup status as completed")
	}

	// Update backup status in cluster conditions on backup completion
	if err := b.retryWithRefreshedCluster(ctx, func() error {
		return status.PatchConditionsWithOptimisticLock(ctx, b.Client, b.Cluster, apiv1.BackupSucceededCondition)
	}); err != nil {
		contextLogger.Error(err, "Can't update the cluster with the completed backup data")
	}
}

func (b *PluginBackupCommand) markBackupAsFailed(ctx context.Context, failure error) {
	contextLogger := log.FromContext(ctx)

	// record the failure
	contextLogger.Error(failure, "Backup failed")
	b.Recorder.Event(b.Backup, "Normal", "Failed", "Backup failed")

	_ = status.FlagBackupAsFailed(ctx, b.Client, b.Backup, b.Cluster, failure)
}

func (b *PluginBackupCommand) retryWithRefreshedCluster(
	ctx context.Context,
	cb func() error,
) error {
	return resources.RetryWithRefreshedResource(ctx, b.Client, b.Cluster, cb)
}
