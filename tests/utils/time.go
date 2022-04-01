/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"strings"
	"time"
)

// GetCurrentTimestamp getting current time stamp from postgres server
func GetCurrentTimestamp(namespace, clusterName string, env *TestingEnvironment) (string, error) {
	commandTimeout := time.Second * 5
	primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
	if err != nil {
		return "", err
	}

	query := "select TO_CHAR(CURRENT_TIMESTAMP,'YYYY-MM-DD HH24:MI:SS');"
	stdOut, _, err := env.ExecCommand(env.Ctx, *primaryPodInfo, "postgres",
		&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
	if err != nil {
		return "", err
	}

	currentTimestamp := strings.Trim(stdOut, "\n")
	return currentTimestamp, nil
}
