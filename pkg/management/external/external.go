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

// Package external contains the functions needed to manage servers which are external to this
// PostgreSQL cluster
package external

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/configfile"
)

// ConfigureConnectionToServer creates a connection string to the external
// server, using the configuration inside the cluster and dumping the secret when
// needed. This function will return a connection string, the name of the pgpass file
// to be used, and an error state
func ConfigureConnectionToServer(
	ctx context.Context, client ctrl.Client,
	namespace string, server *apiv1.ExternalCluster,
) (string, string, error) {
	connectionParameters := make(map[string]string, len(server.ConnectionParameters))
	pgpassfile := ""

	for key, value := range server.ConnectionParameters {
		connectionParameters[key] = value
	}

	if server.SSLCert != nil {
		name, err := DumpSecretKeyRefToFile(ctx, client, namespace, server.Name, server.SSLCert)
		if err != nil {
			return "", "", err
		}

		connectionParameters["sslcert"] = name
	}

	if server.SSLKey != nil {
		name, err := DumpSecretKeyRefToFile(ctx, client, namespace, server.Name, server.SSLKey)
		if err != nil {
			return "", "", err
		}

		connectionParameters["sslkey"] = name
	}

	if server.SSLRootCert != nil {
		name, err := DumpSecretKeyRefToFile(ctx, client, namespace, server.Name, server.SSLRootCert)
		if err != nil {
			return "", "", err
		}

		connectionParameters["sslrootcert"] = name
	}

	if server.Password != nil {
		password, err := ReadSecretKeyRef(ctx, client, namespace, server.Password)
		if err != nil {
			return "", "", err
		}

		pgpassfile, err = CreatePgPassFile(server.Name, password)
		if err != nil {
			return "", "", err
		}
	}

	return configfile.CreateConnectionString(connectionParameters), pgpassfile, nil
}
