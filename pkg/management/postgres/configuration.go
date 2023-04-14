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

package postgres

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	postgresutils "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
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
func (instance *Instance) RefreshConfigurationFilesFromCluster(
	cluster *apiv1.Cluster,
	preserveUserSettings bool,
) (bool, error) {
	postgresConfiguration, sha256, err := createPostgresqlConfiguration(cluster, preserveUserSettings)
	if err != nil {
		return false, err
	}

	postgresConfigurationChanged, err := InstallPgDataFileContent(
		instance.PgData,
		postgresConfiguration,
		constants.PostgresqlCustomConfigurationFile)
	if err != nil {
		return postgresConfigurationChanged, fmt.Errorf(
			"installing postgresql configuration: %w",
			err)
	}

	if sha256 != "" && postgresConfigurationChanged {
		instance.ConfigSha256 = sha256
	}

	return postgresConfigurationChanged, nil
}

// GeneratePostgresqlHBA generates the pg_hba.conf content with the LDAP configuration if configured.
func (instance *Instance) GeneratePostgresqlHBA(cluster *apiv1.Cluster, ldapBindPassword string) (string, error) {
	version, err := cluster.GetPostgresqlVersion()
	if err != nil {
		return "", err
	}

	// From PostgreSQL 14 we default to SCRAM-SHA-256
	// authentication as the default `password_encryption`
	// is set to `scram-sha-256` and this is the most
	// secure authentication method available.
	//
	// See:
	// https://www.postgresql.org/docs/14/release-14.html
	defaultAuthenticationMethod := "scram-sha-256"
	if version < 140000 {
		defaultAuthenticationMethod = "md5"
	}

	return postgres.CreateHBARules(
		cluster.Spec.PostgresConfiguration.PgHBA,
		defaultAuthenticationMethod,
		buildLDAPConfigString(cluster, ldapBindPassword))
}

// RefreshPGHBA generates and writes down the pg_hba.conf file
func (instance *Instance) RefreshPGHBA(cluster *apiv1.Cluster, ldapBindPassword string) (
	postgresHBAChanged bool,
	err error,
) {
	// Generate pg_hba.conf file
	pgHBAContent, err := instance.GeneratePostgresqlHBA(cluster, ldapBindPassword)
	if err != nil {
		return false, nil
	}
	postgresHBAChanged, err = InstallPgDataFileContent(
		instance.PgData,
		pgHBAContent,
		constants.PostgresqlHBARulesFile)
	if err != nil {
		return postgresHBAChanged, fmt.Errorf(
			"installing postgresql HBA rules: %w",
			err)
	}

	return postgresHBAChanged, err
}

// buildLDAPConfigString will create the string needed for ldap in pg_hba
func buildLDAPConfigString(cluster *apiv1.Cluster, ldapBindPassword string) string {
	var ldapConfigString string
	if !cluster.GetEnableLDAPAuth() {
		return ldapConfigString
	}
	ldapConfig := cluster.Spec.PostgresConfiguration.LDAP

	ldapConfigString += fmt.Sprintf("host all all 0.0.0.0/0 ldap ldapserver=%s", ldapConfig.Server)

	if ldapConfig.Port != 0 {
		ldapConfigString += fmt.Sprintf(" ldapport=%d", ldapConfig.Port)
	}

	if ldapConfig.Scheme != "" {
		ldapConfigString += fmt.Sprintf(" ldapscheme=%s", ldapConfig.Scheme)
	}

	if ldapConfig.TLS {
		ldapConfigString += " ldaptls=1"
	}

	if ldapConfig.BindAsAuth != nil {
		log.Debug("Setting pg_hba to use ldap authentication in simple bind mode",
			"server", ldapConfig.Server,
			"prefix", ldapConfig.BindAsAuth.Prefix,
			"suffix", ldapConfig.BindAsAuth.Suffix)
		ldapConfigString += fmt.Sprintf(" ldapprefix=\"%s\" ldapsuffix=\"%s\"", ldapConfig.BindAsAuth.Prefix,
			ldapConfig.BindAsAuth.Suffix)
	}

	if ldapConfig.BindSearchAuth != nil {
		log.Debug("setting pg_hba to use ldap authentication in search+bind mode",
			"server", ldapConfig.Server,
			"BaseDN", ldapConfig.BindSearchAuth.BaseDN,
			"binDN", ldapConfig.BindSearchAuth.BindDN,
			"search attribute", ldapConfig.BindSearchAuth.SearchAttribute,
			"search filter", ldapConfig.BindSearchAuth.SearchFilter)

		ldapConfigString += fmt.Sprintf(" ldapbasedn=\"%s\" ldapbinddn=\"%s\" "+
			"ldapbindpasswd=%s", ldapConfig.BindSearchAuth.BaseDN, ldapConfig.BindSearchAuth.BindDN, ldapBindPassword)
		if ldapConfig.BindSearchAuth.SearchFilter != "" {
			ldapConfigString += fmt.Sprintf(" ldapsearchfilter=%s", ldapConfig.BindSearchAuth.SearchFilter)
		}
		if ldapConfig.BindSearchAuth.SearchAttribute != "" {
			ldapConfigString += fmt.Sprintf(" ldapsearchattribute=%s", ldapConfig.BindSearchAuth.SearchAttribute)
		}
	}

	return ldapConfigString
}

// UpdateReplicaConfiguration updates the postgresql.auto.conf or recovery.conf file for the proper version
// of PostgreSQL, using the specified connection string to connect to the primary server
func UpdateReplicaConfiguration(pgData, primaryConnInfo, slotName string) (changed bool, err error) {
	major, err := postgresutils.GetMajorVersion(pgData)
	if err != nil {
		return false, err
	}

	if major < 12 {
		return configureRecoveryConfFile(pgData, primaryConnInfo, slotName)
	}

	if err := createStandbySignal(pgData); err != nil {
		return false, err
	}

	return configurePostgresAutoConfFile(pgData, primaryConnInfo, slotName)
}

// configureRecoveryConfFile configures replication in the recovery.conf file
// for PostgreSQL 11 and earlier
func configureRecoveryConfFile(pgData, primaryConnInfo, slotName string) (changed bool, err error) {
	targetFile := path.Join(pgData, "recovery.conf")

	options := map[string]string{
		"standby_mode": "on",
		"restore_command": fmt.Sprintf(
			"/controller/manager wal-restore --log-destination %s/%s.json %%f %%p",
			postgres.LogPath, postgres.LogFileName),
		"recovery_target_timeline": "latest",
	}

	if slotName != "" {
		options["primary_slot_name"] = slotName
	}

	if primaryConnInfo != "" {
		options["primary_conninfo"] = primaryConnInfo
	}

	changed, err = configfile.UpdatePostgresConfigurationFile(
		targetFile,
		options,
		"primary_slot_name",
		"primary_conninfo",
	)
	if err != nil {
		return false, err
	}
	if changed {
		log.Info("Updated replication settings in recovery.conf file")
	}

	return changed, nil
}

// configurePostgresAutoConfFile configures replication in the postgresql.auto.conf file
// for PostgreSQL 12 and newer
func configurePostgresAutoConfFile(pgData, primaryConnInfo, slotName string) (changed bool, err error) {
	targetFile := path.Join(pgData, "postgresql.auto.conf")

	options := map[string]string{
		"restore_command": fmt.Sprintf(
			"/controller/manager wal-restore --log-destination %s/%s.json %%f %%p",
			postgres.LogPath, postgres.LogFileName),
		"recovery_target_timeline": "latest",
		"primary_slot_name":        slotName,
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
	emptyFile, err := os.Create(filepath.Clean(filepath.Join(pgData, "standby.signal")))
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

	updatedContent := configfile.RemoveOptionFromConfigurationContents(string(currentContent), "archive_mode")
	return fileutils.WriteStringToFile(targetFile, updatedContent)
}

// createPostgresqlConfiguration creates the PostgreSQL configuration to be
// used for this cluster and return it and its sha256 checksum
func createPostgresqlConfiguration(cluster *apiv1.Cluster, preserveUserSettings bool) (string, string, error) {
	// Extract the PostgreSQL major version
	fromVersion, err := cluster.GetPostgresqlVersion()
	if err != nil {
		return "", "", err
	}

	info := postgres.ConfigurationInfo{
		Settings:                         postgres.CnpgConfigurationSettings,
		MajorVersion:                     fromVersion,
		UserSettings:                     cluster.Spec.PostgresConfiguration.Parameters,
		IncludingSharedPreloadLibraries:  true,
		AdditionalSharedPreloadLibraries: cluster.Spec.PostgresConfiguration.AdditionalLibraries,
		IsReplicaCluster:                 cluster.IsReplica(),
	}

	if preserveUserSettings {
		info.PreserveFixedSettingsFromUser = true
	} else {
		info.IncludingMandatory = true
	}

	// Compute the actual number of sync replicas
	syncReplicas, electable := cluster.GetSyncReplicasData()
	info.SyncReplicas = syncReplicas
	info.SyncReplicasElectable = electable

	// Ensure a consistent ordering to avoid spurious configuration changes
	sort.Strings(info.SyncReplicasElectable)

	// Set cluster name
	info.ClusterName = cluster.Name

	conf, sha256 := postgres.CreatePostgresqlConfFile(postgres.CreatePostgresqlConfiguration(info))
	return conf, sha256, nil
}
