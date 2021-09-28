/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package status implement the "instance status" subcommand of the operator
package status

import (
	"io"
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
	resp, err := http.Get(url.Local(url.PathPgStatus, url.StatusPort))
	if err != nil {
		log.Error(err, "Error while requesting instance status")
		return err
	}

	if resp.StatusCode != 200 {
		bytes, _ := ioutil.ReadAll(resp.Body)
		log.Info(
			"Error while extracting status",
			"statusCode", resp.StatusCode, "body", string(bytes))
		_ = resp.Body.Close()
		return err
	}

	_, err = io.Copy(os.Stdout, resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		log.Error(err, "Error while showing status info")
		return err
	}

	return nil
}
