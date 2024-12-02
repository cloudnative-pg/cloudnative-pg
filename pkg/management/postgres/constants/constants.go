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

// Package constants provides the needed constants in the postgres package
package constants

const (
	// PostgresqlCustomConfigurationFile is the name of the file containing
	// the main PostgreSQL configuration parameters directly managed by the
	// operator
	PostgresqlCustomConfigurationFile = "custom.conf"

	// PostgresqlOverrideConfigurationFile is the name of the file containing
	// the PostgreSQL configuration parameters that the operator need to
	// control to coordinate the activities in the cluster (normally they
	// contain HA and DR settings)
	PostgresqlOverrideConfigurationFile = "override.conf"

	// PostgresqlHBARulesFile is the name of the file which contains
	// the host-based access rules
	PostgresqlHBARulesFile = "pg_hba.conf"

	// PostgresqlIdentFile is the name of the file which contains
	// the user name maps
	PostgresqlIdentFile = "pg_ident.conf"

	// BackupLabelFile holds the content of BackupLabelFile. Used during a restore from a hot backup.
	BackupLabelFile = "backup_label"

	// TablespaceMapFile holds the content of TablespaceMapFile. Used during a restore from a hot backup.
	TablespaceMapFile = "tablespace_map"

	// InitdbName is the name of the command to initialize a PostgreSQL database
	InitdbName = "initdb"

	// WalArchiveCommand  is the name of the wal-archive command
	WalArchiveCommand = "wal-archive"

	// Startup is the name of a file that is created once during the first reconcile of an instance
	Startup = "cnpg_initialized"

	// CheckEmptyWalArchiveFile is the name of the file in the PGDATA that,
	// if present, requires the WAL archiver to check that the backup object
	// store is empty.
	CheckEmptyWalArchiveFile = ".check-empty-wal-archive"
)
