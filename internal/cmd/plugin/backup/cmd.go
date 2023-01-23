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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// NewCmd creates the new "backup" subcommand
func NewCmd() *cobra.Command {
	var backupName string

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
					time.Now().Format("20060102150400"))
			}

			return createBackup(cmd.Context(), backupName, clusterName)
		},
	}

	backupSubcommand.Flags().StringVar(
		&backupName,
		"backup-name",
		"",
		"The name of the Backup resource that will be created, "+
			"defaults to \"[cluster]-[current_timestamp]\"",
	)

	return backupSubcommand
}

// createBackup handles the Backup resource creation
func createBackup(ctx context.Context, backupName, clusterName string) error {
	backup := apiv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: plugin.Namespace,
			Name:      backupName,
		},
		Spec: apiv1.BackupSpec{
			Cluster: apiv1.LocalObjectReference{
				Name: clusterName,
			},
		},
	}

	err := plugin.Client.Create(ctx, &backup)
	if err == nil {
		fmt.Printf("backup/%v created\n", backup.Name)
	}
	return err
}
