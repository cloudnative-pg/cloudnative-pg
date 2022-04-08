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
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/url"
)

// NewCmd create the "instance status" subcommand
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return statusSubCommand()
		},
	}

	return cmd
}

func statusSubCommand() error {
	statusURL := url.Local(url.PathPgStatus, url.StatusPort)
	resp, err := http.Get(statusURL) // nolint:gosec
	if err != nil {
		log.Error(err, "Error while requesting instance status")
		return err
	}

	defer func() {
		err = resp.Body.Close()
		if err != nil {
			log.Error(err, "Can't close the connection",
				"statusURL", statusURL,
				"statusCode", resp.StatusCode,
			)
		}
	}()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error(err, "Error while reading status response body",
			"statusURL", statusURL,
			"statusCode", resp.StatusCode,
		)
		return err
	}

	if resp.StatusCode != 200 {
		log.Info(
			"Error while extracting status",
			"statusURL", statusURL,
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
