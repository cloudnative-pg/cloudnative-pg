/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package postgres

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1alpha1 "github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/utils"
)

// Backup start a backup for this instance using
// barman-cloud-backup
func (instance *Instance) Backup(
	ctx context.Context,
	client client.StatusClient,
	configuration apiv1alpha1.BackupConfiguration,
	backup apiv1alpha1.BackupCommon,
	log logr.Logger,
) error {
	var options []string
	if configuration.Data != nil {
		if len(configuration.Data.Compression) != 0 {
			options = append(
				options,
				fmt.Sprintf("--%v", configuration.Data.Compression))
		}
		if len(configuration.Data.Encryption) != 0 {
			options = append(
				options,
				"-e",
				string(configuration.Data.Encryption))
		}
	}
	if len(configuration.EndpointURL) > 0 {
		options = append(
			options,
			"--endpoint-url",
			configuration.EndpointURL)
	}
	serverName := instance.ClusterName
	if len(configuration.ServerName) != 0 {
		serverName = configuration.ServerName
	}
	options = append(
		options,
		configuration.DestinationPath,
		serverName)

	// Mark the backup as running
	backup.GetStatus().Phase = apiv1alpha1.BackupPhaseRunning
	backup.GetStatus().StartedAt = &metav1.Time{
		Time: time.Now(),
	}

	if err := utils.UpdateStatusAndRetry(ctx, client, backup.GetKubernetesObject()); err != nil {
		return fmt.Errorf("can't set backup as running: %v", err)
	}

	// Run the actual backup process
	go func() {
		log.Info("Backup started",
			"options",
			options)

		cmd := exec.Command("barman-cloud-backup", options...) // #nosec G204
		var stdoutBuffer bytes.Buffer
		var stderrBuffer bytes.Buffer
		cmd.Stdout = &stdoutBuffer
		cmd.Stderr = &stderrBuffer
		err := cmd.Run()

		log.Info("Backup completed", "err", err)

		if err != nil {
			backup.GetStatus().SetAsFailed(stdoutBuffer.String(), stderrBuffer.String(), err)
		} else {
			backup.GetStatus().SetAsCompleted(stdoutBuffer.String(), stderrBuffer.String())
		}
		backup.GetStatus().StoppedAt = &metav1.Time{
			Time: time.Now(),
		}

		if err := utils.UpdateStatusAndRetry(ctx, client, backup.GetKubernetesObject()); err != nil {
			log.Error(err,
				"Can't mark backup as done",
				"stdout", stdoutBuffer.String(),
				"stderr", stderrBuffer.String())
		}
	}()

	return nil
}
