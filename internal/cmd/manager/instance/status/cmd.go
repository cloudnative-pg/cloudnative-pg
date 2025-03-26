/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

// Package status implement the "instance status" subcommand of the operator
package status

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
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
	contextLogger := log.FromContext(ctx)
	statusURL := url.Local(url.PathPgStatus, url.StatusPort)
	resp, err := http.Get(statusURL) // nolint:gosec
	if err != nil {
		contextLogger.Error(err, "Error while requesting instance status")
		return err
	}

	defer func() {
		err = resp.Body.Close()
		if err != nil {
			contextLogger.Error(err, "Can't close the connection",
				"statusURL", statusURL,
				"statusCode", resp.StatusCode,
			)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		contextLogger.Error(err, "Error while reading status response body",
			"statusURL", statusURL,
			"statusCode", resp.StatusCode,
		)
		return err
	}

	if resp.StatusCode != 200 {
		contextLogger.Info(
			"Error while extracting status",
			"statusURL", statusURL,
			"statusCode", resp.StatusCode,
			"body", string(body),
		)
		return fmt.Errorf("invalid status code: %v", resp.StatusCode)
	}

	_, err = os.Stdout.Write(body)
	if err != nil {
		contextLogger.Error(err, "Error while showing status info")
		return err
	}

	return nil
}
