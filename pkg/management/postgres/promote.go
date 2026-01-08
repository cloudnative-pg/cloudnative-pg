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
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/log"
)

// PromoteAndWait promotes this instance, and wait DefaultPgCtlTimeoutForPromotion
// seconds for it to happen
func (instance *Instance) PromoteAndWait(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	instance.ShutdownConnections()

	instance.LogPgControldata(ctx, "promote")

	options := []string{
		"-D",
		instance.PgData,
		"-w",
		"promote",
		"-t " + strconv.Itoa(int(instance.Cluster.GetPgCtlTimeoutForPromotion())),
	}

	contextLogger.Info("Promoting instance", "pgctl_options", options)

	pgCtlCmd := exec.Command(pgCtlName, options...) // #nosec
	err := execlog.RunStreaming(pgCtlCmd, pgCtlName)
	if err != nil {
		return fmt.Errorf("error promoting the PostgreSQL instance: %w", err)
	}

	timeLimit := time.Now().Add(1 * time.Minute)
	for {
		if time.Now().After(timeLimit) {
			contextLogger.Info("The standby.signal file still exists but timeout reached, " +
				"error during PostgreSQL instance promotion")
			return fmt.Errorf("standby.signal still existent")
		}

		if status, _ := instance.IsPrimary(); status {
			break
		}

		time.Sleep(1 * time.Second)
	}

	contextLogger.Info("Requesting a checkpoint")

	db, err := instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("after having promoted the instance: %v", err)
	}

	err = db.Ping()
	if err != nil {
		return fmt.Errorf("after having promoted the instance: %v", err)
	}

	// For pg_rewind to work we need to issue a checkpoint here
	_, err = db.Exec("CHECKPOINT")
	if err != nil {
		return fmt.Errorf("checkpoint after instance promotion: %v", err)
	}

	contextLogger.Info("The PostgreSQL instance has been promoted successfully")

	return nil
}
