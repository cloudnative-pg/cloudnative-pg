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

package client

import (
	"context"

	"github.com/cloudnative-pg/cnpg-i/pkg/postgres"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PostgresConfigurationCapabilities is the interface that defines the
// capabilities of interacting with PostgreSQL.
type PostgresConfigurationCapabilities interface {
	// EnrichConfiguration is the method that enriches the PostgreSQL configuration
	EnrichConfiguration(
		ctx context.Context,
		cluster client.Object,
		config map[string]string,
		operationType postgres.OperationType_Type,
	) (map[string]string, error)
}
