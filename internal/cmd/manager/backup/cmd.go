/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package backup implement the "controller backup" command
package backup

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/url"
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
