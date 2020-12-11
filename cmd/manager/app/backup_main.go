/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package app

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// BackupCommand start a backup via the manager HTTP interface
func BackupCommand(args []string) {
	if len(args) != 1 {
		fmt.Println("Usage: manager backup <backup name>")
		os.Exit(1)
	}

	resp, err := http.Get("http://localhost:8000/pg/backup?name=" + args[0])
	if err != nil {
		log.Log.Error(err, "Error while requesting backup")
		os.Exit(1)
	}

	if resp.StatusCode != 200 {
		bytes, _ := ioutil.ReadAll(resp.Body)
		log.Log.Info(
			"Error while requesting backup",
			"statusCode", resp.StatusCode,
			"body", string(bytes))
		_ = resp.Body.Close()
		os.Exit(1)
	}

	_, err = io.Copy(os.Stderr, resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		log.Log.Error(err, "Error while starting a backup")
		os.Exit(1)
	}
}
