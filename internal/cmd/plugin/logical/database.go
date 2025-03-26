/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package logical

import (
	"context"
	"fmt"

	"github.com/lib/pq"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// GetApplicationDatabaseName gets the application database name for
// a cluster with a given name
func GetApplicationDatabaseName(ctx context.Context, clusterName string) (string, error) {
	var cluster apiv1.Cluster
	err := plugin.Client.Get(
		ctx,
		client.ObjectKey{
			Namespace: plugin.Namespace,
			Name:      clusterName,
		},
		&cluster,
	)
	if err != nil {
		return "", fmt.Errorf("cluster %s not found in namespace %s: %w", clusterName, plugin.Namespace, err)
	}

	return cluster.GetApplicationDatabaseName(), nil
}

// GetSubscriptionConnInfo gets the connection string a subscription is connected to
func GetSubscriptionConnInfo(
	ctx context.Context,
	clusterName string,
	connectionString string,
	subscriptionName string,
) (string, error) {
	sqlCommand := fmt.Sprintf(
		"SELECT subconninfo FROM pg_catalog.pg_subscription WHERE subname=%s",
		pq.QuoteLiteral(subscriptionName),
	)
	output, err := RunSQLWithOutput(
		ctx,
		clusterName,
		connectionString,
		sqlCommand,
	)
	return string(output), err
}
