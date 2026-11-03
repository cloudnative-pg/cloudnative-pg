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

package postgres

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const maxConnectionsParameter = "max_connections"

// pgControldataSettingsToParamsMap maps the hot standby sensitive settings, as
// printed by pg_controldata, to the corresponding PostgreSQL parameter names
var pgControldataSettingsToParamsMap = map[string]string{
	"max_connections setting":      maxConnectionsParameter,
	"max_wal_senders setting":      "max_wal_senders",
	"max_worker_processes setting": "max_worker_processes",
	"max_prepared_xacts setting":   "max_prepared_transactions",
	"max_locks_per_xact setting":   "max_locks_per_transaction",
}

// LoadEnforcedParametersFromPgControldata will parse the output of pg_controldata in order to get
// the values of all the hot standby sensible parameters
func LoadEnforcedParametersFromPgControldata(pgData string) (map[string]int, error) {
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	pgControlDataCmd := exec.Command(pgControlDataName,
		"-D",
		pgData) // #nosec G204
	pgControlDataCmd.Stdout = &stdoutBuffer
	pgControlDataCmd.Stderr = &stderrBuffer
	pgControlDataCmd.Env = append(pgControlDataCmd.Env, "LANG=C", "LC_MESSAGES=C")
	err := pgControlDataCmd.Run()
	if err != nil {
		log.Error(err, "while reading pg_controldata",
			"stderr", stderrBuffer.String(),
			"stdout", stdoutBuffer.String())
		return nil, err
	}

	log.Debug("pg_controldata stdout", "stdout", stdoutBuffer.String())

	enforcedParams := make(map[string]int)
	for key, value := range utils.ParsePgControldataOutput(stdoutBuffer.String()) {
		if param, ok := pgControldataSettingsToParamsMap[key]; ok {
			intValue, err := strconv.Atoi(value)
			if err != nil {
				log.Error(err, "while parsing pg_controldata content",
					"key", key,
					"value", value)
				return nil, err
			}
			enforcedParams[param] = intValue
		}
	}

	return enforcedParams, nil
}

// LoadStartupParameterValue asks PostgreSQL which value the given parameter
// will have at startup with the current configuration files, by running
// `postgres -C <name>`. It works while the server is not running: when no
// configuration file sets the parameter, it returns the built-in default.
func LoadStartupParameterValue(pgData string, name string) (int, error) {
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	postgresCmd := exec.Command(GetPostgresExecutableName(),
		"-D", pgData,
		"-C", name) // #nosec G204
	postgresCmd.Stdout = &stdoutBuffer
	postgresCmd.Stderr = &stderrBuffer
	// the postgres executable needs the parent environment (PATH above all)
	// to locate itself while evaluating the -C flag
	postgresCmd.Env = append(os.Environ(), "LANG=C", "LC_MESSAGES=C")
	if err := postgresCmd.Run(); err != nil {
		log.Error(err, "while reading the startup value of a parameter",
			"name", name,
			"stderr", stderrBuffer.String(),
			"stdout", stdoutBuffer.String())
		return 0, err
	}

	value, err := strconv.Atoi(strings.TrimSpace(stdoutBuffer.String()))
	if err != nil {
		log.Error(err, "while parsing the startup value of a parameter",
			"name", name,
			"value", stdoutBuffer.String())
		return 0, err
	}

	return value, nil
}

// LoadEnforcedParametersFromCluster loads the enforced parameters which defined in cluster spec
func LoadEnforcedParametersFromCluster(
	cluster *apiv1.Cluster,
) (map[string]int, error) {
	clusterParams := cluster.Spec.PostgresConfiguration.Parameters
	enforcedParams := map[string]int{}
	for _, param := range pgControldataSettingsToParamsMap {
		value, found := clusterParams[param]
		if !found {
			continue
		}

		intValue, err := strconv.Atoi(value)
		if err != nil {
			log.Error(err, "while parsing enforced postgres parameter",
				"param", param,
				"value", value)
			return nil, err
		}
		enforcedParams[param] = intValue
	}
	return enforcedParams, nil
}
