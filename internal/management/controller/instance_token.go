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

package controller

import (
	"fmt"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/promotiontoken"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Assuming this PostgreSQL instance is a replica and we have a promotion token
// to wait before promoting it, we verify it, delaying the promotion if the
// token conditions are not met
func (r *InstanceReconciler) verifyPromotionToken(cluster *apiv1.Cluster) error {
	if !cluster.ShouldPromoteFromReplicaCluster() {
		return nil
	}

	promotionToken, err := utils.ParsePgControldataToken(cluster.Spec.ReplicaCluster.PromotionToken)
	if err != nil {
		// The promotion token is not correct, and the webhook should
		// have prevented this to happen. If we're here, two things
		// could have happened:
		//
		// 1. we've a bug in the webhook
		// 2. the user didn't install the webhook
		//
		// We don't have any other possibility than raising this error.
		// It will be written in the log of the instance manager
		return fmt.Errorf("while decoding the promotion token: %w", err)
	}

	if err := promotionToken.IsValid(); err != nil {
		// The promotion token is not valid, and the webhook should
		// have prevented this to happen. This is the same case as
		// the previous check
		return fmt.Errorf("while validating the promotion token: %w", err)
	}

	// Request a checkpoint on the replica instance, to
	// ensure update the control file
	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("could not get the database connection pool: %w", err)
	}

	if _, err := db.Exec("CHECKPOINT"); err != nil {
		return fmt.Errorf("could not request a checkpoint: %w", err)
	}

	// This is a replica, and we can't get the current timeline using
	// SQL. We need to call pg_controldata just for that.
	out, err := r.instance.GetPgControldata()
	if err != nil {
		return fmt.Errorf("while verifying the promotion token [pg_controldata]: %w", err)
	}

	parsedControlData := utils.ParsePgControldataOutput(out)
	currentTimelineIDString := parsedControlData.GetLatestCheckpointTimelineID()
	currentSystemIdentifier := parsedControlData.GetDatabaseSystemIdentifier()
	replayLSNString := parsedControlData.GetLatestCheckpointREDOLocation()

	return promotiontoken.ValidateAgainstInstanceStatus(promotionToken, currentSystemIdentifier,
		currentTimelineIDString, replayLSNString)
}
