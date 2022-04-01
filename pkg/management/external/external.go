/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
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
