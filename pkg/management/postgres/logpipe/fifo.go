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

package logpipe

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/fileutils/compatibility"
	"github.com/cloudnative-pg/machinery/pkg/log"
)

// retryBackoff is the delay observed by the log pipe loops between a failed
// attempt (directory missing, FIFO creation, or log streaming) and the next
// one, to avoid busy-spinning and flooding the logs when the error persists.
const retryBackoff = time.Second

// ensureLogFifo makes sure a FIFO exists at fileName, creating it if needed.
// If a filesystem entry already exists at that path but isn't a FIFO (e.g. a
// wal-archive/wal-restore invocation created a regular file there before the
// reader had a chance to create the FIFO), it is removed so the FIFO can be
// created on a following call. The removal is racy: an in-flight writer
// keeping the old inode open loses its buffered lines, but the next writer
// invocation opens the freshly created FIFO. This is convergent and
// acceptable, and is not something we try to eliminate further.
func ensureLogFifo(logger log.Logger, fileName string) error {
	err := compatibility.CreateFifo(fileName)
	if err == nil {
		return nil
	}

	if errors.Is(err, compatibility.ErrExistsNotFifo) {
		logger.Warning("Log FIFO path held by a non-FIFO entry; removing it to restore log streaming")
		if rmErr := os.Remove(fileName); rmErr != nil {
			logger.Error(rmErr, "Failed to remove stale non-FIFO log path")
		}
		return err
	}

	logger.Error(err, "Error creating log FIFO")
	return err
}

// waitBeforeRetry pauses for retryBackoff before a loop retries after an
// error, returning early if the context is cancelled in the meantime.
func waitBeforeRetry(ctx context.Context) {
	timer := time.NewTimer(retryBackoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
