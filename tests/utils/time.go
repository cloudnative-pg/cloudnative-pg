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
	stdOut, _, err := env.EventuallyExecCommand(env.Ctx, *primaryPodInfo, "postgres",
		&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
	if err != nil {
		return "", err
	}

	currentTimestamp := strings.Trim(stdOut, "\n")
	return currentTimestamp, nil
}
