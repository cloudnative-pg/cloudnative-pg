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
	uri := &url.URL{
		Scheme: "postgresql",
		User:   url.UserPassword(username, password),
		Host:   fmt.Sprintf("%s:%d", hostname, postgres.ServerPort),
		Path:   dbname,
	}
	jdbc_uri := &url.URL{
		Scheme: "jdbc:postgresql",
		Host:   fmt.Sprintf("%s:%d", hostname, postgres.ServerPort),
		Path:   dbname,
	}
	q := jdbc_uri.Query()
	q.Set("user", username)
	q.Set("password", password)
	jdbc_uri.RawQuery = q.Encode()
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
			"uri":      uri.String(),
			"jdbc-uri": jdbc_uri.String(),
		},
	}
}
