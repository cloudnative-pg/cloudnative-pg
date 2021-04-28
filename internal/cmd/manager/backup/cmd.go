/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package backup implement the "controller backup" command
package backup

import (
	"fmt"
	"io"
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
			resp, err := http.Get(url.Local(url.PathPgBackup) + "?name=" + args[0])
			if err != nil {
				log.Log.Error(err, "Error while requesting backup")
				return err
			}

			if resp.StatusCode != 200 {
				bytes, _ := ioutil.ReadAll(resp.Body)
				log.Log.Info(
					"Error while requesting backup",
					"statusCode", resp.StatusCode,
					"body", string(bytes))
				_ = resp.Body.Close()
				return fmt.Errorf("invalid status code: %v", resp.StatusCode)
			}

			_, err = io.Copy(os.Stderr, resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				log.Log.Error(err, "Error while starting a backup")
				return err
			}

			return nil
		},
		Args: cobra.ExactArgs(1),
	}

	return &cmd
}
