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

/*
The manager command is the main entrypoint of CloudNativePG operator.
*/
package main

import (
	"errors"
	"net"
	"os"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/backup"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/bootstrap"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/controller"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/instance"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/istio"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/pgbouncer"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/show"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/walarchive"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/walrestore"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/versions"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func main() {
	if !isK8sRESTServerReadyWithRetries() {
		log.Warning("The K8S REST API Server is not ready")
		os.Exit(1)
	}
	logFlags := &log.Flags{}

	cmd := &cobra.Command{
		Use:          "manager [cmd]",
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			logFlags.ConfigureLogging()
		},
	}

	logFlags.AddFlags(cmd.PersistentFlags())

	cmd.AddCommand(backup.NewCmd())
	cmd.AddCommand(bootstrap.NewCmd())
	cmd.AddCommand(controller.NewCmd())
	cmd.AddCommand(instance.NewCmd())
	cmd.AddCommand(show.NewCmd())
	cmd.AddCommand(walarchive.NewCmd())
	cmd.AddCommand(walrestore.NewCmd())
	cmd.AddCommand(versions.NewCmd())
	cmd.AddCommand(pgbouncer.NewCmd())
	cmd.AddCommand(istio.NewCmd())

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// isK8sRESTServerReadyWithRetries attempts to retrieve the version of k8s REST API server, retrying
// the request if some communication error is encountered
func isK8sRESTServerReadyWithRetries() bool {
	// readinessCheckRetry is the default backoff used to query the healthiness of the k8s REST API Server
	readinessCheckRetry := wait.Backoff{
		Steps:    10,
		Duration: 10 * time.Millisecond,
		Factor:   5.0,
		Jitter:   0.1,
	}

	isErrorRetryable := func(err error) bool {
		// If it's a timeout, we do not want to retry
		var netError net.Error
		if errors.As(err, &netError) && netError.Timeout() {
			return false
		}

		return true
	}

	err := retry.OnError(readinessCheckRetry, isErrorRetryable, isK8sRESTServerReady)
	return err == nil
}

// isK8sRESTServerReady attempts to retrieve the version of k8s REST API server to test the readiness of the k8s REST
// API server.
func isK8sRESTServerReady() error {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	_, err = clientset.DiscoveryClient.ServerVersion()
	if err != nil {
		return err
	}
	return nil
}
