/*
Copyright The CloudNativePG Contributors

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

package controller

import (
	"fmt"
	"strconv"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// tokenVerificationError are raised when the shutdown token
// does not correspond to the status of the current instance
type tokenVerificationError struct {
	msg          string
	retryable    bool
	tokenContent *utils.PgControldataTokenContent
}

// Error implements the error interface
func (e *tokenVerificationError) Error() string {
	return e.msg
}

// IsRetryable is true when this condition is temporary
// and the calling code is expected to retry this
// operator in the future
func (e *tokenVerificationError) IsRetryable() bool {
	return e.retryable
}

// Assuming this PostgreSQL instance is a replica and we have a shutdown token
// to wait before promoting it, we verify it, delaying the promotion if the
// token conditions are not met
func (r *InstanceReconciler) verifyShutdownToken(cluster *apiv1.Cluster) error {
	// If there's no replica cluster configuration there's no
	// shutdown token too, so we don't need to wait.
	if cluster.Spec.ReplicaCluster == nil {
		return nil
	}

	// If we don't have a shutdown token, we don't need to wait
	if len(cluster.Spec.ReplicaCluster.Token) == 0 {
		return nil
	}

	// If the current token was already used, there's no need to
	// verify it again
	if cluster.Spec.ReplicaCluster.Token == cluster.Status.LastPromotionToken {
		return nil
	}

	shutdownToken, err := utils.ParsePgControldataToken(cluster.Spec.ReplicaCluster.Token)
	if err != nil {
		// The shutdown token is not correct, and the webhook should
		// have prevented this to happen. If we're here, two things
		// could have happened:
		//
		// 1. we've a bug in the webhook
		// 2. the user didn't install the webhook
		//
		// We don't have any other possibility than raising this error.
		// It will be written in the log of the instance manager
		return fmt.Errorf("while decoding the shutdown token: %w", err)
	}

	if err := shutdownToken.IsValid(); err != nil {
		// The shutdown token is not valid, and the webhook should
		// have prevented this to happen. This is the same case as
		// the previous check
		return fmt.Errorf("while validating the shutdown token: %w", err)
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
		return fmt.Errorf("while verifying the shutdown token [pg_controldata]: %w", err)
	}

	parsedControlData := utils.ParsePgControldataOutput(out)
	currentTimelineIDString := parsedControlData[utils.PgControlDataKeyLatestCheckpointTimelineID]
	currentSystemIdentifier := parsedControlData[utils.PgControlDataKeyDatabaseSystemIdentifier]
	replayLSNString := parsedControlData[utils.PgControlDataKeyLatestCheckpointREDOLocation]

	// If the token belongs to a different database, we cannot use if
	if shutdownToken.DatabaseSystemIdentifier != currentSystemIdentifier {
		return &tokenVerificationError{
			msg: fmt.Sprintf("mismatching system identifiers, current:%s wanted:%s",
				currentSystemIdentifier, shutdownToken.DatabaseSystemIdentifier),
			retryable:    false,
			tokenContent: shutdownToken,
		}
	}

	// If we're in a different timeline, we should definitely wait
	// for this replica to be in the same timeline as the old primary
	shutdownTokenTimeline, err := strconv.Atoi(shutdownToken.LatestCheckpointTimelineID)
	if err != nil {
		return &tokenVerificationError{
			msg: fmt.Sprintf("shutdown token timeline is not an integer: %s (%s)",
				shutdownToken.LatestCheckpointTimelineID, err.Error()),
			retryable:    false,
			tokenContent: shutdownToken,
		}
	}

	currentTimelineID, err := strconv.Atoi(currentTimelineIDString)
	if err != nil {
		return &tokenVerificationError{
			msg: fmt.Sprintf("current timeline is not an integer: %s (%s)",
				currentTimelineIDString, err.Error()),
			retryable:    false,
			tokenContent: shutdownToken,
		}
	}

	switch {
	case shutdownTokenTimeline > currentTimelineID:
		return &tokenVerificationError{
			msg: fmt.Sprintf("requested timeline not reached, current:%d wanted:%d",
				currentTimelineID, shutdownTokenTimeline),
			retryable:    true,
			tokenContent: shutdownToken,
		}

	case shutdownTokenTimeline < currentTimelineID:
		return &tokenVerificationError{
			msg: fmt.Sprintf("requested timeline is older than current one, current:%d wanted:%d",
				currentTimelineID, shutdownTokenTimeline),
			retryable:    false,
			tokenContent: shutdownToken,
		}
	}

	shutdownTokenLSNString := shutdownToken.LatestCheckpointREDOLocation
	shutdownTokenLSN, err := postgres.LSN(shutdownTokenLSNString).Parse()
	if err != nil {
		return &tokenVerificationError{
			msg:          fmt.Sprintf("shutdown token LSN is invalid: %s", shutdownToken.LatestCheckpointREDOLocation),
			retryable:    false,
			tokenContent: shutdownToken,
		}
	}

	replayLSN, err := postgres.LSN(replayLSNString).Parse()
	if err != nil {
		return &tokenVerificationError{
			msg:          fmt.Sprintf("last replay LSN is invalid: %s", replayLSNString),
			retryable:    false,
			tokenContent: shutdownToken,
		}
	}

	switch {
	case shutdownTokenLSN < replayLSN:
		return &tokenVerificationError{
			msg: fmt.Sprintf(
				"shutdown token LSN (%s) is older than the last replay LSN (%s)",
				shutdownTokenLSNString, replayLSNString),
			retryable:    false,
			tokenContent: shutdownToken,
		}

	case replayLSN < shutdownTokenLSN:
		return &tokenVerificationError{
			msg: fmt.Sprintf(
				"waiting for shutdown token LSN (%s) to be replayed (the last replayed LSN is %s)",
				shutdownTokenLSNString, replayLSNString),
			retryable:    true,
			tokenContent: shutdownToken,
		}
	}

	return nil
}
