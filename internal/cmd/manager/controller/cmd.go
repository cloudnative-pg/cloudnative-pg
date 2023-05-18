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

package controller

import (
	"time"

	"github.com/spf13/cobra"
)

// NewCmd create a new cobra command
func NewCmd() *cobra.Command {
	var metricsAddr string
	var leaderElectionEnable bool
	var configMapName string
	var secretName string
	var port int
	var pprofHTTPServer bool
	var leaderLeaseDuration int
	var leaderRenewDeadline int

	cmd := cobra.Command{
		Use:           "controller [flags]",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunController(
				metricsAddr,
				configMapName,
				secretName,
				leaderElectionConfiguration{
					enable:        leaderElectionEnable,
					leaseDuration: time.Duration(leaderLeaseDuration) * time.Second,
					renewDeadline: time.Duration(leaderRenewDeadline) * time.Second,
				},
				pprofHTTPServer,
				port,
			)
		},
	}

	cmd.Flags().StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")

	cmd.Flags().BoolVar(&leaderElectionEnable, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"If enabled, this will ensure there is only one active controller manager.")
	cmd.Flags().IntVar(&leaderLeaseDuration, "leader-lease-duration", 15,
		"the leader lease duration expressed in seconds")
	cmd.Flags().IntVar(&leaderRenewDeadline, "leader-renew-deadline", 10,
		"the leader renew deadline expressed in seconds")

	cmd.Flags().StringVar(&configMapName, "config-map-name", "", "The name of the ConfigMap containing "+
		"the operator configuration")
	cmd.Flags().StringVar(&secretName, "secret-name", "", "The name of the Secret containing "+
		"the operator configuration. Values are merged with the ConfigMap's one, overwriting them if already defined")
	cmd.Flags().IntVar(&port, "webhook-port", 9443, "The port the controller should be listening on."+
		" If modified, take care to update the service pointing to it")
	cmd.Flags().BoolVar(
		&pprofHTTPServer,
		"pprof-server",
		false,
		"If true it will start a pprof debug http server on localhost:6060. Defaults to false.",
	)

	return &cmd
}
