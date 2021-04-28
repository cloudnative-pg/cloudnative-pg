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

	"k8s.io/client-go/dynamic"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/configfile"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

// InstallPgDataFileContent install a file in PgData, returning true/false if
// the file has been changed and an error state
func InstallPgDataFileContent(pgdata, contents, destinationFile string) (bool, error) {
	targetFile := path.Join(pgdata, destinationFile)
	result, err := fileutils.WriteStringToFile(targetFile, contents)
	if err != nil {
		return false, err
	}

	if result {
		log.Log.Info(
			"Installed configuration file",
			"pgdata", pgdata,
			"filename", destinationFile)
	}

	return result, nil
}

// RefreshConfigurationFilesFromCluster receive a cluster object, generate the
// PostgreSQL configuration and rewrite the file in the PGDATA if needed. This
// function will return "true" if the configuration has been really changed.
// Important: this won't send a SIGHUP to the server
func (instance *Instance) RefreshConfigurationFilesFromCluster(cluster *apiv1.Cluster) (bool, error) {
	postgresConfiguration, err := cluster.CreatePostgresqlConfiguration()
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

	return postgresConfigurationChanged || postgresHBAChanged, nil
}

// RefreshConfigurationFiles get the latest version of the ConfigMap from the API
// server and then write the configuration in PGDATA
func (instance *Instance) RefreshConfigurationFiles(ctx context.Context, client dynamic.Interface) (bool, error) {
	cluster, err := utils.GetCluster(ctx, client, instance.Namespace, instance.ClusterName)
	if err != nil {
		return false, err
	}

	return instance.RefreshConfigurationFilesFromCluster(cluster)
}

// UpdateReplicaConfiguration update the postgresql.auto.conf or recovery.conf file for the proper version
// of PostgreSQL
func UpdateReplicaConfiguration(pgData string, clusterName string, podName string, primary bool) error {
	major, err := postgres.GetMajorVersion(pgData)
	if err != nil {
		return err
	}

	if primary {
		return nil
	}

	if major < 12 {
		return configureRecoveryConfFile(pgData, clusterName, podName)
	}

	if err := createStandbySignal(pgData); err != nil {
		return err
	}

	return configurePostgresAutoConfFile(pgData, clusterName, podName)
}

// configureRecoveryConfFile configures replication in the recovery.conf file
// for PostgreSQL 11 and earlier
func configureRecoveryConfFile(pgData string, clusterName string, podName string) error {
	log.Log.Info("Installing recovery.conf file")
	primaryConnInfo := buildPrimaryConnInfo(clusterName+"-rw", podName)
	targetFile := path.Join(pgData, "recovery.conf")

	options := map[string]string{
		"standby_mode":             "on",
		"primary_conninfo":         primaryConnInfo,
		"restore_command":          "/controller/manager wal-restore %f %p",
		"recovery_target_timeline": "latest",
	}

	err := configfile.UpdatePostgresConfigurationFile(targetFile, options)
	if err != nil {
		return err
	}

	return nil
}

// configurePostgresAutoConfFile configures replication a in the postgresql.auto.conf file
// for PostgreSQL 11 and earlier
func configurePostgresAutoConfFile(pgData string, clusterName string, podName string) error {
	log.Log.Info("Updating postgresql.auto.conf file")
	primaryConnInfo := buildPrimaryConnInfo(clusterName+"-rw", podName)
	targetFile := path.Join(pgData, "postgresql.auto.conf")

	options := map[string]string{
		"cluster_name":             clusterName,
		"primary_conninfo":         primaryConnInfo,
		"restore_command":          "/controller/manager wal-restore %f %p",
		"recovery_target_timeline": "latest",
	}

	err := configfile.UpdatePostgresConfigurationFile(targetFile, options)
	if err != nil {
		return err
	}

	return nil
}

// createStandbySignal creates a standby.signal file for PostgreSQL 12 and beyond
func createStandbySignal(pgData string) error {
	emptyFile, err := os.Create(filepath.Join(pgData, "standby.signal"))
	if emptyFile != nil {
		_ = emptyFile.Close()
	}

	return err
}
