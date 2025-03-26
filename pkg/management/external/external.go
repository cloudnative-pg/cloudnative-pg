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

package external

import (
	"context"
	"maps"

	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
)

// GetServerConnectionString gets the connection string to be
// used to connect to this external server, without dumping
// the required cryptographic material
func GetServerConnectionString(
	server *apiv1.ExternalCluster,
	databaseName string,
) string {
	connectionParameters := maps.Clone(server.ConnectionParameters)

	if server.SSLCert != nil {
		name := getSecretKeyRefFileName(server.Name, server.SSLCert)
		connectionParameters["sslcert"] = name
	}

	if server.SSLKey != nil {
		name := getSecretKeyRefFileName(server.Name, server.SSLKey)
		connectionParameters["sslkey"] = name
	}

	if server.SSLRootCert != nil {
		name := getSecretKeyRefFileName(server.Name, server.SSLRootCert)
		connectionParameters["sslrootcert"] = name
	}

	if server.Password != nil {
		pgpassfile := getPgPassFilePath(server.Name)
		connectionParameters["passfile"] = pgpassfile
	}

	if databaseName != "" {
		connectionParameters["dbname"] = databaseName
	}

	return configfile.CreateConnectionString(connectionParameters)
}

// ConfigureConnectionToServer creates a connection string to the external
// server, using the configuration inside the cluster and dumping the secret when
// needed in a custom passfile.
// Returns a connection string or any error encountered
func ConfigureConnectionToServer(
	ctx context.Context,
	client ctrl.Client,
	namespace string,
	server *apiv1.ExternalCluster,
) (string, error) {
	connectionParameters := maps.Clone(server.ConnectionParameters)

	if server.SSLCert != nil {
		name, err := dumpSecretKeyRefToFile(ctx, client, namespace, server.Name, server.SSLCert)
		if err != nil {
			return "", err
		}

		connectionParameters["sslcert"] = name
	}

	if server.SSLKey != nil {
		name, err := dumpSecretKeyRefToFile(ctx, client, namespace, server.Name, server.SSLKey)
		if err != nil {
			return "", err
		}

		connectionParameters["sslkey"] = name
	}

	if server.SSLRootCert != nil {
		name, err := dumpSecretKeyRefToFile(ctx, client, namespace, server.Name, server.SSLRootCert)
		if err != nil {
			return "", err
		}

		connectionParameters["sslrootcert"] = name
	}

	if server.Password != nil {
		password, err := readSecretKeyRef(ctx, client, namespace, server.Password)
		if err != nil {
			return "", err
		}

		pgpassfile, err := createPgPassFile(server.Name, connectionParameters, password)
		if err != nil {
			return "", err
		}

		connectionParameters["passfile"] = pgpassfile
	}

	return configfile.CreateConnectionString(connectionParameters), nil
}
