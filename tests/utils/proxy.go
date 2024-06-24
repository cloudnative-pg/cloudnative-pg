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

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
)

func runProxyRequest(env *TestingEnvironment, namespace, podName, path string, port int) ([]byte, error) {
	portString := strconv.Itoa(port)

	req := env.Interface.CoreV1().Pods(namespace).ProxyGet(
		"http", podName, portString, path, map[string]string{})

	return req.DoRaw(env.Ctx)
}

// RetrieveMetricsFromCluster aims to retrieve the metrics from a pod in
// PostgreSQL cluster using a proxy that can be used later
func RetrieveMetricsFromCluster(
	env *TestingEnvironment,
	namespace, podNme string,
) (string, error) {
	body, err := runProxyRequest(env, namespace, podNme, url.PathMetrics, int(url.PostgresMetricsPort))
	return string(body), err
}

// RetrieveMetricsFromPgBouncer aims to retrieve the metrics from a pod in
// PgBouncerusing a proxy that can be used later
func RetrieveMetricsFromPgBouncer(
	env *TestingEnvironment,
	namespace, podNme string,
) (string, error) {
	body, err := runProxyRequest(env, namespace, podNme, url.PathMetrics, int(url.PgBouncerMetricsPort))
	return string(body), err
}
