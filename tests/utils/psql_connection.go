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
	"database/sql"
	"io"
	"time"

	"k8s.io/client-go/tools/portforward"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/forwardconnection"
)

// PSQLForwardConnection manage the creation of a port forward to connect by psql client locally
type PSQLForwardConnection struct {
	pooler      pool.Pooler
	portForward *portforward.PortForwarder
}

// Close will stop the forward and exit
func (psqlc *PSQLForwardConnection) Close() {
	psqlc.portForward.Close()
}

// GetPooler returns the connection Pooler
func (psqlc *PSQLForwardConnection) GetPooler() pool.Pooler {
	return psqlc.pooler
}

// createConnectionParameters return the parameters require to create a connection
// to the current forwarded port
func createConnectionParameters(user, password, localPort string) map[string]string {
	return map[string]string{
		"host":     "localhost",
		"port":     localPort,
		"user":     user,
		"password": password,
	}
}

// ForwardPSQLConnection simplifies the creation of forwarded connection to PostgreSQL cluster
func ForwardPSQLConnection(
	env *TestingEnvironment,
	namespace,
	clusterName,
	dbname,
	secretSuffix string,
) (*PSQLForwardConnection, *sql.DB, error) {
	user, pass, err := GetCredentials(clusterName, namespace, secretSuffix, env)
	if err != nil {
		return nil, nil, err
	}

	return ForwardPSQLConnectionWithCreds(env, namespace, clusterName, dbname, user, pass)
}

// ForwardPSQLConnectionWithCreds does the same as ForwardPSQLConnection but without trying to
// get the credentials using the cluster
func ForwardPSQLConnectionWithCreds(
	env *TestingEnvironment,
	namespace,
	clusterName,
	dbname,
	userApp,
	passApp string,
) (*PSQLForwardConnection, *sql.DB, error) {
	cluster, err := env.GetCluster(namespace, clusterName)
	if err != nil {
		return nil, nil, err
	}

	forwarder, err := forwardconnection.NewPodForward(
		env.Ctx,
		env.Interface,
		env.RestClientConfig,
		namespace, cluster.Status.CurrentPrimary, "5432",
		io.Discard, io.Discard,
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

	pooler := pool.NewPostgresqlConnectionPool(configfile.CreateConnectionString(connParameters))
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

// RunQueryRowOverForward runs QueryRow with a given query, returning the Row of the SQL command
func RunQueryRowOverForward(
	env *TestingEnvironment,
	namespace,
	clusterName,
	dbname,
	secretSuffix,
	query string,
) (*sql.Row, error) {
	forward, conn, err := ForwardPSQLConnection(
		env,
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
	env *TestingEnvironment,
	namespace,
	clusterName,
	dbname,
	secretSuffix,
	query string,
) (sql.Result, error) {
	forward, conn, err := ForwardPSQLConnection(
		env,
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
