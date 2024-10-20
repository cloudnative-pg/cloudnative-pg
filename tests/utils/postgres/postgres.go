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

// Package postgres provides functions to handle postgres in cnpg clusters
package postgres

import (
	"context"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
)

const (
	// PGLocalSocketDir is the directory containing the PostgreSQL local socket
	PGLocalSocketDir = "/controller/run"
	// AppUser for app user
	AppUser = "app"
	// PostgresUser for postgres user
	PostgresUser = "postgres"
	// AppDBName database name app
	AppDBName = "app"
	// PostgresDBName database name postgres
	PostgresDBName = "postgres"
	// TablespaceDefaultName is the default tablespace location
	TablespaceDefaultName = "pg_default"
)

// CountReplicas counts the number of replicas attached to an instance
func CountReplicas(
	ctx context.Context,
	crudClient client.Client,
	kubeInterface kubernetes.Interface,
	restConfig *rest.Config,
	pod *corev1.Pod,
	retryTimeout int,
) (int, error) {
	query := "SELECT count(*) FROM pg_stat_replication"
	stdOut, _, err := exec.EventuallyExecQueryInInstancePod(
		ctx, crudClient, kubeInterface, restConfig,
		exec.PodLocator{
			Namespace: pod.Namespace,
			PodName:   pod.Name,
		}, AppDBName,
		query,
		retryTimeout,
		objects.PollingTime,
	)
	if err != nil {
		return 0, nil
	}
	return strconv.Atoi(strings.Trim(stdOut, "\n"))
}
