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
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	postgresClient "github.com/cloudnative-pg/cnpg-i/pkg/postgres"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	postgresutils "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres/replication"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// InstallPgDataFileContent installs a file in PgData, returning true/false if
// the file has been changed and an error state
func InstallPgDataFileContent(ctx context.Context, pgdata, contents, destinationFile string) (bool, error) {
	contextLogger := log.FromContext(ctx)

	targetFile := path.Join(pgdata, destinationFile)
	result, err := fileutils.WriteStringToFile(targetFile, contents)
	if err != nil {
		return false, err
	}

	if result {
		contextLogger.Info(
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
	ctx context.Context,
	cluster *apiv1.Cluster,
	preserveUserSettings bool,
	operationType postgresClient.OperationType_Type,
) (bool, error) {
	pgMajor, err := postgresutils.GetMajorVersionFromPgData(instance.PgData)
	if err != nil {
		return false, err
	}

	postgresConfiguration, sha256, err := createPostgresqlConfiguration(
		ctx, cluster, preserveUserSettings, pgMajor,
		operationType,
	)
	if err != nil {
		return false, fmt.Errorf("creating postgresql configuration: %w", err)
	}
	postgresConfigurationChanged, err := InstallPgDataFileContent(
		ctx,
		instance.PgData,
		postgresConfiguration,
		constants.PostgresqlCustomConfigurationFile)
	if err != nil {
		return postgresConfigurationChanged, fmt.Errorf(
			"installing postgresql configuration: %w",
			err)
	}
	instance.ConfigSha256 = sha256

	return postgresConfigurationChanged, nil
}

// GeneratePostgresqlHBA generates the pg_hba.conf content with the LDAP configuration if configured.
func (instance *Instance) GeneratePostgresqlHBA(cluster *apiv1.Cluster, ldapBindPassword string) (string, error) {
	majorVersion, err := cluster.GetPostgresqlMajorVersion()
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
	if majorVersion < 14 {
		defaultAuthenticationMethod = "md5"
	}

	return postgres.CreateHBARules(
		cluster.Spec.PostgresConfiguration.PgHBA,
		defaultAuthenticationMethod,
		buildLDAPConfigString(cluster, ldapBindPassword))
}

// RefreshPGHBA generates and writes down the pg_hba.conf file
func (instance *Instance) RefreshPGHBA(ctx context.Context, cluster *apiv1.Cluster, ldapBindPassword string) (
	postgresHBAChanged bool,
	err error,
) {
	// Generate pg_hba.conf file
	pgHBAContent, err := instance.GeneratePostgresqlHBA(cluster, ldapBindPassword)
	if err != nil {
		return false, nil
	}
	postgresHBAChanged, err = InstallPgDataFileContent(
		ctx,
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
func (instance *Instance) RefreshPGIdent(
	ctx context.Context,
	additionalLines []string,
) (postgresIdentChanged bool, err error) {
	// Generate pg_ident.conf file
	pgIdentContent, err := instance.generatePostgresqlIdent(additionalLines)
	if err != nil {
		return false, nil
	}
	postgresIdentChanged, err = InstallPgDataFileContent(
		ctx,
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

	return changed, createStandbySignal(pgData)
}

// configurePostgresOverrideConfFile writes the content of override.conf file, including
// replication information. The “primary_slot_name` parameter will be generated only when the parameter slotName is not
// empty.
// Returns a boolean indicating if any changes were done and any errors encountered
func configurePostgresOverrideConfFile(pgData, primaryConnInfo, slotName string) (changed bool, err error) {
	targetFile := path.Join(pgData, constants.PostgresqlOverrideConfigurationFile)
	options := map[string]string{
		"restore_command": fmt.Sprintf(
			"/controller/manager wal-restore --log-destination %s/%s.json %%f %%p",
			postgres.LogPath, postgres.LogFileName),
		"recovery_target_timeline": "latest",
		"primary_conninfo":         primaryConnInfo,
	}

	if len(slotName) > 0 {
		options["primary_slot_name"] = slotName
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

// createPostgresqlConfiguration creates the PostgreSQL configuration to be
// used for this cluster and return it and its sha256 checksum
func createPostgresqlConfiguration(
	ctx context.Context,
	cluster *apiv1.Cluster,
	preserveUserSettings bool,
	majorVersion int,
	operationType postgresClient.OperationType_Type,
) (string, string, error) {
	info := postgres.ConfigurationInfo{
		Settings:                         postgres.CnpgConfigurationSettings,
		MajorVersion:                     majorVersion,
		UserSettings:                     cluster.Spec.PostgresConfiguration.Parameters,
		IncludingSharedPreloadLibraries:  true,
		AdditionalSharedPreloadLibraries: cluster.Spec.PostgresConfiguration.AdditionalLibraries,
		IsReplicaCluster:                 cluster.IsReplica(),
		IsWalArchivingDisabled:           utils.IsWalArchivingDisabled(&cluster.ObjectMeta),
		IsAlterSystemEnabled:             cluster.Spec.PostgresConfiguration.EnableAlterSystem,
		SynchronousStandbyNames:          replication.GetSynchronousStandbyNames(ctx, cluster),
	}

	if preserveUserSettings {
		info.PreserveFixedSettingsFromUser = true
	} else {
		info.IncludingMandatory = true
	}

	// Set cluster name
	info.ClusterName = cluster.Name

	// Set temporary tablespaces
	for _, tablespace := range cluster.Spec.Tablespaces {
		if tablespace.Temporary {
			info.TemporaryTablespaces = append(info.TemporaryTablespaces, tablespace.Name)
		}
	}
	sort.Strings(info.TemporaryTablespaces)

	// Set additional extensions
	for _, extension := range cluster.Spec.PostgresConfiguration.Extensions {
		info.AdditionalExtensions = append(
			info.AdditionalExtensions,
			postgres.AdditionalExtensionConfiguration{
				Name:                 extension.Name,
				ExtensionControlPath: extension.ExtensionControlPath,
				DynamicLibraryPath:   extension.DynamicLibraryPath,
			},
		)
	}

	// Setup minimum replay delay if we're on a replica cluster
	if cluster.IsReplica() && cluster.Spec.ReplicaCluster.MinApplyDelay != nil {
		info.RecoveryMinApplyDelay = cluster.Spec.ReplicaCluster.MinApplyDelay.Duration
	}

	if isSynchronizeLogicalDecodingEnabled(cluster) {
		slots := make([]string, 0, len(cluster.Status.InstanceNames)-1)
		for _, instanceName := range cluster.Status.InstanceNames {
			if instanceName == cluster.Status.CurrentPrimary {
				continue
			}
			slots = append(slots, cluster.GetSlotNameFromInstanceName(instanceName))
		}
		info.SynchronizedStandbySlots = slots
	}

	config, err := plugin.CreatePostgresqlConfigurationWithPlugins(ctx, info, operationType)
	if err != nil {
		return "", "", err
	}

	file, sha := postgres.CreatePostgresqlConfFile(config)
	return file, sha, nil
}

func isSynchronizeLogicalDecodingEnabled(cluster *apiv1.Cluster) bool {
	return cluster.Spec.ReplicationSlots != nil &&
		cluster.Spec.ReplicationSlots.HighAvailability != nil &&
		cluster.Spec.ReplicationSlots.HighAvailability.GetEnabled() &&
		cluster.Spec.ReplicationSlots.HighAvailability.SynchronizeLogicalDecoding
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
