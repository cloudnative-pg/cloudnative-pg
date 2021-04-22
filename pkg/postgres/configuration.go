/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
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
hostssl postgres streaming_replica all cert clientcert=1
hostssl replication streaming_replica all cert clientcert=1
`

	// hbaFooter is the footer of generated pg_hba.conf.
	// The content provided by the user is inserted before this text
	hbaFooter = `
# Otherwise use md5 authentication
host all all all md5
`
	// fixedConfigurationParameter are the configuration parameters
	// whose value is managed by the operator and should not be changed
	// be the user
	fixedConfigurationParameter = "fixed"

	// blockedConfigurationParameter are the configuration parameters
	// whose value must not be changed from the default one for the
	// operator to work correctly
	blockedConfigurationParameter = "blocked"

	// ServerCertificateLocation is the location where the server certificate
	// is stored
	ServerCertificateLocation = "/tmp/server.crt"

	// ServerKeyLocation is the location where the private key is stored
	ServerKeyLocation = "/tmp/server.key"

	// StreamingReplicaCertificateLocation is the location where the certificate
	// of the "postgres" user is stored
	StreamingReplicaCertificateLocation = "/tmp/streaming_replica.crt"

	// StreamingReplicaKeyLocation is the location where the private key of
	// the "postgres" user is stored
	StreamingReplicaKeyLocation = "/tmp/streaming_replica.key"

	// CACertificateLocation is the location where the CA certificate
	// is stored, and this certificate will be use to authenticate
	// client certificates
	CACertificateLocation = "/tmp/ca.crt"
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
}

// ConfigurationInfo contains the required information to create a PostgreSQL
// configuration
type ConfigurationInfo struct {
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
}

var (
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
			"max_parallel_workers":  "32",
			"max_worker_processes":  "32",
			"max_replication_slots": "32",
			"logging_collector":     "off",
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
			"unix_socket_directories": "/var/run/postgresql",
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
			"ssl_ca_file":             CACertificateLocation,
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

// CreatePostgresqlConfiguration create the configuration from the settings
// and the default values
func CreatePostgresqlConfiguration(info ConfigurationInfo) map[string]string {
	// Start from scratch
	configuration := make(map[string]string)

	// start from the default settings
	for key, value := range info.Settings.GlobalDefaultSettings {
		configuration[key] = value
	}

	// apply settings relative to a certain PostgreSQL version
	for constraints, settings := range info.Settings.DefaultSettings {
		if constraints.Min == MajorVersionRangeUnlimited || (constraints.Min <= info.MajorVersion) {
			if constraints.Max == MajorVersionRangeUnlimited || (info.MajorVersion < constraints.Max) {
				for key, value := range settings {
					configuration[key] = value
				}
			}
		}
	}

	// apply the values from the user
	for key, value := range info.UserSettings {
		configuration[key] = value
	}

	// apply the mandatory settings
	if info.IncludingMandatory {
		for key, value := range info.Settings.MandatorySettings {
			configuration[key] = value
		}
	}

	// apply the list of replicas
	if info.Replicas != nil && info.SyncReplicas > 0 {
		escapedReplicas := make([]string, len(info.Replicas))
		for idx, name := range info.Replicas {
			escapedReplicas[idx] = escapePostgresConfLiteral(name)
		}
		configuration["synchronous_standby_names"] = fmt.Sprintf(
			"ANY %v (%v)",
			info.SyncReplicas,
			strings.Join(escapedReplicas, ","))
	}

	return configuration
}

// FillCNPConfiguration create the actual PostgreSQL configuration
// for CNP given the user settings and the major version. This is
// useful during the configuration validation
func FillCNPConfiguration(
	majorVersion int,
	userSettings map[string]string,
	includingMandatory bool,
) map[string]string {
	info := ConfigurationInfo{
		Settings:           CnpConfigurationSettings,
		MajorVersion:       majorVersion,
		UserSettings:       userSettings,
		IncludingMandatory: includingMandatory,
		Replicas:           nil,
	}
	return CreatePostgresqlConfiguration(info)
}

// CreatePostgresqlConfFile create the contents of the postgresql.conf file
func CreatePostgresqlConfFile(configuration map[string]string) string {
	// We need to be able to compare two configurations generated
	// by operator to know if they are different or not. To do
	// that we sort the configuration by parameter name as order
	// is really irrelevant for our purposes
	parameters := make([]string, len(configuration))
	i := 0
	for key := range configuration {
		parameters[i] = key
		i++
	}
	sort.Strings(parameters)

	postgresConf := ""
	for _, parameter := range parameters {
		postgresConf += fmt.Sprintf(
			"%v = %v\n",
			parameter,
			escapePostgresConfValue(configuration[parameter]))
	}
	return postgresConf
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
