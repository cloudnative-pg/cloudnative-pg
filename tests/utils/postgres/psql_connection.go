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

package postgres

import (
	"context"
	"database/sql"
	"io"
	"time"

	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/forwardconnection"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
)

// PSQLForwardConnection manages the creation of a port-forwarding to open a new database connection
type PSQLForwardConnection struct {
	pooler      pool.Pooler
	portForward *portforward.PortForwarder
}

// Close will stop the port-forwarding and exit
func (psqlc *PSQLForwardConnection) Close() {
	psqlc.portForward.Close()
}

// GetPooler returns the connection Pooler
func (psqlc *PSQLForwardConnection) GetPooler() pool.Pooler {
	return psqlc.pooler
}

// createConnectionParameters returns a map of parameters required to perform a connection
func createConnectionParameters(user, password, localPort string) map[string]string {
	return map[string]string{
		"host":     "localhost",
		"port":     localPort,
		"user":     user,
		"password": password,
	}
}

func startForwardConnection(
	dialer httpstream.Dialer,
	portMap []string,
	dbname,
	userApp,
	passApp string,
) (*PSQLForwardConnection, *sql.DB, error) {
	forwarder, err := forwardconnection.NewForwardConnection(
		dialer,
		portMap,
		io.Discard,
		io.Discard,
	)
	if err != nil {
		return nil, nil, err
	}

	if err = forwarder.StartAndWait(); err != nil {
		return nil, nil, err
	}

	localPort, err := forwarder.GetLocalPort()
	if err != nil {
		return nil, nil, err
	}

	connParameters := createConnectionParameters(userApp, passApp, localPort)

	pooler := pool.NewPgbouncerConnectionPool(configfile.CreateConnectionString(connParameters))

	conn, err := pooler.Connection(dbname)
	if err != nil {
		return nil, nil, err
	}

	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(10)
	conn.SetConnMaxLifetime(time.Hour)
	conn.SetConnMaxIdleTime(time.Hour)

	return &PSQLForwardConnection{
		portForward: forwarder.Forwarder,
		pooler:      pooler,
	}, conn, err
}

// ForwardPSQLConnection simplifies the creation of forwarded connection to PostgreSQL cluster
func ForwardPSQLConnection(
	ctx context.Context,
	crudClient client.Client,
	kubeInterface kubernetes.Interface,
	restConfig *rest.Config,
	namespace,
	clusterName,
	dbname,
	secretSuffix string,
) (*PSQLForwardConnection, *sql.DB, error) {
	user, pass, err := secrets.GetCredentials(ctx, crudClient, clusterName, namespace, secretSuffix)
	if err != nil {
		return nil, nil, err
	}

	return ForwardPSQLConnectionWithCreds(
		ctx,
		crudClient,
		kubeInterface,
		restConfig,
		namespace, clusterName, dbname, user, pass,
	)
}

// ForwardPSQLConnectionWithCreds creates a forwarded connection to a PostgreSQL cluster
// using the given credentials
func ForwardPSQLConnectionWithCreds(
	ctx context.Context,
	crudClient client.Client,
	kubeInterface kubernetes.Interface,
	restConfig *rest.Config,
	namespace,
	clusterName,
	dbname,
	userApp,
	passApp string,
) (*PSQLForwardConnection, *sql.DB, error) {
	cluster, err := clusterutils.Get(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return nil, nil, err
	}

	dialer, err := forwardconnection.NewDialer(
		kubeInterface,
		restConfig,
		namespace,
		cluster.Status.CurrentPrimary,
	)
	if err != nil {
		return nil, nil, err
	}

	psqlForwardConn, conn, err := startForwardConnection(
		dialer,
		[]string{forwardconnection.PostgresPortMap},
		dbname,
		userApp,
		passApp,
	)
	if err != nil {
		return nil, nil, err
	}

	return psqlForwardConn, conn, err
}

// ForwardPSQLServiceConnection creates a forwarded connection to a PostgreSQL service
// using the given credentials
func ForwardPSQLServiceConnection(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	restConfig *rest.Config,
	namespace,
	serviceName,
	dbname,
	userApp,
	passApp string,
) (*PSQLForwardConnection, *sql.DB, error) {
	dialer, portMap, err := forwardconnection.NewDialerFromService(
		ctx,
		kubeInterface,
		restConfig,
		namespace,
		serviceName,
	)
	if err != nil {
		return nil, nil, err
	}

	psqlForwardConn, conn, err := startForwardConnection(dialer, portMap, dbname, userApp, passApp)
	if err != nil {
		return nil, nil, err
	}

	return psqlForwardConn, conn, err
}

// RunQueryRowOverForward runs QueryRow with a given query, returning the Row of the SQL command
func RunQueryRowOverForward(
	ctx context.Context,
	crudClient client.Client,
	kubeInterface kubernetes.Interface,
	restConfig *rest.Config,
	namespace,
	clusterName,
	dbname,
	secretSuffix,
	query string,
) (*sql.Row, error) {
	forward, conn, err := ForwardPSQLConnection(
		ctx,
		crudClient,
		kubeInterface,
		restConfig,
		namespace,
		clusterName,
		dbname,
		secretSuffix,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = conn.Close()
		forward.Close()
	}()

	return conn.QueryRow(query), nil
}

// RunExecOverForward runs Exec with a given query, returning the Result of the SQL command
func RunExecOverForward(
	ctx context.Context,
	crudClient client.Client,
	kubeInterface kubernetes.Interface,
	restConfig *rest.Config,
	namespace,
	clusterName,
	dbname,
	secretSuffix,
	query string,
) (sql.Result, error) {
	forward, conn, err := ForwardPSQLConnection(
		ctx,
		crudClient,
		kubeInterface,
		restConfig,
		namespace,
		clusterName,
		dbname,
		secretSuffix,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = conn.Close()
		forward.Close()
	}()

	return conn.Exec(query)
}
