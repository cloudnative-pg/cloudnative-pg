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

package config

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// databaseEntry represents a rendered database entry for the pgbouncer.ini [databases] section
type databaseEntry struct {
	Name   string
	Config string
}

const (
	// ConfigsDir is the directory in which all pgbouncer configurations are
	ConfigsDir = postgres.ScratchDataDirectory + "/configs"

	// ServerTLSCAPath is the path where the server CA is stored
	serverTLSCAPath = ConfigsDir + "/server-tls/ca.crt"

	// ClientTLSCertPath is the path where the client TLS certificate
	// is stored
	clientTLSCertPath = ConfigsDir + "/client-tls/tls.crt"

	// ClientTLSKeyPath is the path where the client TLS private key
	// is stored
	clientTLSKeyPath = ConfigsDir + "/client-tls/tls.key"

	// ServerTLSCertPath is the path where the server TLS certificate
	// is stored
	serverTLSCertPath = ConfigsDir + "/server-tls/tls.crt"

	// ServerTLSKeyPath is the path where the server TLS private key
	// is stored
	serverTLSKeyPath = ConfigsDir + "/server-tls/tls.key"

	// ClientTLSCAPath is the path where the public key of the CA
	// used to authenticate clients is stored
	clientTLSCAPath = ConfigsDir + "/client-ca/ca.crt"

	ignoreStartupParametersKey = "ignore_startup_parameters"
	authUserCrtPath            = ConfigsDir + "/authUser/tls.crt"
	authUserKeyPath            = ConfigsDir + "/authUser/tls.key"
	authFilePath               = ConfigsDir + "/userlist.txt"

	// PgBouncerIniFileName is the name of PgBouncer configuration file
	PgBouncerIniFileName = "pgbouncer.ini"
	// PgBouncerHBAConfFileName is the name of PgBouncer Host Based Authentication file
	PgBouncerHBAConfFileName = "pg_hba.conf"
	// PgBouncerUserListFileName is the name of PgBouncer user list file
	PgBouncerUserListFileName = "userlist.txt"
	// PgBouncerAdminUser is the default admin user for pgbouncer
	PgBouncerAdminUser = "pgbouncer"
	// PgBouncerSocketDir is the directory in which pgbouncer socket is
	PgBouncerSocketDir = postgres.SocketDirectory
	// PgBouncerPort is the port where pgbouncer will be listening
	PgBouncerPort = 5432
	// PgBouncerPortName is the name of the port where pgbouncer will be listening
	PgBouncerPortName = "pgbouncer"

	pgBouncerIniTemplateString = `
[databases]
{{ .DatabaseEntries }}
[pgbouncer]
pool_mode = {{ .Pooler.Spec.PgBouncer.PoolMode }}
auth_user = {{ .AuthQueryUser }}
auth_query = {{ .AuthQuery }}
auth_dbname = {{ .AuthDBName }}

{{ .Parameters -}}
`
	pgbouncerHBAFileTemplateString = `
local pgbouncer pgbouncer peer

{{ range $rule := .PgHba }}
{{ $rule -}}
{{ end }}

host all all 0.0.0.0/0 md5
host all all ::/0 md5
`

	pgBouncerUserListTemplateString = `
"{{ .AuthQueryUser }}" "{{ .AuthQueryPassword }}"
`
)

var (
	pgBouncerIniTemplate = template.Must(
		template.New(PgBouncerIniFileName).Parse(pgBouncerIniTemplateString))
	pgBouncerUserListTemplate = template.Must(
		template.New(PgBouncerUserListFileName).Parse(pgBouncerUserListTemplateString))
	pgBouncerHBATemplate = template.Must(
		template.New(PgBouncerHBAConfFileName).Parse(pgbouncerHBAFileTemplateString))

	// the PgBouncer parameters we want to have a default different from the default one
	defaultPgBouncerParameters = map[string]string{
		"log_stats":          "0",
		"auth_type":          "hba",
		"client_tls_sslmode": "prefer",
		"server_tls_sslmode": "verify-ca",
		// We are going to append these ignore_startup_parameters to the ones provided by the user,
		// as we need them to be able to connect using libpq.
		// See: https://github.com/lib/pq/issues/475
		ignoreStartupParametersKey: "extra_float_digits,options",
	}

	// The PgBouncer parameters we want to be enforced
	forcedPgBouncerParameters = map[string]string{
		"unix_socket_dir":      PgBouncerSocketDir,
		"listen_port":          "5432",
		"listen_addr":          "*",
		"admin_users":          PgBouncerAdminUser,
		"auth_hba_file":        ConfigsDir + "/pg_hba.conf",
		"server_tls_ca_file":   serverTLSCAPath,
		"client_tls_cert_file": clientTLSCertPath,
		"client_tls_key_file":  clientTLSKeyPath,
		"client_tls_ca_file":   clientTLSCAPath,
	}
)

// BuildConfigurationFiles create the config files containing the pgbouncer configuration and
// the users file
func BuildConfigurationFiles(pooler *apiv1.Pooler, secrets *Secrets) (ConfigurationFiles, error) {
	files := make(map[string][]byte)
	var pgbouncerIni bytes.Buffer
	var pgbouncerUserList bytes.Buffer
	var pgbouncerHBA bytes.Buffer

	var authQueryUser, authQueryPassword string
	var isCertAuth bool

	// if no user is provided we have to check the secret for a username, and we must be using basic auth
	// if a user is provided it will overwrite the user in the secret, or we could be using cert auth
	authQuerySecret := secrets.AuthQuery
	if authQuerySecret == nil {
		authQuerySecret = secrets.ServerTLS
	}

	if authQuerySecret != nil {
		authQuerySecretType, err := detectSecretType(authQuerySecret)
		if err != nil {
			return nil, fmt.Errorf("while detecting auth user secret type: %w", err)
		}

		switch authQuerySecretType {
		case corev1.SecretTypeBasicAuth:
			authQueryUser = string(authQuerySecret.Data["username"])
			authQueryPassword = strings.ReplaceAll(string(authQuerySecret.Data["password"]), "\"", "\"\"")

		case corev1.SecretTypeTLS:
			keyPair, err := certs.ParseServerSecret(authQuerySecret)
			if err != nil {
				return nil, fmt.Errorf("while parsing TLS secret for auth user: %w", err)
			}

			certificate, err := keyPair.ParseCertificate()
			if err != nil {
				return nil, fmt.Errorf("while parsing certificate for auth user: %w", err)
			}

			authQueryUser = certificate.Subject.CommonName
			isCertAuth = true
			files[authUserCrtPath] = authQuerySecret.Data[certs.TLSCertKey]
			files[authUserKeyPath] = authQuerySecret.Data[certs.TLSPrivateKeyKey]

		default:
			return nil, fmt.Errorf("unsupported secret type for auth query: %s", authQuerySecret.Type)
		}
	}

	parameters := buildPgBouncerParameters(pooler.Spec.PgBouncer.Parameters)

	if isCertAuth {
		parameters["server_tls_cert_file"] = authUserCrtPath
		parameters["server_tls_key_file"] = authUserKeyPath
	} else {
		parameters["auth_file"] = authFilePath
	}

	if secrets.ServerTLS != nil {
		parameters["server_tls_cert_file"] = serverTLSCertPath
		parameters["server_tls_key_file"] = serverTLSKeyPath
	}

	templateData := struct {
		Pooler            *apiv1.Pooler
		AuthQuery         string
		AuthQueryUser     string
		AuthQueryPassword string
		AuthDBName        string
		Parameters        string
		PgHba             []string
		DatabaseEntries   string
	}{
		Pooler:            pooler,
		AuthQuery:         pooler.GetAuthQuery(),
		AuthQueryUser:     authQueryUser,
		AuthQueryPassword: authQueryPassword,
		AuthDBName:        apiv1.PoolerAuthDBName,
		// We are not directly passing the map of parameters inside the template
		// because the iteration order of the entries inside a map is undefined
		// and this could lead to the secret being rewritten where isn't really
		// needed, leading to spurious rollouts of the Pods.
		//
		// Also, we want the list of parameters inside the PgBouncer configuration
		// to be stable.
		Parameters:      stringifyPgBouncerParameters(parameters),
		PgHba:           pooler.Spec.PgBouncer.PgHBA,
		DatabaseEntries: buildDatabaseEntries(pooler),
	}

	if err := pgBouncerIniTemplate.Execute(&pgbouncerIni, templateData); err != nil {
		return nil, fmt.Errorf("while executing %s template: %w", PgBouncerIniFileName, err)
	}
	files[filepath.Join(ConfigsDir, PgBouncerIniFileName)] = pgbouncerIni.Bytes()

	if !isCertAuth {
		err := pgBouncerUserListTemplate.Execute(&pgbouncerUserList, templateData)
		if err != nil {
			return nil, fmt.Errorf("while executing %s template: %w", PgBouncerUserListFileName, err)
		}
		files[filepath.Join(ConfigsDir, PgBouncerUserListFileName)] = pgbouncerUserList.Bytes()
	}

	if err := pgBouncerHBATemplate.Execute(&pgbouncerHBA, templateData); err != nil {
		return nil, fmt.Errorf("while executing %s template: %w", PgBouncerHBAConfFileName, err)
	}
	files[filepath.Join(ConfigsDir, PgBouncerHBAConfFileName)] = pgbouncerHBA.Bytes()

	// The required crypto-material
	files[serverTLSCAPath] = secrets.ServerCA.Data[certs.CACertKey]
	files[clientTLSCAPath] = secrets.ClientCA.Data[certs.CACertKey]
	files[clientTLSCertPath] = secrets.ClientTLS.Data[certs.TLSCertKey]
	files[clientTLSKeyPath] = secrets.ClientTLS.Data[certs.TLSPrivateKeyKey]

	if secrets.ServerTLS != nil {
		files[serverTLSCertPath] = secrets.ServerTLS.Data[certs.TLSCertKey]
		files[serverTLSKeyPath] = secrets.ServerTLS.Data[certs.TLSPrivateKeyKey]
	}

	return files, nil
}

// buildDatabaseEntries creates the [databases] section entries for pgbouncer.ini.
// A default wildcard entry is always added to ensure all databases can be accessed.
// The entries are sorted by database name to ensure stable configuration output,
// with the wildcard entry always appearing last.
func buildDatabaseEntries(pooler *apiv1.Pooler) string {
	defaultHost := fmt.Sprintf("%s-%s", pooler.Spec.Cluster.Name, pooler.Spec.Type)

	// Build entries for each configured database (skip any wildcard entries)
	entries := make([]databaseEntry, 0, len(pooler.Spec.PgBouncer.Databases)+1)
	for _, db := range pooler.Spec.PgBouncer.Databases {
		// Skip wildcard entries - the wildcard is always added automatically
		if db.Name == "*" {
			continue
		}
		entry := buildSingleDatabaseEntry(db, defaultHost)
		entries = append(entries, entry)
	}

	// Always add the default wildcard entry to ensure all databases can be accessed
	entries = append(entries, databaseEntry{
		Name:   "*",
		Config: fmt.Sprintf("host=%s", defaultHost),
	})

	// Sort entries by database name for stable output
	// Wildcards (*) should come last
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name == "*" {
			return false
		}
		if entries[j].Name == "*" {
			return true
		}
		return entries[i].Name < entries[j].Name
	})

	// Build the final string
	var result strings.Builder
	for _, entry := range entries {
		result.WriteString(entry.Name)
		result.WriteString(" = ")
		result.WriteString(entry.Config)
		result.WriteString("\n")
	}

	return result.String()
}

// buildSingleDatabaseEntry builds a single database entry configuration string.
// PgBouncer database entry format: dbname = host=X dbname=Y pool_mode=Z pool_size=N ...
func buildSingleDatabaseEntry(db apiv1.PgBouncerDatabaseConfig, defaultHost string) databaseEntry {
	var parts []string

	// Host configuration - always use the cluster service
	parts = append(parts, fmt.Sprintf("host=%s", defaultHost))

	// DBName - if different from the client-facing name
	if db.DBName != "" {
		parts = append(parts, fmt.Sprintf("dbname=%s", db.DBName))
	}

	// Pool mode override (explicit field takes precedence over parameters)
	if db.PoolMode != "" {
		parts = append(parts, fmt.Sprintf("pool_mode=%s", db.PoolMode))
	}

	// Additional parameters - sort keys for stable output
	if len(db.Parameters) > 0 {
		keys := make([]string, 0, len(db.Parameters))
		for k := range db.Parameters {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%s", k, db.Parameters[k]))
		}
	}

	return databaseEntry{
		Name:   db.Name,
		Config: strings.Join(parts, " "),
	}
}
