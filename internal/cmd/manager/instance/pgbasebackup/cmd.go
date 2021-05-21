/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package pgbasebackup implement the pgbasebackup bootstrap method
package pgbasebackup

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/configfile"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

// CloneInfo is the structure containing all the information needed
// to clone an existing server
type CloneInfo struct {
	info   *postgres.InitInfo
	client ctrl.Client
}

// NewCmd creates the "pgbasebackup" subcommand
func NewCmd() *cobra.Command {
	var clusterName string
	var namespace string
	var pwFile string
	var pgData string

	cmd := &cobra.Command{
		Use: "pgbasebackup",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := management.NewControllerRuntimeClient()
			if err != nil {
				return err
			}

			env := CloneInfo{
				info: &postgres.InitInfo{
					PgData:       pgData,
					PasswordFile: pwFile,
					ClusterName:  clusterName,
					Namespace:    namespace,
				},
				client: client,
			}

			ctx := context.Background()

			if err = env.bootstrapUsingPgbasebackup(ctx); err != nil {
				log.Log.Error(err, "Unable to boostrap cluster")
			}
			return err
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")
	cmd.Flags().StringVar(&pwFile, "pw-file", "",
		"The file containing the PostgreSQL superuser password to use during the init phase")
	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")

	return cmd
}

// bootstrapUsingPgbasebackup creates a new data dir from the configuration
func (env *CloneInfo) bootstrapUsingPgbasebackup(ctx context.Context) error {
	var cluster apiv1.Cluster
	err := env.client.Get(ctx, ctrl.ObjectKey{Namespace: env.info.Namespace, Name: env.info.ClusterName}, &cluster)
	if err != nil {
		return err
	}

	server, ok := cluster.ExternalServer(cluster.Spec.Bootstrap.PgBaseBackup.Source)
	if !ok {
		return fmt.Errorf("missing external server")
	}

	connectionString, err := env.configureConnectionToExternalServer(ctx, &server)
	if err != nil {
		return err
	}

	err = postgres.ClonePgData(connectionString, env.info.PgData)
	if err != nil {
		return err
	}

	if err := env.info.WriteInitialPostgresqlConf(ctx, env.client); err != nil {
		return err
	}

	if err := env.info.WriteRestoreHbaConf(); err != nil {
		return err
	}

	return env.info.ConfigureInstanceAfterRestore()
}

// configureConnectionToExternalServer creates a connection string to the external
// server, using the configuration inside the cluster and dumping the secret when
// needed
func (env *CloneInfo) configureConnectionToExternalServer(
	ctx context.Context, server *apiv1.ExternalCluster) (string, error) {
	connectionParameters := make(map[string]string, len(server.ConnectionParameters))
	for key, value := range server.ConnectionParameters {
		connectionParameters[key] = value
	}

	if server.SSLCert != nil {
		name, err := env.dumpSecretKeyRefToFile(ctx, server.SSLCert)
		if err != nil {
			return "", err
		}

		connectionParameters["sslcert"] = name
	}

	if server.SSLKey != nil {
		name, err := env.dumpSecretKeyRefToFile(ctx, server.SSLKey)
		if err != nil {
			return "", err
		}

		connectionParameters["sslkey"] = name
	}

	if server.SSLRootCert != nil {
		name, err := env.dumpSecretKeyRefToFile(ctx, server.SSLRootCert)
		if err != nil {
			return "", err
		}

		connectionParameters["sslrootcert"] = name
	}

	if server.Password != nil {
		password, err := env.readSecretKeyRef(ctx, server.Password)
		if err != nil {
			return "", err
		}

		passfile, err := createPgPassFile(password)
		if err != nil {
			return "", err
		}

		// Unfortunately we can't use the "passfile" option inside the PostgreSQL
		// DSN, as the "github.com/lib/pq" doesn't support it (while libpq handles
		// it correctly)
		err = os.Setenv("PGPASSFILE", passfile)
		if err != nil {
			return "", err
		}
	}

	return configfile.CreateConnectionString(connectionParameters), nil
}

// dumpSecretKeyRefToFile dumps a certain secret to a file inside a temporary folder
// using 0600 as permission bits
func (env *CloneInfo) dumpSecretKeyRefToFile(
	ctx context.Context, selector *corev1.SecretKeySelector) (string, error) {
	var secret corev1.Secret
	err := env.client.Get(ctx, ctrl.ObjectKey{Namespace: env.info.Namespace, Name: selector.Name}, &secret)
	if err != nil {
		return "", err
	}

	value, ok := secret.Data[selector.Key]
	if !ok {
		return "", fmt.Errorf("missing key %v in secret %v", selector.Key, selector.Name)
	}

	f, err := ioutil.TempFile("/controller", fmt.Sprintf("%v_%v", selector.Name, selector.Key))
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
	}()

	_, err = f.Write(value)
	if err != nil {
		return "", err
	}

	return f.Name(), nil
}

// readSecretKeyRef dumps a certain secret to a file inside a temporary folder
// using 0600 as permission bits
func (env *CloneInfo) readSecretKeyRef(
	ctx context.Context, selector *corev1.SecretKeySelector) (string, error) {
	var secret corev1.Secret
	err := env.client.Get(ctx, ctrl.ObjectKey{Namespace: env.info.Namespace, Name: selector.Name}, &secret)
	if err != nil {
		return "", err
	}

	value, ok := secret.Data[selector.Key]
	if !ok {
		return "", fmt.Errorf("missing key %v in secret %v", selector.Key, selector.Name)
	}

	return string(value), err
}

// createPgPassFile creates a pgpass file inside the user home directory
func createPgPassFile(password string) (string, error) {
	pgpassContent := fmt.Sprintf(
		"%v:%v:%v:%v:%v",
		"*", // hostname
		"*", // port
		"*", // database
		"*", // username
		password)

	f, err := ioutil.TempFile("/controller", "pgpass")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
	}()

	_, err = f.WriteString(pgpassContent)
	if err != nil {
		return "", err
	}

	return f.Name(), nil
}
