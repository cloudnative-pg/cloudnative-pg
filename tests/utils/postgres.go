/*
Copyright 2019-2022 The CloudNativePG Contributors

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
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
)

// PGLocalSocketDir is the directory containing the PostgreSQL local socket
const PGLocalSocketDir = "/controller/run"

// RunQueryFromPod executes a query from a pod to a host
func RunQueryFromPod(
	connectingPod *corev1.Pod,
	host string,
	dbname string,
	user string,
	password string,
	query string,
	env *TestingEnvironment,
) (string, string, error) {
	timeout := time.Second * 2
	dsn := fmt.Sprintf("host=%v user=%v dbname=%v password=%v sslmode=require",
		host, user, dbname, password)

	stdout, stderr, err := env.EventuallyExecCommand(env.Ctx, *connectingPod, specs.PostgresContainerName, &timeout,
		"psql", dsn, "-tAc", query)
	return stdout, stderr, err
}

// CountReplicas counts the number of replicas attached to an instance
func CountReplicas(env *TestingEnvironment, pod *corev1.Pod) (int, error) {
	query := "SELECT count(*) FROM pg_stat_replication"
	commandTimeout := time.Second * 2
	stdOut, _, err := env.EventuallyExecCommand(env.Ctx, *pod, specs.PostgresContainerName,
		&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
	if err != nil {
		return 0, nil
	}
	return strconv.Atoi(strings.Trim(stdOut, "\n"))
}
