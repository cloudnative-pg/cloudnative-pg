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

package config

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

const (
	// ConfigsDir is the directory in which all pgbouncer configurations are
	ConfigsDir = postgres.ScratchDataDirectory + "/configs"

	// ServerTLSCAPath is the path where the server CA is stored
	serverTLSCAPath = ConfigsDir + "/server-tls/ca.crt"

	// ClientTLSCertPath is the path where the client TLS certificate
	// is stored
	clientTLSCertPath = ConfigsDir + "/server-tls/tls.crt"

	// ClientTLSKeyPath is the path where the client TLS private key
	// is stored
	clientTLSKeyPath = ConfigsDir + "/server-tls/tls.key"

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
* = host={{.Pooler.Spec.Cluster.Name}}-{{.Pooler.Spec.Type}}

[pgbouncer]
pool_mode = {{ .Pooler.Spec.PgBouncer.PoolMode }}
auth_user = {{ .AuthQueryUser }}
auth_query = {{ .AuthQuery }}
auth_dbname = {{ .AuthQueryDb }}

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
		"log_stats": "0",
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
		"auth_type":            "hba",
		"auth_hba_file":        ConfigsDir + "/pg_hba.conf",
		"server_tls_sslmode":   "verify-ca",
		"server_tls_ca_file":   serverTLSCAPath,
		"client_tls_sslmode":   "prefer",
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
	authQuerySecretType, err := detectSecretType(secrets.AuthQuery)
	if err != nil {
		return nil, fmt.Errorf("while detecting auth user secret type: %w", err)
	}

	switch authQuerySecretType {
	case corev1.SecretTypeBasicAuth:
		authQueryUser = string(secrets.AuthQuery.Data["username"])
		authQueryPassword = strings.ReplaceAll(string(secrets.AuthQuery.Data["password"]), "\"", "\"\"")

	case corev1.SecretTypeTLS:
		keyPair, err := certs.ParseServerSecret(secrets.AuthQuery)
		if err != nil {
			return nil, fmt.Errorf("while parsing TLS secret for auth user: %w", err)
		}

		certificate, err := keyPair.ParseCertificate()
		if err != nil {
			return nil, fmt.Errorf("while parsing certificate for auth user: %w", err)
		}

		authQueryUser = certificate.Subject.CommonName
		isCertAuth = true
		files[authUserCrtPath] = secrets.AuthQuery.Data[certs.TLSCertKey]
		files[authUserKeyPath] = secrets.AuthQuery.Data[certs.TLSPrivateKeyKey]

	default:
		return nil, fmt.Errorf("unsupported secret type for auth query: %s", secrets.AuthQuery.Type)
	}

	parameters := buildPgBouncerParameters(pooler.Spec.PgBouncer.Parameters)

	if isCertAuth {
		parameters["server_tls_cert_file"] = authUserCrtPath
		parameters["server_tls_key_file"] = authUserKeyPath
	} else {
		parameters["auth_file"] = authFilePath
	}

	templateData := struct {
		Pooler            *apiv1.Pooler
		AuthQuery         string
		AuthQueryUser     string
		AuthQueryPassword string
		AuthQueryDb 	  string
		Parameters        string
		PgHba             []string
	}{
		Pooler:            pooler,
		AuthQuery:         pooler.GetAuthQuery(),
		AuthQueryUser:     authQueryUser,
		// TODO: control from `pooler.Spec.PgBouncer.AuthQueryDb`
		AuthQueryDb: 	   "postgres",
		AuthQueryPassword: authQueryPassword,
		// We are not directly passing the map of parameters inside the template
		// because the iteration order of the entries inside a map is undefined
		// and this could lead to the secret being rewritten where isn't really
		// needed, leading to spurious rollouts of the Pods.
		//
		// Also, we want the list of parameters inside the PgBouncer configuration
		// to be stable.
		Parameters: stringifyPgBouncerParameters(parameters),
		PgHba:      pooler.Spec.PgBouncer.PgHBA,
	}

	err = pgBouncerIniTemplate.Execute(&pgbouncerIni, templateData)
	if err != nil {
		return nil, fmt.Errorf("while executing %s template: %w", PgBouncerIniFileName, err)
	}
	files[filepath.Join(ConfigsDir, PgBouncerIniFileName)] = pgbouncerIni.Bytes()

	if !isCertAuth {
		err = pgBouncerUserListTemplate.Execute(&pgbouncerUserList, templateData)
		if err != nil {
			return nil, fmt.Errorf("while executing %s template: %w", PgBouncerUserListFileName, err)
		}
		files[filepath.Join(ConfigsDir, PgBouncerUserListFileName)] = pgbouncerUserList.Bytes()
	}

	err = pgBouncerHBATemplate.Execute(&pgbouncerHBA, templateData)
	if err != nil {
		return nil, fmt.Errorf("while executing %s template: %w", PgBouncerHBAConfFileName, err)
	}
	files[filepath.Join(ConfigsDir, PgBouncerHBAConfFileName)] = pgbouncerHBA.Bytes()

	// The required crypto-material
	files[serverTLSCAPath] = secrets.ServerCA.Data[certs.CACertKey]
	files[clientTLSCAPath] = secrets.ClientCA.Data[certs.CACertKey]
	files[clientTLSCertPath] = secrets.Client.Data[certs.TLSCertKey]
	files[clientTLSKeyPath] = secrets.Client.Data[certs.TLSPrivateKeyKey]

	return files, nil
}
