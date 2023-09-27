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

package utils

import (
	"strings"

	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// GetCurrentTimestamp getting current time stamp from postgres server
func GetCurrentTimestamp(namespace, clusterName string, env *TestingEnvironment, podName *corev1.Pod) (string, error) {
	host, err := GetHostName(namespace, clusterName, env)
	if err != nil {
		return "", err
	}
	appUser, appUserPass, err := GetCredentials(clusterName, namespace, apiv1.ApplicationUserSecretSuffix, env)
	if err != nil {
		return "", err
	}
	query := "select TO_CHAR(CURRENT_TIMESTAMP,'YYYY-MM-DD HH24:MI:SS.US');"
	stdOut, _, err := RunQueryFromPod(
		podName,
		host,
		AppDBName,
		appUser,
		appUserPass,
		query,
		env,
	)
	if err != nil {
		return "", err
	}
	currentTimestamp := strings.Trim(stdOut, "\n")
	return currentTimestamp, nil
}
