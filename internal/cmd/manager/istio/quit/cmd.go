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

// Package quit implement the quit command
package quit

import (
	istioproxy "github.com/allisson/go-istio-proxy-wait"
	"github.com/spf13/cobra"
	"net/http"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// NewCmd generates the "quit" subcommand
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quit",
		Short: "Quit istio sidecar if exist",
		RunE: func(cobraCmd *cobra.Command, args []string) error {

			return quitSubCommand()
		},
	}

	return cmd
}

func quitSubCommand() error {
	if isIstioReady() {

		IstioQuitEndpoint := "http://localhost:15000/quitquitquit"

		resp, err := http.Post(IstioQuitEndpoint, "", nil)
		if err != nil {
			log.Warning("Fail to quit istio-proxy")
		}
		defer resp.Body.Close()

	}
	log.Warning("istio-proxy is not ready or is not enabled at all, no need to quit it")
	return nil
}

func isIstioReady() bool {
	istioProxy := istioproxy.New(time.Second, time.Second, 5)
	// Wait until the istio-proxy is ready, or it's out of the timeout.
	if err := istioProxy.Wait(); err != nil {
		log.Warning("istio-proxy is not ready or is not enabled at all: %s", err.Error())
		return false
	}
	defer func() {
		if err := istioProxy.Close(); err != nil {
			log.Warning(err.Error())
		}
	}()
	return true
}
