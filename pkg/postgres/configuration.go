/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

const (
	// hbaHeader is the header of generated pg_hba.conf.
	// The content provided by the user is inserted after this text
	hbaHeader = `
# Grant local access
local all all peer map=local

# Require client certificate authentication for the streaming_replica user
hostssl postgres streaming_replica all cert
hostssl replication streaming_replica all cert
`

	// hbaFooter is the footer of generated pg_hba.conf.
	// The content provided by the user is inserted before this text
	hbaFooter = `
# Otherwise use md5 authentication
host all all all md5
`
	// fixedConfigurationParameter are the configuration parameters
	// whose value is managed by the operator and should not be changed
	// by the user
	fixedConfigurationParameter = "fixed"

	// blockedConfigurationParameter are the configuration parameters
	// whose value must not be changed from the default one for the
	// operator to work correctly
	blockedConfigurationParameter = "blocked"

	// CertificatesDir location to store the certificates
	CertificatesDir = "/controller/certificates/"

	// ServerCertificateLocation is the location where the server certificate
	// is stored
	ServerCertificateLocation = CertificatesDir + "server.crt"

	// ServerKeyLocation is the location where the private key is stored
	ServerKeyLocation = CertificatesDir + "server.key"

	// StreamingReplicaCertificateLocation is the location where the certificate
	// of the "postgres" user is stored
	StreamingReplicaCertificateLocation = CertificatesDir + "streaming_replica.crt"

	// StreamingReplicaKeyLocation is the location where the private key of
	// the "postgres" user is stored
	StreamingReplicaKeyLocation = CertificatesDir + "streaming_replica.key"

	// ClientCACertificateLocation is the location where the CA certificate
	// is stored, and this certificate will be use to authenticate
	// client certificates
	ClientCACertificateLocation = CertificatesDir + "client-ca.crt"

	// ServerCACertificateLocation is the location where the CA certificate
	// is stored, and this certificate will be use to authenticate
	// server certificates
	ServerCACertificateLocation = CertificatesDir + "server-ca.crt"

	// BarmanEndpointCACertificateLocation is the location where the barman endpoint
	// CA certificate is stored
	BarmanEndpointCACertificateLocation = CertificatesDir + "barman-ca.crt"

	// BackupTemporaryDirectory provides a path to backup temporary files
	// needed in the recovery process
	BackupTemporaryDirectory = "/controller/backup"

	// RecoveryTemporaryDirectory provides a path to store temporary files
	// needed in the recovery process
	RecoveryTemporaryDirectory = "/controller/recovery"

	// SocketDirectory provides a path to store the Unix socket to be
	// used by the PostgreSQL server
	SocketDirectory = "/controller/run"

	// LogPath is the path of the folder used by the logging_collector
	LogPath = "/controller/log"

	// LogFileName is the name of the file produced by the logging_collector,
	// excluding the extension. The logging collector process will append
	// `.csv` and `.log` as needed.
	LogFileName = "postgres"

	// CNPConfigSha256 is the parameter to be used to inject the sha256 of the
	// config in the custom.conf file
	CNPConfigSha256 = "cnp.config_sha256"

	// SharedPreloadLibraries shared preload libraries key in the config
	SharedPreloadLibraries = "shared_preload_libraries"
)

// MajorVersionRangeUnlimited is used to represent an unbound limit in a MajorVersionRange
const MajorVersionRangeUnlimited = 0

// MajorVersionRange is used to represent a range of PostgreSQL versions
type MajorVersionRange = struct {
	// The minimum limit of PostgreSQL major version, extreme included
	Min int

	// The maximum limit of PostgreSQL version, extreme excluded, or MajorVersionRangeUnlimited
	Max int
}

// SettingsCollection is a collection of PostgreSQL settings
type SettingsCollection = map[string]string

// ConfigurationSettings is the set of settings that are applied,
// together with the parameters supplied by the users, to generate a custom
// PostgreSQL configuration
type ConfigurationSettings struct {
	// These settings are applied to the PostgreSQL default configuration when
	// the user don't specify something different
	GlobalDefaultSettings SettingsCollection

	// The following settings are like GlobalPostgresSettings
	// but are relative only to certain PostgreSQL versions
	DefaultSettings map[MajorVersionRange]SettingsCollection

	// The following settings are applied to the final PostgreSQL configuration,
	// even if the user specified something different
	MandatorySettings SettingsCollection

	// The following settings are applied if pgaudit is enabled
	PgAuditSettings SettingsCollection
}

// ConfigurationInfo contains the required information to create a PostgreSQL
// configuration
type ConfigurationInfo struct {
	// The name of this cluster
	ClusterName string

	// The database settings to be used
	Settings ConfigurationSettings

	// The major version
	MajorVersion int

	// The list of user-level settings
	UserSettings map[string]string

	// If we need to include mandatory settings or not
	IncludingMandatory bool

	// The list of replicas
	Replicas []string

	// The number of desired number of synchronous replicas
	SyncReplicas int

	// If the generated configuration should contain shared_preload_libraries too or no
	IncludingSharedPreloadLibraries bool

	// List of additional sharedPreloadLibraries to be loaded
	AdditionalSharedPreloadLibraries []string
}

// ManagedExtension defines all the information about a managed extension
type ManagedExtension struct {
	// Name of the extension
	Name string
	// SkipCreateExtension is true when the extension is made only from a shared preload library
	SkipCreateExtension bool
	// Namespaces contains the configuration namespaces handled by the extension
	Namespaces []string
	// SharedPreloadLibraries is the list of needed shared preload libraries
	SharedPreloadLibraries []string
}

// IsUsed checks whether a configuration namespace in the namespaces list
// is used in the user provided configuration
func (e ManagedExtension) IsUsed(userConfigs map[string]string) bool {
	for k := range userConfigs {
		for _, namespace := range e.Namespaces {
			if strings.HasPrefix(k, namespace+".") {
				return true
			}
		}
	}
	return false
}

var (
	// ManagedExtensions contains the list of extensions the operator supports to manage
	ManagedExtensions = []ManagedExtension{
		{
			Name:                   "pgaudit",
			Namespaces:             []string{"pgaudit"},
			SharedPreloadLibraries: []string{"pgaudit"},
		},
		{
			Name:                   "pg_stat_statements",
			Namespaces:             []string{"pg_stat_statements"},
			SharedPreloadLibraries: []string{"pg_stat_statements"},
		},
		{
			Name:                   "auto_explain",
			SkipCreateExtension:    true,
			Namespaces:             []string{"auto_explain"},
			SharedPreloadLibraries: []string{"auto_explain"},
		},
	}

	// FixedConfigurationParameters contains the parameters that can't be
	// changed by the user
	FixedConfigurationParameters = map[string]string{
		// The following parameters need a restart to be applied
		"allow_system_table_mods":    blockedConfigurationParameter,
		"archive_mode":               fixedConfigurationParameter,
		"bonjour":                    blockedConfigurationParameter,
		"bonjour_name":               blockedConfigurationParameter,
		"cluster_name":               fixedConfigurationParameter,
		"config_file":                blockedConfigurationParameter,
		"data_directory":             blockedConfigurationParameter,
		"data_sync_retry":            blockedConfigurationParameter,
		"dynamic_shared_memory_type": blockedConfigurationParameter,
		"event_source":               blockedConfigurationParameter,
		"external_pid_file":          blockedConfigurationParameter,
		"hba_file":                   blockedConfigurationParameter,
		"hot_standby":                blockedConfigurationParameter,
		"huge_pages":                 blockedConfigurationParameter,
		"ident_file":                 blockedConfigurationParameter,
		"jit_provider":               blockedConfigurationParameter,
		"listen_addresses":           blockedConfigurationParameter,
		"logging_collector":          blockedConfigurationParameter,
		"port":                       fixedConfigurationParameter,
		"primary_conninfo":           fixedConfigurationParameter,
		"primary_slot_name":          fixedConfigurationParameter,
		"recovery_target":            fixedConfigurationParameter,
		"recovery_target_action":     fixedConfigurationParameter,
		"recovery_target_inclusive":  fixedConfigurationParameter,
		"recovery_target_lsn":        fixedConfigurationParameter,
		"recovery_target_name":       fixedConfigurationParameter,
		"recovery_target_time":       fixedConfigurationParameter,
		"recovery_target_timeline":   fixedConfigurationParameter,
		"recovery_target_xid":        fixedConfigurationParameter,
		"restore_command":            fixedConfigurationParameter,
		"shared_preload_libraries":   fixedConfigurationParameter,
		"shared_memory_type":         blockedConfigurationParameter,
		"unix_socket_directories":    blockedConfigurationParameter,
		"unix_socket_group":          blockedConfigurationParameter,
		"unix_socket_permissions":    blockedConfigurationParameter,
		"wal_level":                  fixedConfigurationParameter,
		"wal_log_hints":              fixedConfigurationParameter,

		// The following parameters need a reload to be applied
		"archive_cleanup_command":                blockedConfigurationParameter,
		"archive_command":                        fixedConfigurationParameter,
		"archive_timeout":                        fixedConfigurationParameter,
		"full_page_writes":                       fixedConfigurationParameter,
		"log_destination":                        blockedConfigurationParameter,
		"log_directory":                          blockedConfigurationParameter,
		"log_file_mode":                          blockedConfigurationParameter,
		"log_filename":                           blockedConfigurationParameter,
		"log_rotation_age":                       blockedConfigurationParameter,
		"log_rotation_size":                      blockedConfigurationParameter,
		"log_truncate_on_rotation":               blockedConfigurationParameter,
		"promote_trigger_file":                   blockedConfigurationParameter,
		"recovery_end_command":                   blockedConfigurationParameter,
		"recovery_min_apply_delay":               blockedConfigurationParameter,
		"restart_after_crash":                    blockedConfigurationParameter,
		"ssl":                                    fixedConfigurationParameter,
		"ssl_ca_file":                            fixedConfigurationParameter,
		"ssl_cert_file":                          fixedConfigurationParameter,
		"ssl_ciphers":                            fixedConfigurationParameter,
		"ssl_crl_file":                           fixedConfigurationParameter,
		"ssl_dh_params_file":                     fixedConfigurationParameter,
		"ssl_ecdh_curve":                         fixedConfigurationParameter,
		"ssl_key_file":                           fixedConfigurationParameter,
		"ssl_max_protocol_version":               fixedConfigurationParameter,
		"ssl_min_protocol_version":               fixedConfigurationParameter,
		"ssl_passphrase_command":                 fixedConfigurationParameter,
		"ssl_passphrase_command_supports_reload": fixedConfigurationParameter,
		"ssl_prefer_server_ciphers":              fixedConfigurationParameter,
		"stats_temp_directory":                   blockedConfigurationParameter,
		"synchronous_standby_names":              fixedConfigurationParameter,
		"syslog_facility":                        blockedConfigurationParameter,
		"syslog_ident":                           blockedConfigurationParameter,
		"syslog_sequence_numbers":                blockedConfigurationParameter,
		"syslog_split_messages":                  blockedConfigurationParameter,
	}

	// CnpConfigurationSettings contains the settings that represent the
	// default and the mandatory behavior of CNP
	CnpConfigurationSettings = ConfigurationSettings{
		GlobalDefaultSettings: SettingsCollection{
			"max_parallel_workers":     "32",
			"max_worker_processes":     "32",
			"max_replication_slots":    "32",
			"logging_collector":        "on",
			"log_destination":          "csvlog",
			"log_rotation_age":         "0",
			"log_rotation_size":        "0",
			"log_truncate_on_rotation": "false",
			"log_directory":            LogPath,
			"log_filename":             LogFileName,
			// Workaround for PostgreSQL not behaving correctly when
			// a default value is not explicit in the postgresql.conf and
			// the parameter cannot be changed without a restart.
			SharedPreloadLibraries: "",
		},
		DefaultSettings: map[MajorVersionRange]SettingsCollection{
			{MajorVersionRangeUnlimited, 130000}: {
				"wal_keep_segments": "32",
			},
			{130000, MajorVersionRangeUnlimited}: {
				"wal_keep_size": "512MB",
			},
		},
		MandatorySettings: SettingsCollection{
			"listen_addresses":        "*",
			"unix_socket_directories": SocketDirectory,
			"hot_standby":             "true",
			"archive_mode":            "on",
			"archive_command":         "/controller/manager wal-archive %p",
			"port":                    "5432",
			"wal_level":               "logical",
			"wal_log_hints":           "on",
			"archive_timeout":         "5min", // TODO support configurable archive timeout
			"full_page_writes":        "on",
			"ssl":                     "on",
			"ssl_cert_file":           ServerCertificateLocation,
			"ssl_key_file":            ServerKeyLocation,
			"ssl_ca_file":             ClientCACertificateLocation,
		},
	}
)

// CreateHBARules will create the content of pg_hba.conf file given
// the rules set by the cluster spec
func CreateHBARules(hba []string) string {
	var hbaContent []string
	hbaContent = append(hbaContent, strings.TrimSpace(hbaHeader), "")
	if len(hba) > 0 {
		hbaContent = append(hbaContent, hba...)
		hbaContent = append(hbaContent, "")
	}
	hbaContent = append(hbaContent, strings.TrimSpace(hbaFooter), "")

	return strings.Join(hbaContent, "\n")
}

// PgConfiguration wraps configuration parameters with some checks
type PgConfiguration struct {
	configs map[string]string
}

// OverwriteConfig overwrites a configuration in the map, given the key/value pair.
// If the map is nil, it is created and the pair is added
func (p *PgConfiguration) OverwriteConfig(key, value string) {
	if p.configs == nil {
		p.configs = make(map[string]string)
	}

	p.configs[key] = value
}

// AddSharedPreloadLibrary add anew shared preloaded library to PostgreSQL configuration
func (p *PgConfiguration) AddSharedPreloadLibrary(newLibrary string) {
	if len(newLibrary) == 0 {
		return
	}
	if strings.Contains(p.configs[SharedPreloadLibraries], newLibrary) {
		return
	}
	if libraries, ok := p.configs[SharedPreloadLibraries]; ok &&
		libraries != "" {
		p.configs[SharedPreloadLibraries] = strings.Join([]string{libraries, newLibrary}, ",")
		return
	}
	p.configs[SharedPreloadLibraries] = newLibrary
}

// GetConfig retrieves a configuration from the map of configurations, given the key
func (p *PgConfiguration) GetConfig(key string) string {
	return p.configs[key]
}

// GetSortedList returns a sorted list of configurations
func (p *PgConfiguration) GetSortedList() []string {
	parameters := make([]string, len(p.configs))
	i := 0
	for key := range p.configs {
		parameters[i] = key
		i++
	}
	sort.Strings(parameters)
	return parameters
}

// CreatePostgresqlConfiguration creates the configuration from the settings
// and the default values
func CreatePostgresqlConfiguration(info ConfigurationInfo) *PgConfiguration {
	// Start from scratch
	configuration := &PgConfiguration{}

	// Set all the default settings
	setDefaultConfigurations(info, configuration)

	// Apply all the values from the user, overriding defaults
	for key, value := range info.UserSettings {
		configuration.OverwriteConfig(key, value)
	}

	// Apply all mandatory settings, on top of defaults and user settings
	if info.IncludingMandatory {
		for key, value := range info.Settings.MandatorySettings {
			configuration.OverwriteConfig(key, value)
		}
	}

	// Apply the list of replicas
	setReplicasListConfigurations(info, configuration)

	if info.IncludingSharedPreloadLibraries {
		// Set all managed shared preload libraries
		setManagedSharedPreloadLibraries(info, configuration)

		// Set all user provided shared preload libraries
		setUserSharedPreloadLibraries(info, configuration)
	}

	return configuration
}

//  setDefaultConfigurations sets all default configurations into the configuration map
// from the provided info
func setDefaultConfigurations(info ConfigurationInfo, configuration *PgConfiguration) {
	// start from the global default settings
	for key, value := range info.Settings.GlobalDefaultSettings {
		configuration.OverwriteConfig(key, value)
	}

	// apply settings relative to a certain PostgreSQL version
	for constraints, settings := range info.Settings.DefaultSettings {
		if constraints.Min == MajorVersionRangeUnlimited || (constraints.Min <= info.MajorVersion) {
			if constraints.Max == MajorVersionRangeUnlimited || (info.MajorVersion < constraints.Max) {
				for key, value := range settings {
					configuration.OverwriteConfig(key, value)
				}
			}
		}
	}
}

// setManagedSharedPreloadLibraries sets all additional preloaded libraries
func setManagedSharedPreloadLibraries(info ConfigurationInfo, configuration *PgConfiguration) {
	for _, extension := range ManagedExtensions {
		if extension.IsUsed(info.UserSettings) {
			for _, library := range extension.SharedPreloadLibraries {
				configuration.AddSharedPreloadLibrary(library)
			}
		}
	}
}

// setUserSharedPreloadLibraries sets all additional preloaded libraries.
// The resulting list will have all the user provided libraries, followed by all the ones managed
// by the operator, removing any duplicate and keeping the first occurrence in case of duplicates.
// Therefore the user provided order is preserved, if an overlap (with the ones already present) happens
func setUserSharedPreloadLibraries(info ConfigurationInfo, configuration *PgConfiguration) {
	oldLibraries := strings.Split(configuration.GetConfig(SharedPreloadLibraries), ",")
	dedupedLibraries := make(map[string]bool, len(oldLibraries)+len(info.AdditionalSharedPreloadLibraries))
	var libraries []string
	for _, library := range append(info.AdditionalSharedPreloadLibraries, oldLibraries...) {
		// if any, delete empty string
		if library == "" {
			continue
		}
		if !dedupedLibraries[library] {
			dedupedLibraries[library] = true
			libraries = append(libraries, library)
		}
	}
	if len(libraries) > 0 {
		configuration.OverwriteConfig(SharedPreloadLibraries, strings.Join(libraries, ","))
	}
}

// setReplicasListConfigurations sets the standby node list
func setReplicasListConfigurations(info ConfigurationInfo, configuration *PgConfiguration) {
	if info.Replicas != nil && info.SyncReplicas > 0 {
		escapedReplicas := make([]string, len(info.Replicas))
		for idx, name := range info.Replicas {
			escapedReplicas[idx] = escapePostgresConfLiteral(name)
		}
		configuration.OverwriteConfig("synchronous_standby_names", fmt.Sprintf(
			"ANY %v (%v)",
			info.SyncReplicas,
			strings.Join(escapedReplicas, ",")))
	}

	if info.ClusterName != "" {
		// Apply the cluster name
		configuration.OverwriteConfig("cluster_name", info.ClusterName)
	}
}

// FillCNPConfiguration creates the actual PostgreSQL configuration
// for CNP given the user settings and the major version. This is
// useful during the configuration validation
func FillCNPConfiguration(
	majorVersion int,
	userSettings map[string]string,
) map[string]string {
	info := ConfigurationInfo{
		Settings:     CnpConfigurationSettings,
		MajorVersion: majorVersion,
		UserSettings: userSettings,
		Replicas:     nil,
	}
	pgConfig := CreatePostgresqlConfiguration(info)
	return pgConfig.configs
}

// CreatePostgresqlConfFile creates the contents of the postgresql.conf file
func CreatePostgresqlConfFile(configuration *PgConfiguration) (string, string) {
	// We need to be able to compare two configurations generated
	// by operator to know if they are different or not. To do
	// that we sort the configuration by parameter name as order
	// is really irrelevant for our purposes
	parameters := configuration.GetSortedList()
	postgresConf := ""
	for _, parameter := range parameters {
		postgresConf += fmt.Sprintf(
			"%v = %v\n",
			parameter,
			escapePostgresConfValue(configuration.configs[parameter]))
	}

	sha256sum := fmt.Sprintf("%x", sha256.Sum256([]byte(postgresConf)))
	postgresConf += fmt.Sprintf("%v = %v", CNPConfigSha256,
		escapePostgresConfValue(sha256sum))

	return postgresConf, sha256sum
}

// escapePostgresConfValue escapes a value to make its representation
// directly embeddable in the PostgreSQL configuration file
func escapePostgresConfValue(value string) string {
	return fmt.Sprintf("'%v'", strings.ReplaceAll(value, "'", "''"))
}

// escapePostgresLiteral escapes a value to make its representation
// similar to the literals in PostgreSQL
func escapePostgresConfLiteral(value string) string {
	return fmt.Sprintf("\"%v\"", strings.ReplaceAll(value, "\"", "\"\""))
}
