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

package status

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// BackupTransaction is a function that modifies a Backup object.
type BackupTransaction func(*apiv1.Backup)

type flagBackupErrors struct {
	clusterStatusErr    error
	backupErr           error
	clusterConditionErr error
}

func (f flagBackupErrors) Error() string {
	var message string
	if f.clusterStatusErr != nil {
		message += fmt.Sprintf("error patching cluster status: %v; ", f.clusterStatusErr)
	}
	if f.backupErr != nil {
		message += fmt.Sprintf("error patching backup status: %v; ", f.backupErr)
	}
	if f.clusterConditionErr != nil {
		message += fmt.Sprintf("error patching cluster conditions: %v; ", f.clusterConditionErr)
	}

	return message
}

// toError returns the errors encountered or nil
func (f flagBackupErrors) toError() error {
	if f.clusterStatusErr != nil || f.backupErr != nil || f.clusterConditionErr != nil {
		return f
	}
	return nil
}

// FlagBackupAsFailed updates the status of a Backup object to indicate that it has failed.
func FlagBackupAsFailed(
	ctx context.Context,
	cli client.Client,
	backup *apiv1.Backup,
	cluster *apiv1.Cluster,
	err error,
	transactions ...BackupTransaction,
) error {
	contextLogger := log.FromContext(ctx)

	var flagErr flagBackupErrors

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var livingBackup apiv1.Backup
		if err := cli.Get(ctx, client.ObjectKeyFromObject(backup), &livingBackup); err != nil {
			contextLogger.Error(err, "failed to get backup")
			return err
		}
		origBackup := livingBackup.DeepCopy()
		livingBackup.Status.SetAsFailed(err)
		livingBackup.Status.Method = livingBackup.Spec.Method
		for _, transaction := range transactions {
			transaction(&livingBackup)
		}

		err := cli.Status().Patch(ctx, &livingBackup, client.MergeFrom(origBackup))
		if err != nil {
			contextLogger.Error(err, "while patching backup status")
			return err
		}
		// we mutate the original object
		backup.Status = livingBackup.Status

		return nil
	}); err != nil {
		contextLogger.Error(err, "while flagging backup as failed")
		flagErr.backupErr = err
	}

	if cluster == nil {
		return flagErr.toError()
	}

	if err := PatchWithOptimisticLock(
		ctx,
		cli,
		cluster,
		func(cluster *apiv1.Cluster) {
			cluster.Status.LastFailedBackup = pgTime.GetCurrentTimestampWithFormat(time.RFC3339)
		},
	); err != nil {
		contextLogger.Error(err, "while patching cluster status with last failed backup")
		flagErr.clusterStatusErr = err
	}

	if err := PatchConditionsWithOptimisticLock(
		ctx,
		cli,
		cluster,
		apiv1.BuildClusterBackupFailedCondition(err),
	); err != nil {
		contextLogger.Error(err, "while patching backup condition in the cluster status (backup failed)")
		flagErr.clusterConditionErr = err
	}

	return flagErr.toError()
}
