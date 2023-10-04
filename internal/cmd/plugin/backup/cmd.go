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

// Package backup implements a command to request an on-demand backup
// for a PostgreSQL cluster
package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/exp/slices"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// backupCommandOptions are the options that are provider to the backup
// cnpg command
type backupCommandOptions struct {
	backupName  string
	clusterName string
	target      apiv1.BackupTarget
	method      apiv1.BackupMethod
}

// NewCmd creates the new "backup" subcommand
func NewCmd() *cobra.Command {
	var backupName string
	var backupTarget string
	var backupMethod string
	var cluster apiv1.Cluster

	backupSubcommand := &cobra.Command{
		Use:   "backup [cluster]",
		Short: "Request an on-demand backup for a PostgreSQL Cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]

			if len(backupName) == 0 {
				backupName = fmt.Sprintf(
					"%s-%s",
					clusterName,
					utils.ToCompactISO8601(time.Now()),
				)
			}

			// Check if the backup target is correct
			allowedBackupTargets := []string{
				"",
				string(apiv1.BackupTargetPrimary),
				string(apiv1.BackupTargetStandby),
			}
			if !slices.Contains(allowedBackupTargets, backupTarget) {
				return fmt.Errorf("backup-target: %s is not supported by the backup command", backupTarget)
			}

			// Check if the backup method is correct
			allowedBackupMethods := []string{
				"",
				string(apiv1.BackupMethodBarmanObjectStore),
				string(apiv1.BackupMethodVolumeSnapshot),
			}
			if !slices.Contains(allowedBackupMethods, backupMethod) {
				return fmt.Errorf("backup-method: %s is not supported by the backup command", backupMethod)
			}

			// check if the cluster exists
			err := plugin.Client.Get(
				cmd.Context(),
				client.ObjectKey{
					Namespace: plugin.Namespace,
					Name:      clusterName,
				},
				&cluster,
			)
			if err != nil {
				return fmt.Errorf("Cluster %s does not exist", clusterName)
			}

			return createBackup(
				cmd.Context(),
				backupCommandOptions{
					backupName:  backupName,
					clusterName: clusterName,
					target:      apiv1.BackupTarget(backupTarget),
					method:      apiv1.BackupMethod(backupMethod),
				})
		},
	}

	backupSubcommand.Flags().StringVar(
		&backupName,
		"backup-name",
		"",
		"The name of the Backup resource that will be created, "+
			"defaults to \"[cluster]-[current_timestamp]\"",
	)
	backupSubcommand.Flags().StringVarP(
		&backupTarget,
		"backup-target",
		"t",
		"",
		"If present, will override the backup target defined in cluster, "+
			"valid values are primary and prefer-standby.",
	)
	backupSubcommand.Flags().StringVarP(
		&backupMethod,
		"method",
		"m",
		"",
		"If present, will override the backup method defined in backup resource, "+
			"valid values are volumeSnapshot and barmanObjectStore.",
	)

	return backupSubcommand
}

// createBackup handles the Backup resource creation
func createBackup(ctx context.Context, options backupCommandOptions) error {
	backup := apiv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: plugin.Namespace,
			Name:      options.backupName,
		},
		Spec: apiv1.BackupSpec{
			Cluster: apiv1.LocalObjectReference{
				Name: options.clusterName,
			},
			Target: options.target,
			Method: options.method,
		},
	}

	err := plugin.Client.Create(ctx, &backup)
	if err == nil {
		fmt.Printf("backup/%v created\n", backup.Name)
	}
	return err
}
