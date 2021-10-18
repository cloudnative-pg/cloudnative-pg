/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package pgbouncer

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/certs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

var (
	pgBouncerIniTemplate      = template.Must(template.New(pgbouncerIniKey).Parse(pgBouncerIniTemplateString))
	pgBouncerUserListTemplate = template.Must(template.New(pgbouncerUserListKey).Parse(pgBouncerUserListTemplateString))
	pgBouncerHBATemplate      = template.Must(template.New(pgbouncerHBAConfKey).Parse(pgbouncerHBAFileTemplateString))

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
		"listen_port":          "5432",
		"listen_addr":          "*",
		"admin_users":          PgBouncerAdminUser,
		"auth_type":            "hba",
		"auth_hba_file":        "/config/pg_hba.conf",
		"server_tls_sslmode":   "verify-ca",
		"server_tls_ca_file":   "/secret/ca/ca.crt",
		"client_tls_sslmode":   "prefer",
		"client_tls_cert_file": "/secret/server-tls/tls.crt",
		"client_tls_key_file":  "/secret/server-tls/tls.key",
		"client_tls_ca_file":   "/secret/ca/ca.crt",
	}

	// The following regexp will match any newline character. PgBouncer
	// doesn't admit newlines inside the configuration at all
	newlineRegexp = regexp.MustCompile(`\r\n|[\r\n\v\f\x{0085}\x{2028}\x{2029}]`)
)

const (
	ignoreStartupParametersKey = "ignore_startup_parameters"
	pgbouncerIniKey            = "pgbouncer.ini"
	pgbouncerHBAConfKey        = "pg_hba.conf"
	pgbouncerUserListKey       = "userlist.txt"
	// PgBouncerAdminUser is the default admin user for pgbouncer
	PgBouncerAdminUser = "pgbouncer"
	// PgBouncerSocketDir is the directory in which pgbouncer socket is
	PgBouncerSocketDir = postgres.SocketDirectory
	// PgBouncerPort is the port where pgbouncer will be listening
	PgBouncerPort = 5432

	pgBouncerIniTemplateString = `
[databases]
* = host={{.Pooler.Spec.Cluster.Name}}-{{.Pooler.Spec.Type}}

[pgbouncer]
unix_socket_dir = {{.SocketDir}}
pool_mode = {{.Pooler.Spec.PgBouncer.PoolMode}}
auth_user = {{.AuthQueryUser}}
auth_query = {{ .AuthQuery }}
{{ if .IsCertAuth -}}
server_tls_cert_file = /secret/authUser/tls.crt
server_tls_key_file = /secret/authUser/tls.key
{{ else -}}
auth_file = /config/userlist.txt
{{- end }}
{{ .Parameters -}}
`
	pgbouncerHBAFileTemplateString = `
local pgbouncer pgbouncer peer
host all all 0.0.0.0/0 md5
`

	pgBouncerUserListTemplateString = `
{{ if not .IsCertAuth -}}
"{{ .AuthQueryUser }}" "{{ .AuthQueryPassword }}"
{{ end -}}
`
)

// Secret create the secret containing the pgbouncer configuration and
// the users file
func Secret(pooler *apiv1.Pooler, authQuerySecret *corev1.Secret) (*corev1.Secret, error) {
	var pgbouncerIni bytes.Buffer
	var pgbouncerUserList bytes.Buffer
	var pgbouncerHBA bytes.Buffer

	var authQueryUser, authQueryPassword string
	var isCertAuth bool

	// if no user is provided we have to check the secret for a username, and we must be using basic auth
	// if a user is provided it will overwrite the user in the secret, or we could be using cert auth
	switch authQuerySecret.Type {
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
	default:
		return nil, fmt.Errorf("unsupported secret type for auth query: %s", authQuerySecret.Type)
	}

	templateData := struct {
		Pooler            *apiv1.Pooler
		AuthQuery         string
		AuthQueryUser     string
		AuthQueryPassword string
		IsCertAuth        bool
		Parameters        string
		AdminUsers        string
		SocketDir         string
	}{
		Pooler:            pooler,
		AuthQuery:         pooler.GetAuthQuery(),
		AuthQueryUser:     authQueryUser,
		AuthQueryPassword: authQueryPassword,
		IsCertAuth:        isCertAuth,
		// We are not directly passing the map of parameters inside the template
		// because the iteration order of the entries inside a map is undefined
		// and this could lead to the secret being rewritten where isn't really
		// needed, leading to spurious rollouts of the Pods.
		//
		// Also, we want the list of parameters inside the PgBouncer configuration
		// to be stable.
		Parameters: stringifyPgBouncerParameters(buildPgBouncerParameters(pooler.Spec.PgBouncer.Parameters)),
		AdminUsers: PgBouncerAdminUser,
		SocketDir:  PgBouncerSocketDir,
	}

	err := pgBouncerIniTemplate.Execute(&pgbouncerIni, templateData)
	if err != nil {
		return nil, fmt.Errorf("while executing %s template: %w", pgbouncerIniKey, err)
	}

	err = pgBouncerUserListTemplate.Execute(&pgbouncerUserList, templateData)
	if err != nil {
		return nil, fmt.Errorf("while executing %s template: %w", pgbouncerUserListKey, err)
	}

	err = pgBouncerHBATemplate.Execute(&pgbouncerHBA, templateData)
	if err != nil {
		return nil, fmt.Errorf("while executing %s template: %w", pgbouncerHBAConfKey, err)
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pooler.Name,
			Namespace: pooler.Namespace,
		},
		Data: map[string][]byte{
			pgbouncerIniKey:      pgbouncerIni.Bytes(),
			pgbouncerHBAConfKey:  pgbouncerHBA.Bytes(),
			pgbouncerUserListKey: pgbouncerUserList.Bytes(),
		},
	}, nil
}

// stringifyPgBouncerParameters will take map of PgBouncer parameters and emit
// the relative configuration. We are using a function instead of using the template
// because we want the order of the parameters to be stable to avoid doing rolling
// out new PgBouncer Pods when it's not really needed
func stringifyPgBouncerParameters(parameters map[string]string) (paramsString string) {
	keys := make([]string, 0, len(parameters))
	for k := range parameters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		paramsString += fmt.Sprintf("%s = %s\n", k, parameters[k])
	}
	return paramsString
}

// buildPgBouncerParameters will build a PgBouncer configuration applying any
// default parameters and forcing any required parameter needed for the
// controller to work correctly
func buildPgBouncerParameters(userParameters map[string]string) map[string]string {
	params := make(map[string]string, len(userParameters))

	for k, v := range userParameters {
		params[k] = cleanupPgBouncerValue(v)
	}

	for k, defaultValue := range defaultPgBouncerParameters {
		if userValue, ok := params[k]; ok {
			if k == ignoreStartupParametersKey {
				params[k] = strings.Join([]string{defaultValue, userValue}, ",")
			}
			continue
		}
		params[k] = defaultValue
	}

	for k, v := range forcedPgBouncerParameters {
		params[k] = v
	}

	return params
}

// cleanupPgBouncerValue removes any newline character from a configuration value.
// The parser used by libusual doesn't support that.
func cleanupPgBouncerValue(parameter string) (escaped string) {
	// See:
	// https://github.com/libusual/libusual/blob/master/usual/cfparser.c  //wokeignore:rule=master
	//
	// The PgBouncer ini file parser doesn't admit any newline character
	// so we are just removing from the value
	return newlineRegexp.ReplaceAllString(parameter, "")
}
