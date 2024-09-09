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
	"strconv"

	corev1 "k8s.io/api/core/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
)

// runProxyRequest makes a GET call on the pod interface proxy, and returns the raw response
func runProxyRequest(env *TestingEnvironment, pod *corev1.Pod, tlsEnabled bool, path string, port int) ([]byte, error) {
	portString := strconv.Itoa(port)

	schema := "http"
	if tlsEnabled {
		schema = "https"
	}

	req := env.Interface.CoreV1().Pods(pod.Namespace).ProxyGet(
		schema, pod.Name, portString, path, map[string]string{})

	return req.DoRaw(env.Ctx)
}

// RetrieveMetricsFromInstance aims to retrieve the metrics from a PostgreSQL instance pod
// using a GET request on the pod interface proxy
func RetrieveMetricsFromInstance(
	env *TestingEnvironment,
	pod corev1.Pod,
	tlsEnabled bool,
) (string, error) {
	body, err := runProxyRequest(env, &pod, tlsEnabled, url.PathMetrics, int(url.PostgresMetricsPort))
	return string(body), err
}

// RetrieveMetricsFromPgBouncer aims to retrieve the metrics from a PgBouncer pod
// using a GET request on the pod interface proxy
func RetrieveMetricsFromPgBouncer(
	env *TestingEnvironment,
	pod corev1.Pod,
) (string, error) {
	body, err := runProxyRequest(env, &pod, false, url.PathMetrics, int(url.PgBouncerMetricsPort))
	return string(body), err
}

// RetrievePgStatusFromInstance aims to retrieve the pgStatus from a PostgreSQL instance pod
// using a GET request on the pod interface proxy
func RetrievePgStatusFromInstance(
	env *TestingEnvironment,
	pod corev1.Pod,
	tlsEnabled bool,
) (string, error) {
	body, err := runProxyRequest(env, &pod, tlsEnabled, url.PathPgStatus, int(url.StatusPort))
	return string(body), err
}
