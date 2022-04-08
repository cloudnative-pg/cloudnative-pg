/*
Copyright 2019-2022 The CloudNativePG Contributors

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

// Package backup implement the "controller backup" command
package backup

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
)

// NewCmd create a new cobra command
func NewCmd() *cobra.Command {
	cmd := cobra.Command{
		Use: "backup [backup_name]",
		RunE: func(cmd *cobra.Command, args []string) error {
			backupURL := url.Local(url.PathPgBackup, url.LocalPort)
			resp, err := http.Get(backupURL + "?name=" + args[0])
			if err != nil {
				log.Error(err, "Error while requesting backup")
				return err
			}

			defer func() {
				err := resp.Body.Close()
				if err != nil {
					log.Error(err, "Can't close the connection",
						"backupURL", backupURL,
						"statusCode", resp.StatusCode,
					)
				}
			}()

			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				log.Error(err, "Error while reading backup response body",
					"backupURL", backupURL,
					"statusCode", resp.StatusCode,
				)
				return err
			}

			if resp.StatusCode != 200 {
				log.Info(
					"Error while requesting backup",
					"backupURL", backupURL,
					"statusCode", resp.StatusCode,
					"body", string(body),
				)
				return fmt.Errorf("invalid status code: %v", resp.StatusCode)
			}

			_, err = os.Stderr.Write(body)
			if err != nil {
				log.Error(err, "Error while starting a backup")
				return err
			}

			return nil
		},
		Args: cobra.ExactArgs(1),
	}

	return &cmd
}
