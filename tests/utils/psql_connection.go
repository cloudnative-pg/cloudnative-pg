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
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/onsi/ginkgo/v2"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
)

// PSQLConnection manage the creation of a port forward to connect by psql client locally
type PSQLConnection struct {
	Namespace   string
	Pod         string
	stopChan    chan struct{}
	readyChan   chan struct{}
	Pooler      *pool.ConnectionPool
	PortForward *portforward.PortForwarder
	err         error
}

// PSQLConnectionNew initialize and create the proper forward configuration
func PSQLConnectionNew(env *TestingEnvironment, namespace, pod string) (*PSQLConnection, error) {
	psqlc := &PSQLConnection{}
	if pod == "" {
		return nil, fmt.Errorf("servicename or pod not provided")
	}
	psqlc.Namespace = namespace
	psqlc.Pod = pod

	req := psqlc.createRequest(env)

	transport, upgrader, err := spdy.RoundTripperFor(env.RestClientConfig)
	if err != nil {
		return nil, err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	psqlc.readyChan = make(chan struct{}, 1)
	psqlc.stopChan = make(chan struct{})

	psqlc.PortForward, err = portforward.New(
		dialer,
		[]string{"0:5432"},
		psqlc.stopChan,
		psqlc.readyChan,
		os.Stdout,
		os.Stderr,
	)

	return psqlc, err
}

func (psqlc *PSQLConnection) createRequest(env *TestingEnvironment) *rest.Request {
	return env.Interface.CoreV1().
		RESTClient().
		Post().
		Resource("pods").
		Namespace(psqlc.Namespace).
		Name(psqlc.Pod).
		SubResource("portforward")
}

// StartAndWait will begin the forward and wait to be ready
func (psqlc *PSQLConnection) StartAndWait() error {
	go func() {
		ginkgo.GinkgoWriter.Printf("Starting port-forward\n")
		psqlc.err = psqlc.PortForward.ForwardPorts()
		if psqlc.err != nil {
			ginkgo.GinkgoWriter.Printf("port-foward failed with error %s\n", psqlc.err.Error()) //nolint:misspell
			return
		}
	}()
	select {
	case <-psqlc.readyChan:
		ginkgo.GinkgoWriter.Printf("port-forward ready\n")
		return nil
	case <-psqlc.stopChan:
		ginkgo.GinkgoWriter.Printf("port-forward closed\n")
		return psqlc.err
	}
}

// GetLocalPort gets the local port needed to connect to Postgres
func (psqlc *PSQLConnection) GetLocalPort() (string, error) {
	forwardedPorts, err := psqlc.PortForward.GetPorts()
	if err != nil {
		return "", err
	}
	port := strconv.Itoa(int(forwardedPorts[0].Local))
	return port, nil
}

// Stop will stop the forward and exit
func (psqlc *PSQLConnection) Stop() {
	psqlc.PortForward.Close()
}

// createConnectionParameters return the parameters require to create a connection
// to the current forwarded port
func (psqlc *PSQLConnection) createConnectionParameters(user, password string) (map[string]string, error) {
	port, err := psqlc.GetLocalPort()
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"host":     "localhost",
		"port":     port,
		"user":     user,
		"password": password,
	}, nil
}

// ForwardPSQLConnection simplify the creation of forwarded connection to PostgreSQL cluster
func ForwardPSQLConnection(
	env *TestingEnvironment,
	namespace,
	clusterName,
	dbname,
	secretSuffix string,
) (*PSQLConnection, *sql.DB, error) {
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
) (*PSQLConnection, *sql.DB, error) {
	cluster, err := env.GetCluster(namespace, clusterName)
	if err != nil {
		return nil, nil, err
	}

	forward, err := PSQLConnectionNew(env, namespace, cluster.Status.CurrentPrimary)
	if err != nil {
		return nil, nil, err
	}
	err = forward.StartAndWait()
	if err != nil {
		return nil, nil, err
	}

	connParameters, err := forward.createConnectionParameters(userApp, passApp)
	if err != nil {
		return nil, nil, err
	}

	forward.Pooler = pool.NewPostgresqlConnectionPool(configfile.CreateConnectionString(connParameters))
	conn, err := forward.Pooler.Connection(dbname)
	return forward, conn, err
}
