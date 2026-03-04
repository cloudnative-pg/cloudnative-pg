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
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"iter"
	"math"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
)

// WalLevelValue a value that is assigned to the 'wal_level' configuration field
type WalLevelValue string

const (
	// ParameterWalLevel the configuration key containing the wal_level value
	ParameterWalLevel = "wal_level"

	// ParameterMaxWalSenders the configuration key containing the max_wal_senders value
	ParameterMaxWalSenders = "max_wal_senders"

	// ParameterArchiveMode the configuration key containing the archive_mode value
	ParameterArchiveMode = "archive_mode"

	// ParameterWalLogHints the configuration key containing the wal_log_hints value
	ParameterWalLogHints = "wal_log_hints"

	// ParameterRecoveryMinApplyDelay is the configuration key containing the recovery_min_apply_delay parameter
	ParameterRecoveryMinApplyDelay = "recovery_min_apply_delay"

	// ParameterSyncReplicationSlots the configuration key containing the sync_replication_slots value
	ParameterSyncReplicationSlots = "sync_replication_slots"

	// ParameterHotStandbyFeedback the configuration key containing the hot_standby_feedback value
	ParameterHotStandbyFeedback = "hot_standby_feedback"
)

// An acceptable wal_level value
const (
	WalLevelValueLogical WalLevelValue = "logical"
	WalLevelValueReplica WalLevelValue = "replica"
	WalLevelValueMinimal WalLevelValue = "minimal"
)

// IsKnownValue returns a bool indicating if the contained value is a well-know value
func (w WalLevelValue) IsKnownValue() bool {
	switch w {
	case WalLevelValueLogical, WalLevelValueReplica, WalLevelValueMinimal:
		return true
	default:
		return false
	}
}

// IsStricterThanMinimal returns a boolean indicating if the contained value is stricter than the minimal
// wal_level
func (w WalLevelValue) IsStricterThanMinimal() bool {
	switch w {
	case WalLevelValueLogical, WalLevelValueReplica:
		return true
	default:
		return false
	}
}

const (
	// hbaTemplateString is the template used to generate the pg_hba.conf
	// configuration file
	hbaTemplateString = `
#
# FIXED RULES
#

# Grant local access ('local' user map)
local all all peer map=local

# Require client certificate authentication for the streaming_replica user
hostssl postgres streaming_replica all cert map=cnpg_streaming_replica
hostssl replication streaming_replica all cert map=cnpg_streaming_replica
hostssl all cnpg_pooler_pgbouncer all cert map=cnpg_pooler_pgbouncer

#
# USER-DEFINED RULES
#

{{ range $rule := .UserRules }}
{{ $rule -}}
{{ end }}

{{ if .LDAPConfiguration }}
#
# LDAP CONFIGURATION (optional)
#
{{.LDAPConfiguration}}
{{ end }}

#
# DEFAULT RULES
#
host all all all {{.DefaultAuthenticationMethod}}
`

	// identTemplateString is the template used to generate the pg_ident.conf
	// configuration file
	identTemplateString = `
#
# FIXED RULES
#

# Grant local access ('local' user map)
local {{.Username}} postgres

# Grant streaming_replica access ('cnpg_streaming_replica' user map)
cnpg_streaming_replica streaming_replica streaming_replica

# Grant cnpg_pooler_pgbouncer access ('cnpg_pooler_pgbouncer' user map)
cnpg_pooler_pgbouncer cnpg_pooler_pgbouncer cnpg_pooler_pgbouncer

#
# USER-DEFINED RULES
#

{{ range $rule := .Mappings }}
{{ $rule -}}
{{ end }}
`

	// fixedConfigurationParameter are the configuration parameters
	// whose value is managed by the operator and should not be changed
	// by the user
	fixedConfigurationParameter = "fixed"

	// blockedConfigurationParameter are the configuration parameters
	// whose value must not be changed from the default one for the
	// operator to work correctly
	blockedConfigurationParameter = "blocked"

	// ScratchDataDirectory is the directory to be used for scratch data
	ScratchDataDirectory = "/controller"

	// TemporaryDirectory is the directory that is used to create
	// temporary files, and configured as TMPDIR in PostgreSQL Pods
	TemporaryDirectory = "/controller/tmp"

	// SpoolDirectory is the directory where we spool the WAL files that
	// were pre-archived in parallel
	SpoolDirectory = ScratchDataDirectory + "/wal-archive-spool"

	// CertificatesDir location to store the certificates
	CertificatesDir = ScratchDataDirectory + "/certificates/"

	// ProjectedVolumeDirectory is the base directory to store ProjectedVolumeSource
	ProjectedVolumeDirectory = "/projected"

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

	// BarmanBackupEndpointCACertificateLocation is the location where the barman endpoint
	// CA certificate is stored
	BarmanBackupEndpointCACertificateLocation = CertificatesDir + BarmanBackupEndpointCACertificateFileName

	// BarmanBackupEndpointCACertificateFileName is the name of the file in which the barman endpoint
	// CA certificate for backups is stored
	BarmanBackupEndpointCACertificateFileName = "backup-" + BarmanEndpointCACertificateFileName

	// BarmanRestoreEndpointCACertificateLocation is the location where the barman endpoint
	// CA certificate is stored
	BarmanRestoreEndpointCACertificateLocation = CertificatesDir + BarmanRestoreEndpointCACertificateFileName

	// BarmanRestoreEndpointCACertificateFileName is the name of the file in which the barman endpoint
	// CA certificate for restores is stored
	BarmanRestoreEndpointCACertificateFileName = "restore-" + BarmanEndpointCACertificateFileName

	// BarmanEndpointCACertificateFileName is the name of the file in which the barman endpoint
	// CA certificate is stored
	BarmanEndpointCACertificateFileName = "barman-ca.crt"

	// BackupTemporaryDirectory provides a path to backup temporary files
	// needed in the recovery process
	BackupTemporaryDirectory = ScratchDataDirectory + "/backup"

	// RecoveryTemporaryDirectory provides a path to store temporary files
	// needed in the recovery process
	RecoveryTemporaryDirectory = ScratchDataDirectory + "/recovery"

	// SocketDirectory provides a path to store the Unix socket to be
	// used by the PostgreSQL server
	SocketDirectory = ScratchDataDirectory + "/run"

	// ServerPort is the port where the postmaster process will be listening.
	// It's also used in the naming of the Unix socket
	ServerPort = 5432

	// LogPath is the path of the folder used by the logging_collector
	LogPath = ScratchDataDirectory + "/log"

	// LogFileName is the name of the file produced by the logging_collector,
	// excluding the extension. The logging collector process will append
	// `.csv` and `.log` as needed.
	LogFileName = "postgres"

	// CNPGConfigSha256 is the parameter to be used to inject the sha256 of the
	// config in the custom.conf file
	CNPGConfigSha256 = "cnpg.config_sha256"

	// CNPGSynchronousStandbyNamesMetadata is used to inject inside PG the parameters
	// that were used to calculate synchronous_standby_names. With this data we're
	// able to know the actual settings without parsing back the
	// synchronous_standby_names GUC
	CNPGSynchronousStandbyNamesMetadata = "cnpg.synchronous_standby_names_metadata"

	// SharedPreloadLibraries shared preload libraries key in the config
	SharedPreloadLibraries = "shared_preload_libraries"

	// SynchronousStandbyNames is the postgresql parameter key for synchronous standbys
	SynchronousStandbyNames = "synchronous_standby_names"

	// ExtensionControlPath is the postgresql parameter key for extension_control_path
	ExtensionControlPath = "extension_control_path"

	// DynamicLibraryPath is the postgresql parameter key dynamic_library_path
	DynamicLibraryPath = "dynamic_library_path"

	// ExtensionsBaseDirectory is the base directory to store ImageVolume Extensions
	ExtensionsBaseDirectory = "/extensions"
)

// hbaTemplate is the template used to create the HBA configuration
var hbaTemplate = template.Must(template.New("pg_hba.conf").Parse(hbaTemplateString))

// identTemplate is the template used to create the HBA configuration
var identTemplate = template.Must(template.New("pg_ident.conf").Parse(identTemplateString))

// MajorVersionRange represents a range of PostgreSQL major versions.
type MajorVersionRange struct {
	// Min is the inclusive lower bound of the PostgreSQL major version range.
	Min int

	// Max is the exclusive upper bound of the PostgreSQL major version range.
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

// SynchronousStandbyNamesConfig is the parameters that are needed
// to create the synchronous_standby_names GUC
type SynchronousStandbyNamesConfig struct {
	// Method accepts 'any' (quorum-based synchronous replication)
	// or 'first' (priority-based synchronous replication) as values.
	Method string `json:"method"`

	// NumSync is the number of synchronous standbys that transactions
	// need to wait for replies from
	NumSync int `json:"number"`

	// StandbyNames is the list of standby servers
	StandbyNames []string `json:"standbyNames"`
}

// ConfigurationInfo contains the required information to create a PostgreSQL
// configuration
type ConfigurationInfo struct {
	// The name of this cluster
	ClusterName string

	// The database settings to be used
	Settings ConfigurationSettings

	// The PostgreSQL version
	MajorVersion int

	// The list of user-level settings
	UserSettings map[string]string

	// The synchronous_standby_names configuration to be applied
	SynchronousStandbyNames SynchronousStandbyNamesConfig

	// The synchronized_standby_slots configuration to be applied
	SynchronizedStandbySlots []string

	// List of additional sharedPreloadLibraries to be loaded
	AdditionalSharedPreloadLibraries []string

	// Whether we need to include mandatory settings that are
	// not meant to be seen by users. Should be set to
	// true only when writing the configuration to disk
	IncludingMandatory bool

	// Whether we preserve user settings even when they are fixed parameters.
	// This setting is ignored if IncludingMandatory is true.
	// This should be set to true only in the defaulting webhook,
	// to allow the validating webhook to return an error
	PreserveFixedSettingsFromUser bool

	// If the generated configuration should contain shared_preload_libraries too or no
	IncludingSharedPreloadLibraries bool

	// Is this a replica cluster?
	IsReplicaCluster bool

	// TemporaryTablespaces is the list of temporary tablespaces
	TemporaryTablespaces []string

	// IsWalArchivingDisabled is true when user requested to disable WAL archiving
	IsWalArchivingDisabled bool

	// IsAlterSystemEnabled is true when 'allow_alter_system' should be set to on
	IsAlterSystemEnabled bool

	// Minimum apply delay of transaction
	RecoveryMinApplyDelay time.Duration

	// The list of additional extensions to be loaded into the PostgreSQL configuration
	AdditionalExtensions []AdditionalExtensionConfiguration
}

// getAlterSystemEnabledValue returns a config compatible value for IsAlterSystemEnabled
func (c ConfigurationInfo) getAlterSystemEnabledValue() string {
	if c.IsAlterSystemEnabled {
		return "on"
	}

	return "off"
}

// ManagedExtension defines all the information about a managed extension
type ManagedExtension struct {
	// Name of the extension
	Name string

	// Namespaces contains the configuration namespaces handled by the extension
	Namespaces []string

	// SharedPreloadLibraries is the list of needed shared preload libraries
	SharedPreloadLibraries []string

	// SkipCreateExtension is true when the extension is made only from a shared preload library
	SkipCreateExtension bool
}

// IsUsed checks whether a configuration namespace in the extension namespaces list
// is used in the user-provided configuration
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

// IsManagedExtensionUsed checks whether a configuration namespace in the named extension namespaces list
// is used in the user-provided configuration
func IsManagedExtensionUsed(name string, userConfigs map[string]string) bool {
	var extension *ManagedExtension
	for _, ext := range ManagedExtensions {
		if ext.Name == name {
			extension = &ext
			break
		}
	}
	if extension == nil {
		return false
	}

	return extension.IsUsed(userConfigs)
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
		{
			Name:                   "pg_failover_slots",
			SkipCreateExtension:    true,
			Namespaces:             []string{"pg_failover_slots"},
			SharedPreloadLibraries: []string{"pg_failover_slots"},
		},
	}

	// FixedConfigurationParameters contains the parameters that can't be
	// changed by the user
	FixedConfigurationParameters = map[string]string{
		// The following parameters need a restart to be applied
		"allow_system_table_mods":   blockedConfigurationParameter,
		"archive_mode":              fixedConfigurationParameter,
		"bonjour":                   blockedConfigurationParameter,
		"bonjour_name":              blockedConfigurationParameter,
		"cluster_name":              fixedConfigurationParameter,
		"config_file":               blockedConfigurationParameter,
		"data_directory":            blockedConfigurationParameter,
		"data_sync_retry":           blockedConfigurationParameter,
		"event_source":              blockedConfigurationParameter,
		"external_pid_file":         blockedConfigurationParameter,
		"hba_file":                  blockedConfigurationParameter,
		"hot_standby":               blockedConfigurationParameter,
		"ident_file":                blockedConfigurationParameter,
		"jit_provider":              blockedConfigurationParameter,
		"listen_addresses":          blockedConfigurationParameter,
		"logging_collector":         blockedConfigurationParameter,
		"port":                      fixedConfigurationParameter,
		"primary_conninfo":          fixedConfigurationParameter,
		"primary_slot_name":         fixedConfigurationParameter,
		"recovery_target":           fixedConfigurationParameter,
		"recovery_target_action":    fixedConfigurationParameter,
		"recovery_target_inclusive": fixedConfigurationParameter,
		"recovery_target_lsn":       fixedConfigurationParameter,
		"recovery_target_name":      fixedConfigurationParameter,
		"recovery_target_time":      fixedConfigurationParameter,
		"recovery_target_timeline":  fixedConfigurationParameter,
		"recovery_target_xid":       fixedConfigurationParameter,
		"restore_command":           fixedConfigurationParameter,
		"shared_preload_libraries":  fixedConfigurationParameter,
		"temp_tablespaces":          fixedConfigurationParameter,
		"unix_socket_directories":   blockedConfigurationParameter,
		"unix_socket_group":         blockedConfigurationParameter,
		"unix_socket_permissions":   blockedConfigurationParameter,

		// The following parameters need a reload to be applied
		"archive_cleanup_command":                blockedConfigurationParameter,
		"archive_command":                        fixedConfigurationParameter,
		"log_destination":                        blockedConfigurationParameter,
		"log_directory":                          blockedConfigurationParameter,
		"log_file_mode":                          blockedConfigurationParameter,
		"log_filename":                           blockedConfigurationParameter,
		"log_rotation_age":                       blockedConfigurationParameter,
		"log_rotation_size":                      blockedConfigurationParameter,
		"log_truncate_on_rotation":               blockedConfigurationParameter,
		"pg_failover_slots.primary_dsn":          fixedConfigurationParameter,
		"pg_failover_slots.standby_slot_names":   fixedConfigurationParameter,
		"promote_trigger_file":                   blockedConfigurationParameter,
		"recovery_end_command":                   blockedConfigurationParameter,
		"recovery_min_apply_delay":               blockedConfigurationParameter,
		"restart_after_crash":                    blockedConfigurationParameter,
		"ssl":                                    fixedConfigurationParameter,
		"ssl_ca_file":                            fixedConfigurationParameter,
		"ssl_cert_file":                          fixedConfigurationParameter,
		"ssl_crl_file":                           fixedConfigurationParameter,
		"ssl_dh_params_file":                     fixedConfigurationParameter,
		"ssl_ecdh_curve":                         fixedConfigurationParameter,
		"ssl_key_file":                           fixedConfigurationParameter,
		"ssl_passphrase_command":                 fixedConfigurationParameter,
		"ssl_passphrase_command_supports_reload": fixedConfigurationParameter,
		"ssl_prefer_server_ciphers":              fixedConfigurationParameter,
		"stats_temp_directory":                   blockedConfigurationParameter,
		"synchronous_standby_names":              fixedConfigurationParameter,
		"synchronized_standby_slots":             fixedConfigurationParameter,
		"syslog_facility":                        blockedConfigurationParameter,
		"syslog_ident":                           blockedConfigurationParameter,
		"syslog_sequence_numbers":                blockedConfigurationParameter,
		"syslog_split_messages":                  blockedConfigurationParameter,
	}

	// CnpgConfigurationSettings contains the settings that represent the
	// default and the mandatory behavior of CNP
	CnpgConfigurationSettings = ConfigurationSettings{
		GlobalDefaultSettings: SettingsCollection{
			"archive_timeout":            "5min",
			"dynamic_shared_memory_type": "posix",
			"full_page_writes":           "on",
			"logging_collector":          "on",
			"log_destination":            "csvlog",
			"log_directory":              LogPath,
			"log_filename":               LogFileName,
			"log_rotation_age":           "0",
			"log_rotation_size":          "0",
			"log_truncate_on_rotation":   "false",
			"max_parallel_workers":       "32",
			"max_worker_processes":       "32",
			"max_replication_slots":      "32",
			"shared_memory_type":         "mmap",
			"ssl_max_protocol_version":   "TLSv1.3",
			"ssl_min_protocol_version":   "TLSv1.3",
			"wal_keep_size":              "512MB",
			"wal_level":                  "logical",
			ParameterWalLogHints:         "on",
			"wal_sender_timeout":         "5s",
			"wal_receiver_timeout":       "5s",
			// Workaround for PostgreSQL not behaving correctly when
			// a default value is not explicit in the postgresql.conf and
			// the parameter cannot be changed without a restart.
			SharedPreloadLibraries: "",
		},
		MandatorySettings: SettingsCollection{
			"archive_command": fmt.Sprintf(
				"/controller/manager wal-archive --log-destination %s/%s.json %%p",
				LogPath, LogFileName),
			"hot_standby":             "true",
			"listen_addresses":        "*",
			"port":                    fmt.Sprint(ServerPort),
			"restart_after_crash":     "false",
			"ssl":                     "on",
			"ssl_cert_file":           ServerCertificateLocation,
			"ssl_key_file":            ServerKeyLocation,
			"ssl_ca_file":             ClientCACertificateLocation,
			"unix_socket_directories": SocketDirectory,
		},
	}
)

// CreateHBARules will create the content of pg_hba.conf file given
// the rules set by the cluster spec
func CreateHBARules(
	hba []string,
	defaultAuthenticationMethod, ldapConfigString string,
) (string, error) {
	var hbaContent bytes.Buffer

	templateData := struct {
		UserRules                   []string
		LDAPConfiguration           string
		DefaultAuthenticationMethod string
	}{
		UserRules:                   hba,
		LDAPConfiguration:           ldapConfigString,
		DefaultAuthenticationMethod: defaultAuthenticationMethod,
	}

	if err := hbaTemplate.Execute(&hbaContent, templateData); err != nil {
		return "", err
	}

	return hbaContent.String(), nil
}

// CreateIdentRules will create the content of pg_ident.conf file given
// the rules set by the cluster spec
func CreateIdentRules(ident []string, username string) (string, error) {
	var identContent bytes.Buffer

	templateData := struct {
		Mappings []string
		Username string
	}{
		Mappings: ident,
		Username: username,
	}

	if err := identTemplate.Execute(&identContent, templateData); err != nil {
		return "", err
	}

	return identContent.String(), nil
}

// PgConfiguration wraps configuration parameters with some checks
type PgConfiguration struct {
	configs map[string]string
}

// GetConfigurationParameters returns the generated configuration parameters
func (p *PgConfiguration) GetConfigurationParameters() map[string]string {
	return p.configs
}

// SetConfigurationParameters sets the configuration parameters
func (p *PgConfiguration) SetConfigurationParameters(configs map[string]string) {
	p.configs = configs
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

	ignoreFixedSettingsFromUser := info.IncludingMandatory || !info.PreserveFixedSettingsFromUser

	// Set all the default settings
	configuration.setDefaultConfigurations(info)

	// Apply all the values from the user, overriding defaults,
	// ignoring those which are fixed if ignoreFixedSettingsFromUser is true
	for key, value := range info.UserSettings {
		_, isFixed := FixedConfigurationParameters[key]
		if isFixed && ignoreFixedSettingsFromUser {
			continue
		}
		configuration.OverwriteConfig(key, value)
	}

	// Apply all mandatory settings, on top of defaults and user settings
	if info.IncludingMandatory {
		for key, value := range info.Settings.MandatorySettings {
			configuration.OverwriteConfig(key, value)
		}

		if info.MajorVersion >= 17 {
			configuration.OverwriteConfig("allow_alter_system", info.getAlterSystemEnabledValue())
		}
	}

	// Apply the correct archive_mode
	switch {
	case info.IsWalArchivingDisabled:
		configuration.OverwriteConfig("archive_mode", "off")

	case info.IsReplicaCluster:
		configuration.OverwriteConfig("archive_mode", "always")

	default:
		configuration.OverwriteConfig("archive_mode", "on")
	}

	// Apply the synchronous replication settings
	syncStandbyNames := info.SynchronousStandbyNames.String()
	if len(syncStandbyNames) > 0 {
		configuration.OverwriteConfig(SynchronousStandbyNames, syncStandbyNames)

		if metadata, err := json.Marshal(info.SynchronousStandbyNames); err != nil {
			log.Error(err,
				"Error while serializing streaming configuration parameters",
				"synchronousStandbyNames", info.SynchronousStandbyNames)
		} else {
			configuration.OverwriteConfig(CNPGSynchronousStandbyNamesMetadata, string(metadata))
		}
	}

	if len(info.SynchronizedStandbySlots) > 0 {
		synchronizedStandbySlots := strings.Join(info.SynchronizedStandbySlots, ",")
		if IsManagedExtensionUsed("pg_failover_slots", info.UserSettings) {
			configuration.OverwriteConfig("pg_failover_slots.standby_slot_names", synchronizedStandbySlots)
		}

		if info.MajorVersion >= 17 {
			if isEnabled, _ := ParsePostgresConfigBoolean(info.UserSettings["sync_replication_slots"]); isEnabled {
				configuration.OverwriteConfig("synchronized_standby_slots", synchronizedStandbySlots)
			}
		}
	}

	if info.ClusterName != "" {
		configuration.OverwriteConfig("cluster_name", info.ClusterName)
	}

	// Apply the replication delay
	if info.RecoveryMinApplyDelay != 0 {
		// We set recovery_min_apply_delay on every instance
		// of a replica cluster and not just on the primary.
		// PostgreSQL will look at the difference between the
		// current timestamp and the timestamp when the commit
		// was created (by the primary instance).
		//
		// Since both timestamps are the same on the designed
		// primary and on the replicas, setting it on both
		// is a safe approach.
		configuration.OverwriteConfig(
			ParameterRecoveryMinApplyDelay,
			fmt.Sprintf("%vs", math.Floor(info.RecoveryMinApplyDelay.Seconds())))
	}

	if info.IncludingSharedPreloadLibraries {
		// Set all managed shared preload libraries
		configuration.setManagedSharedPreloadLibraries(info)

		// Set all user provided shared preload libraries
		configuration.setUserSharedPreloadLibraries(info)
	}

	// Apply the list of temporary tablespaces
	if len(info.TemporaryTablespaces) > 0 {
		configuration.OverwriteConfig("temp_tablespaces", strings.Join(info.TemporaryTablespaces, ","))
	}

	// Setup additional extensions
	if len(info.AdditionalExtensions) > 0 {
		configuration.setExtensionControlPath(info)
		configuration.setDynamicLibraryPath(info)
	}

	return configuration
}

// setDefaultConfigurations sets all default configurations into the configuration map
// from the provided info
func (p *PgConfiguration) setDefaultConfigurations(info ConfigurationInfo) {
	// start from the global default settings
	for key, value := range info.Settings.GlobalDefaultSettings {
		p.OverwriteConfig(key, value)
	}

	// apply settings relative to a certain PostgreSQL version
	for constraints, settings := range info.Settings.DefaultSettings {
		if constraints.Min <= info.MajorVersion && info.MajorVersion < constraints.Max {
			for key, value := range settings {
				p.OverwriteConfig(key, value)
			}
		}
	}
}

// setManagedSharedPreloadLibraries sets all additional preloaded libraries
func (p *PgConfiguration) setManagedSharedPreloadLibraries(info ConfigurationInfo) {
	for _, extension := range ManagedExtensions {
		if extension.IsUsed(info.UserSettings) {
			for _, library := range extension.SharedPreloadLibraries {
				p.AddSharedPreloadLibrary(library)
			}
		}
	}
}

// setUserSharedPreloadLibraries sets all additional preloaded libraries.
// The resulting list will have all the user provided libraries, followed by all the ones managed
// by the operator, removing any duplicate and keeping the first occurrence in case of duplicates.
// Therefore the user provided order is preserved, if an overlap (with the ones already present) happens
func (p *PgConfiguration) setUserSharedPreloadLibraries(info ConfigurationInfo) {
	oldLibraries := strings.Split(p.GetConfig(SharedPreloadLibraries), ",")
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
		p.OverwriteConfig(SharedPreloadLibraries, strings.Join(libraries, ","))
	}
}

// CreatePostgresqlConfFile creates the contents of the postgresql.conf file
func CreatePostgresqlConfFile(configuration *PgConfiguration) (string, string) {
	// We need to be able to compare two configurations generated
	// by operator to know if they are different or not. To do
	// that we sort the configuration by parameter name as order
	// is really irrelevant for our purposes
	parameters := configuration.GetSortedList()
	var postgresConf strings.Builder
	var cnpgConf strings.Builder
	for _, parameter := range parameters {
		line := fmt.Sprintf(
			"%v = %v\n",
			parameter,
			escapePostgresConfValue(configuration.configs[parameter]))
		if strings.HasPrefix(parameter, "cnpg.") {
			cnpgConf.WriteString(line)
		} else {
			postgresConf.WriteString(line)
		}
	}

	sha256sum := fmt.Sprintf("%x", sha256.Sum256([]byte(postgresConf.String())))
	postgresConf.WriteString(cnpgConf.String())
	fmt.Fprintf(&postgresConf, "%v = %v\n", CNPGConfigSha256,
		escapePostgresConfValue(sha256sum))

	return postgresConf.String(), sha256sum
}

// escapePostgresConfValue escapes a value to make its representation
// directly embeddable in the PostgreSQL configuration file
func escapePostgresConfValue(value string) string {
	return fmt.Sprintf("'%v'", strings.ReplaceAll(value, "'", "''"))
}

// AdditionalExtensionConfiguration is the configuration for an Extension added via ImageVolume
type AdditionalExtensionConfiguration struct {
	// The name of the Extension
	Name string

	// The list of directories that should be added to ExtensionControlPath.
	ExtensionControlPath []string

	// The list of directories that should be added to DynamicLibraryPath.
	DynamicLibraryPath []string
}

// absolutizePaths returns an iterator over the passed paths, absolutized
// using the name of the extension
func (ext *AdditionalExtensionConfiguration) absolutizePaths(paths []string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, path := range paths {
			if !yield(filepath.Join(ExtensionsBaseDirectory, ext.Name, path)) {
				break
			}
		}
	}
}

// getRuntimeExtensionControlPath collects the absolute directories to be put
// into the `extension_control_path` GUC to support this additional extension
func (ext *AdditionalExtensionConfiguration) getRuntimeExtensionControlPath() iter.Seq[string] {
	paths := []string{"share"}
	if len(ext.ExtensionControlPath) > 0 {
		paths = ext.ExtensionControlPath
	}

	return ext.absolutizePaths(paths)
}

// getDynamicLibraryPath collects the absolute directories to be put
// into the `dynamic_library_path` GUC to support this additional extension
func (ext *AdditionalExtensionConfiguration) getDynamicLibraryPath() iter.Seq[string] {
	paths := []string{"lib"}
	if len(ext.DynamicLibraryPath) > 0 {
		paths = ext.DynamicLibraryPath
	}

	return ext.absolutizePaths(paths)
}

// setExtensionControlPath manages the `extension_control_path` GUC, merging
// the paths defined by the user with the ones provided by the
// `.spec.postgresql.extensions` stanza
func (p *PgConfiguration) setExtensionControlPath(info ConfigurationInfo) {
	extensionControlPath := []string{"$system"}

	for _, extension := range info.AdditionalExtensions {
		extensionControlPath = slices.AppendSeq(
			extensionControlPath,
			extension.getRuntimeExtensionControlPath(),
		)
	}

	extensionControlPath = slices.AppendSeq(
		extensionControlPath,
		strings.SplitSeq(p.GetConfig(ExtensionControlPath), ":"),
	)

	extensionControlPath = slices.DeleteFunc(
		extensionControlPath,
		func(s string) bool { return s == "" },
	)

	p.OverwriteConfig(ExtensionControlPath, strings.Join(extensionControlPath, ":"))
}

// setDynamicLibraryPath manages the `dynamic_library_path` GUC, merging the
// paths defined by the user with the ones provided by the
// `.spec.postgresql.extensions` stanza
func (p *PgConfiguration) setDynamicLibraryPath(info ConfigurationInfo) {
	dynamicLibraryPath := []string{"$libdir"}

	for _, extension := range info.AdditionalExtensions {
		dynamicLibraryPath = slices.AppendSeq(
			dynamicLibraryPath,
			extension.getDynamicLibraryPath())
	}

	dynamicLibraryPath = slices.AppendSeq(
		dynamicLibraryPath,
		strings.SplitSeq(p.GetConfig(DynamicLibraryPath), ":"))

	dynamicLibraryPath = slices.DeleteFunc(
		dynamicLibraryPath,
		func(s string) bool { return s == "" },
	)

	p.OverwriteConfig(DynamicLibraryPath, strings.Join(dynamicLibraryPath, ":"))
}

// String creates the synchronous_standby_names PostgreSQL GUC
// with the passed members
func (s *SynchronousStandbyNamesConfig) String() string {
	if s.IsZero() {
		return ""
	}

	escapePostgresConfLiteral := func(value string) string {
		return fmt.Sprintf("\"%v\"", strings.ReplaceAll(value, "\"", "\"\""))
	}

	escapedReplicas := make([]string, len(s.StandbyNames))
	for idx, name := range s.StandbyNames {
		escapedReplicas[idx] = escapePostgresConfLiteral(name)
	}

	return fmt.Sprintf(
		"%s %v (%v)",
		s.Method,
		s.NumSync,
		strings.Join(escapedReplicas, ","))
}

// IsZero is true when synchronour replication is disabled
func (s SynchronousStandbyNamesConfig) IsZero() bool {
	return len(s.StandbyNames) == 0
}
