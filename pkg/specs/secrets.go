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

package specs

import (
	"fmt"
	"net/url"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// CreateSecret create a secret with the PostgreSQL and the owner passwords
func CreateSecret(
	name string,
	namespace string,
	hostname string,
	dbname string,
	username string,
	password string,
) *corev1.Secret {
	uriBuilder := newConnectionStringBuilder(hostname, dbname, username, password)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				WatchedLabelName: "true",
			},
		},
		Type: corev1.SecretTypeBasicAuth,
		StringData: map[string]string{
			"username": username,
			"user":     username,
			"password": password,
			"dbname":   dbname,
			"host":     hostname,
			"port":     fmt.Sprintf("%d", postgres.ServerPort),
			"pgpass": fmt.Sprintf(
				"%v:%v:%v:%v:%v\n",
				hostname,
				postgres.ServerPort,
				dbname,
				username,
				password),
			"uri":      uriBuilder.buildPostgres(),
			"jdbc-uri": uriBuilder.buildJdbc(),
		},
	}
}

type connectionStringBuilder struct {
	host     string
	dbname   string
	username string
	password string
}

func newConnectionStringBuilder(hostname, dbname, username, password string) *connectionStringBuilder {
	return &connectionStringBuilder{
		host:     fmt.Sprintf("%s:%d", hostname, postgres.ServerPort),
		dbname:   dbname,
		username: username,
		password: password,
	}
}

func (c connectionStringBuilder) buildPostgres() string {
	postgresURI := url.URL{
		Scheme: "postgresql",
		User:   url.UserPassword(c.username, c.password),
		Host:   c.host,
		Path:   c.dbname,
	}

	return postgresURI.String()
}

func (c connectionStringBuilder) buildJdbc() string {
	jdbcURI := &url.URL{
		Scheme: "jdbc:postgresql",
		Host:   c.host,
		Path:   c.dbname,
	}
	q := jdbcURI.Query()
	q.Set("user", c.username)
	q.Set("password", c.password)
	jdbcURI.RawQuery = q.Encode()
	return jdbcURI.String()
}
