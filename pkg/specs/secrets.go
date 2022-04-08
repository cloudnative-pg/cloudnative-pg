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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
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
			"password": password,
			"pgpass": fmt.Sprintf(
				"%v:%v:%v:%v:%v\n",
				hostname,
				postgres.ServerPort,
				dbname,
				username,
				password),
		},
	}
}
