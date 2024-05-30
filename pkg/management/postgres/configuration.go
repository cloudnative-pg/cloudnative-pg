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
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	postgresutils "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
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
		return ""
	}
	ldapConfig := cluster.Spec.PostgresConfiguration.LDAP

	ldapConfigString += fmt.Sprintf("host all all 0.0.0.0/0 ldap ldapserver=%s",
		quoteHbaLiteral(ldapConfig.Server))

	if ldapConfig.Port != 0 {
		ldapConfigString += fmt.Sprintf(" ldapport=%d", ldapConfig.Port)
	}

	if ldapConfig.Scheme != "" {
		ldapConfigString += fmt.Sprintf(" ldapscheme=%s",
			quoteHbaLiteral(string(ldapConfig.Scheme)))
	}

	if ldapConfig.TLS {
		ldapConfigString += " ldaptls=1"
	}

	if ldapConfig.BindAsAuth != nil {
		log.Debug("Setting pg_hba to use ldap authentication in simple bind mode",
			"server", ldapConfig.Server,
			"prefix", ldapConfig.BindAsAuth.Prefix,
			"suffix", ldapConfig.BindAsAuth.Suffix)
		ldapConfigString += fmt.Sprintf(" ldapprefix=%s ldapsuffix=%s",
			quoteHbaLiteral(ldapConfig.BindAsAuth.Prefix),
			quoteHbaLiteral(ldapConfig.BindAsAuth.Suffix))
	}

	if ldapConfig.BindSearchAuth != nil {
		log.Debug("setting pg_hba to use ldap authentication in search+bind mode",
			"server", ldapConfig.Server,
			"BaseDN", ldapConfig.BindSearchAuth.BaseDN,
			"binDN", ldapConfig.BindSearchAuth.BindDN,
			"search attribute", ldapConfig.BindSearchAuth.SearchAttribute,
			"search filter", ldapConfig.BindSearchAuth.SearchFilter)

		ldapConfigString += fmt.Sprintf(" ldapbasedn=%s ldapbinddn=%s ldapbindpasswd=%s",
			quoteHbaLiteral(ldapConfig.BindSearchAuth.BaseDN),
			quoteHbaLiteral(ldapConfig.BindSearchAuth.BindDN),
			quoteHbaLiteral(ldapBindPassword))
		if ldapConfig.BindSearchAuth.SearchFilter != "" {
			ldapConfigString += fmt.Sprintf(" ldapsearchfilter=%s",
				quoteHbaLiteral(ldapConfig.BindSearchAuth.SearchFilter))
		}
		if ldapConfig.BindSearchAuth.SearchAttribute != "" {
			ldapConfigString += fmt.Sprintf(" ldapsearchattribute=%s",
				quoteHbaLiteral(ldapConfig.BindSearchAuth.SearchAttribute))
		}
	}

	return ldapConfigString
}

// quoteHbaLiteral quotes a string according to pg_hba.conf rules
// (see https://www.postgresql.org/docs/current/auth-pg-hba-conf.html)
func quoteHbaLiteral(literal string) string {
	literal = strings.ReplaceAll(literal, `"`, `""`)
	literal = strings.ReplaceAll(literal, "\n", "\\\n")
	return fmt.Sprintf(`"%s"`, literal)
}

// generatePostgresqlIdent generates the pg_ident.conf content given
// a set of additional pg_ident lines that is usually taken from the
// Cluster configuration
func (instance *Instance) generatePostgresqlIdent(additionalLines []string) (string, error) {
	return postgres.CreateIdentRules(
		additionalLines,
		getCurrentUserOrDefaultToInsecureMapping(),
	)
}

// RefreshPGIdent generates and writes down the pg_ident.conf file given
// a set of additional pg_ident lines that is usually taken from the
// Cluster configuration
func (instance *Instance) RefreshPGIdent(additionalLines []string) (postgresIdentChanged bool, err error) {
	// Generate pg_ident.conf file
	pgIdentContent, err := instance.generatePostgresqlIdent(additionalLines)
	if err != nil {
		return false, nil
	}
	postgresIdentChanged, err = InstallPgDataFileContent(
		instance.PgData,
		pgIdentContent,
		constants.PostgresqlIdentFile)
	if err != nil {
		return postgresIdentChanged, fmt.Errorf(
			"installing postgresql Ident rules: %w",
			err)
	}

	return postgresIdentChanged, err
}

// UpdateReplicaConfiguration updates the override.conf or recovery.conf file for the proper version
// of PostgreSQL, using the specified connection string to connect to the primary server
func UpdateReplicaConfiguration(pgData, primaryConnInfo, slotName string) (changed bool, err error) {
	changed, err = configurePostgresOverrideConfFile(pgData, primaryConnInfo, slotName)
	if err != nil {
		return changed, err
	}

	major, err := postgresutils.GetMajorVersion(pgData)
	if err != nil {
		return false, err
	}

	if major < 12 {
		return configureRecoveryConfFile(pgData, primaryConnInfo, slotName)
	}

	return changed, createStandbySignal(pgData)
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
		log.Info("Updated replication settings", "filename", "recovery.conf")
	}

	return changed, nil
}

// configurePostgresOverrideConfFile writes the content of override.conf file, including
// replication information
func configurePostgresOverrideConfFile(pgData, primaryConnInfo, slotName string) (changed bool, err error) {
	targetFile := path.Join(pgData, constants.PostgresqlOverrideConfigurationFile)

	major, err := postgresutils.GetMajorVersion(pgData)
	if err != nil {
		return false, err
	}

	options := make(map[string]string)

	// Write replication control as GUCs (from PostgreSQL 12 or above)
	if major >= 12 {
		options = map[string]string{
			"restore_command": fmt.Sprintf(
				"/controller/manager wal-restore --log-destination %s/%s.json %%f %%p",
				postgres.LogPath, postgres.LogFileName),
			"recovery_target_timeline": "latest",
			"primary_slot_name":        slotName,
			"primary_conninfo":         primaryConnInfo,
		}
	}

	// Ensure that override.conf file contains just the above options
	changed, err = configfile.WritePostgresConfiguration(targetFile, options)
	if err != nil {
		return false, err
	}

	if changed {
		log.Info("Updated replication settings", "filename", constants.PostgresqlOverrideConfigurationFile)
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

var migrateAutoConfOptions = []string{
	"archive_mode",
	"primary_conninfo",
	"primary_slot_name",
	"recovery_target_timeline",
	"restore_command",
}

var cleanupAutoConfOptions = []string{
	"archive_mode",
	"primary_conninfo",
	"primary_slot_name",
	"recovery_target",
	"recovery_target_inclusive",
	"recovery_target_lsn",
	"recovery_target_name",
	"recovery_target_time",
	"recovery_target_timeline",
	"recovery_target_xid",
	"restore_command",
}

// migratePostgresAutoConfFile migrates options managed by the operator from `postgresql.auto.conf` file,
// to `override.conf` file for an upgrade case.
// Returns a boolean indicating if any changes were done and any errors encountered
func (instance *Instance) migratePostgresAutoConfFile(ctx context.Context) (bool, error) {
	contextLogger := log.FromContext(ctx).WithName("migratePostgresAutoConfFile")

	overrideConfPath := filepath.Join(instance.PgData, constants.PostgresqlOverrideConfigurationFile)
	autoConfFile := filepath.Join(instance.PgData, "postgresql.auto.conf")
	autoConfContent, readLinesErr := fileutils.ReadFileLines(autoConfFile)
	if readLinesErr != nil {
		return false, fmt.Errorf("error while reading postgresql.auto.conf file: %w", readLinesErr)
	}

	overrideConfExists, _ := fileutils.FileExists(overrideConfPath)
	options := configfile.ReadLinesFromConfigurationContents(autoConfContent, migrateAutoConfOptions...)
	if len(options) == 0 && overrideConfExists {
		contextLogger.Trace("no action taken, options slice is empty")
		return false, nil
	}

	contextLogger.Info("Start to migrate replication settings",
		"filename", constants.PostgresqlOverrideConfigurationFile,
		"targetFileExists", overrideConfExists,
		"options", options,
	)

	// We create the override.conf file only if it doesn't exist (first-time migration).
	// The instance manager manages the content of this file, and it will be overwritten
	// later during the configuration update. We create it here just as a precaution.
	if !overrideConfExists {
		if _, err := fileutils.WriteLinesToFile(overrideConfPath, options); err != nil {
			return false, fmt.Errorf("migrating replication settings: %w",
				err)
		}

		if _, err := configfile.EnsureIncludes(
			path.Join(instance.PgData, "postgresql.conf"),
			constants.PostgresqlOverrideConfigurationFile,
		); err != nil {
			return false, fmt.Errorf("migrating replication settings: %w",
				err)
		}
	}

	if _, err := fileutils.WriteLinesToFile(autoConfFile,
		configfile.RemoveOptionsFromConfigurationContents(
			autoConfContent, cleanupAutoConfOptions...),
	); err != nil {
		return true, fmt.Errorf("cleaning up postgresql.auto.conf file: %w", err)
	}

	contextLogger.Info("Migrated replication settings",
		"filename", constants.PostgresqlOverrideConfigurationFile,
		"overrideConfCreated", !overrideConfExists,
		"options", options,
	)

	return true, nil
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
		IsWalArchivingDisabled:           utils.IsWalArchivingDisabled(&cluster.ObjectMeta),
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

	// Set temporary tablespaces
	for _, tablespace := range cluster.Spec.Tablespaces {
		if tablespace.Temporary {
			info.TemporaryTablespaces = append(info.TemporaryTablespaces, tablespace.Name)
		}
	}
	sort.Strings(info.TemporaryTablespaces)

	conf, sha256 := postgres.CreatePostgresqlConfFile(postgres.CreatePostgresqlConfiguration(info))
	return conf, sha256, nil
}

// configurePostgresForImport configures Postgres to be optimized for the firt import
// process, by writing dedicated options the override.conf file just for this phase
func configurePostgresForImport(ctx context.Context, pgData string) (changed bool, err error) {
	contextLogger := log.FromContext(ctx)
	targetFile := path.Join(pgData, constants.PostgresqlOverrideConfigurationFile)

	// Force the following GUCs to optmize the loading process
	options := map[string]string{
		"archive_mode":     "off",
		"fsync":            "off",
		"wal_level":        "minimal",
		"full_page_writes": "off",
		"max_wal_senders":  "0",
	}

	// Ensure that override.conf file contains just the above options
	changed, err = configfile.WritePostgresConfiguration(targetFile, options)
	if err != nil {
		return false, err
	}

	if changed {
		contextLogger.Info("Configuration optimized for import",
			"filename", constants.PostgresqlOverrideConfigurationFile)
	}

	return changed, nil
}
