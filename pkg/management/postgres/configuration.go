/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"

	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/configfile"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

// InstallPgDataFileContent installs a file in PgData, returning true/false if
// the file has been changed and an error state
func InstallPgDataFileContent(pgdata, contents, destinationFile string) (bool, error) {
	targetFile := path.Join(pgdata, destinationFile)
	result, err := fileutils.WriteStringToFile(targetFile, contents)
	if err != nil {
		return false, err
	}

	if result {
		log.Info(
			"Installed configuration file",
			"pgdata", pgdata,
			"filename", destinationFile)
	}

	return result, nil
}

// RefreshConfigurationFilesFromCluster receives a cluster object, then generates the
// PostgreSQL configuration and rewrites the file in the PGDATA if needed. This
// function will return "true" if the configuration has been really changed.
// Important: this will not send a SIGHUP to the server
func (instance *Instance) RefreshConfigurationFilesFromCluster(cluster *apiv1.Cluster) (bool, error) {
	postgresConfiguration, sha256, err := cluster.CreatePostgresqlConfiguration()
	if err != nil {
		return false, err
	}

	postgresHBA := cluster.CreatePostgresqlHBA()

	postgresConfigurationChanged, err := InstallPgDataFileContent(
		instance.PgData,
		postgresConfiguration,
		PostgresqlCustomConfigurationFile)
	if err != nil {
		return postgresConfigurationChanged, fmt.Errorf(
			"installing postgresql configuration: %w",
			err)
	}

	postgresHBAChanged, err := InstallPgDataFileContent(
		instance.PgData,
		postgresHBA,
		PostgresqlHBARulesFile)
	if err != nil {
		return postgresConfigurationChanged || postgresHBAChanged, fmt.Errorf(
			"installing postgresql HBA rules: %w",
			err)
	}

	if sha256 != "" && postgresConfigurationChanged {
		instance.ConfigSha256 = sha256
	}

	return postgresConfigurationChanged || postgresHBAChanged, nil
}

// RefreshConfigurationFiles gets the latest version of the ConfigMap from the API
// server and then write the configuration in PGDATA
func (instance *Instance) RefreshConfigurationFiles(ctx context.Context, client ctrl.Client) (bool, error) {
	var cluster apiv1.Cluster
	err := client.Get(ctx, ctrl.ObjectKey{Namespace: instance.Namespace, Name: instance.ClusterName}, &cluster)
	if err != nil {
		return false, err
	}

	return instance.RefreshConfigurationFilesFromCluster(&cluster)
}

// UpdateReplicaConfiguration updates the postgresql.auto.conf or recovery.conf file for the proper version
// of PostgreSQL
func UpdateReplicaConfiguration(pgData string, clusterName string, podName string) (changed bool, err error) {
	primaryConnInfo := buildPrimaryConnInfo(clusterName+"-rw", podName)
	return UpdateReplicaConfigurationForPrimary(pgData, primaryConnInfo)
}

// UpdateReplicaConfigurationForPrimary updates the postgresql.auto.conf or recovery.conf file for the proper version
// of PostgreSQL, using the specified connection string to connect to the primary server
func UpdateReplicaConfigurationForPrimary(pgData string, primaryConnInfo string) (changed bool, err error) {
	major, err := postgres.GetMajorVersion(pgData)
	if err != nil {
		return false, err
	}

	if major < 12 {
		return configureRecoveryConfFile(pgData, primaryConnInfo)
	}

	if err := createStandbySignal(pgData); err != nil {
		return false, err
	}

	return configurePostgresAutoConfFile(pgData, primaryConnInfo)
}

// configureRecoveryConfFile configures replication in the recovery.conf file
// for PostgreSQL 11 and earlier
func configureRecoveryConfFile(pgData string, primaryConnInfo string) (changed bool, err error) {
	targetFile := path.Join(pgData, "recovery.conf")

	options := map[string]string{
		"standby_mode":             "on",
		"restore_command":          "/controller/manager wal-restore %f %p",
		"recovery_target_timeline": "latest",
	}

	if primaryConnInfo != "" {
		options["primary_conninfo"] = primaryConnInfo
	}

	changed, err = configfile.UpdatePostgresConfigurationFile(targetFile, options)
	if err != nil {
		return false, err
	}
	if changed {
		log.Info("Updated replication settings in recovery.conf file")
	}

	return changed, nil
}

// configurePostgresAutoConfFile configures replication a in the postgresql.auto.conf file
// for PostgreSQL 12 and newer
func configurePostgresAutoConfFile(pgData string, primaryConnInfo string) (changed bool, err error) {
	targetFile := path.Join(pgData, "postgresql.auto.conf")

	options := map[string]string{
		"restore_command":          "/controller/manager wal-restore %f %p",
		"recovery_target_timeline": "latest",
	}

	if primaryConnInfo != "" {
		options["primary_conninfo"] = primaryConnInfo
	}

	changed, err = configfile.UpdatePostgresConfigurationFile(targetFile, options)
	if err != nil {
		return false, err
	}

	if changed {
		log.Info("Updated replication settings in postgresql.auto.conf file")
	}

	return changed, nil
}

// createStandbySignal creates a standby.signal file for PostgreSQL 12 and beyond
func createStandbySignal(pgData string) error {
	emptyFile, err := os.Create(filepath.Join(pgData, "standby.signal"))
	if emptyFile != nil {
		_ = emptyFile.Close()
	}

	return err
}

// RemoveArchiveModeFromPostgresAutoConf removes the "archive_mode" option from "postgresql.auto.conf"
func RemoveArchiveModeFromPostgresAutoConf(pgData string) (changed bool, err error) {
	targetFile := path.Join(pgData, "postgresql.auto.conf")
	currentContent, err := fileutils.ReadFile(targetFile)
	if err != nil {
		return false, fmt.Errorf("error while reading content of %v: %w", targetFile, err)
	}

	updatedContent := configfile.RemoveOptionFromConfigurationContents(currentContent, "archive_mode")
	return fileutils.WriteStringToFile(targetFile, updatedContent)
}

// SetArchiveModeToAlwaysIntoPostgresAutoConf sets the "archive_mode" option to "always" into "postgresql.auto.conf"
func SetArchiveModeToAlwaysIntoPostgresAutoConf(pgData string) (changed bool, err error) {
	targetFile := path.Join(pgData, "postgresql.auto.conf")
	currentContent, err := fileutils.ReadFile(targetFile)
	if err != nil {
		return false, fmt.Errorf("error while reading content of %v: %w", targetFile, err)
	}

	options := map[string]string{
		"archive_mode": "always",
	}

	updatedContent := configfile.UpdateConfigurationContents(currentContent, options)

	return fileutils.WriteStringToFile(targetFile, updatedContent)
}
