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

// Package status implement the "instance status" subcommand of the operator
package status

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	cacheClient "github.com/cloudnative-pg/cloudnative-pg/internal/management/cache/client"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
)

// NewCmd create the "instance status" subcommand
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return statusSubCommand(cmd.Context())
		},
	}

	return cmd
}

func statusSubCommand(ctx context.Context) error {
	cli, err := management.NewControllerRuntimeClient()
	if err != nil {
		log.Error(err, "while building the controller runtime client")
		return err
	}

	cluster, err := cacheClient.GetCluster()
	if err != nil {
		log.Error(err, "while loading the cluster from cache")
		return err
	}

	ctx, err = certs.NewTLSConfigForContext(
		ctx,
		cli,
		cluster.GetServerCASecretObjectKey(),
	)
	if err != nil {
		log.Error(err, "Error while building the TLS context")
		return err
	}

	resp, err := executeRequest(ctx, "https")
	if errors.Is(err, http.ErrSchemeMismatch) {
		resp, err = executeRequest(ctx, "http")
	}
	if err != nil {
		log.Error(err, "Error while requesting instance status")
		return err
	}

	defer func() {
		err = resp.Body.Close()
		if err != nil {
			log.Error(err, "Can't close the connection",
				"statusCode", resp.StatusCode,
			)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error(err, "Error while reading status response body",
			"statusCode", resp.StatusCode,
		)
		return err
	}

	if resp.StatusCode != 200 {
		log.Info(
			"Error while extracting status",
			"statusCode", resp.StatusCode,
			"body", string(body),
		)
		return fmt.Errorf("invalid status code: %v", resp.StatusCode)
	}

	_, err = os.Stdout.Write(body)
	if err != nil {
		log.Error(err, "Error while showing status info")
		return err
	}

	return nil
}

func executeRequest(ctx context.Context, scheme string) (*http.Response, error) {
	const connectionTimeout = 2 * time.Second
	const requestTimeout = 30 * time.Second

	statusURL := url.Build(
		scheme,
		"localhost", url.PathPgStatus, url.StatusPort,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
	if err != nil {
		log.Error(err, "Error while building the request")
		return nil, err
	}
	httpClient := resources.NewHTTPClient(connectionTimeout, requestTimeout)
	return httpClient.Do(req) // nolint:gosec
}
