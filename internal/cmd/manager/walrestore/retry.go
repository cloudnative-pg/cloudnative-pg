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

package walrestore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	barmanRestorer "github.com/cloudnative-pg/barman-cloud/pkg/restorer"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrRetryTimeoutReached is returned when the retry loop has exhausted its
// configurable budget without downloading the WAL. The caller is expected to
// translate this into exit code 255 so that PostgreSQL stops log-shipping
// replication instead of promoting itself prematurely.
var ErrRetryTimeoutReached = errors.New("retry timeout reached while restoring WAL")

// ErrTransientRestore marks a wal-restore failure that the retry loop should
// treat as transient and retry. We use an explicit opt-in marker rather than
// "anything not in a sentinel list is transient": the latter would loop on
// genuinely-final errors (cache misses, malformed WAL names, programmer
// errors), blocking PostgreSQL for the entire retry budget on each
// invocation. Old behavior — return exit 1, let PostgreSQL fall back to
// streaming — is the safer default; opt into retries only when we have a
// positive signal that the failure is recoverable.
var ErrTransientRestore = errors.New("transient WAL restore failure")

// DefaultMaxRetryTimeout is the default budget for retrying transient
// WAL-restore failures. After this deadline the command will ask PostgreSQL
// to stop replication (exit 255).
const DefaultMaxRetryTimeout = 5 * time.Minute

// retryBackoffCap is the maximum interval between retry attempts. We start
// from a small interval and grow up to this cap.
//
// Declared as vars (not consts) so tests can shrink them for fast execution.
var retryBackoffCap = 30 * time.Second

// retryBackoffInitial is the interval between the first and the second
// attempt. Subsequent intervals grow exponentially up to retryBackoffCap.
var retryBackoffInitial = 1 * time.Second

// classifyPluginError categorizes an error returned from a CNPG-i plugin.
//
// Only specific gRPC status codes are considered transient — the same ones
// gRPC's own retry machinery treats as retryable. Anything else (Internal,
// InvalidArgument, PermissionDenied, plain non-gRPC errors, ...) maps to
// "Other" and gets the legacy exit-1 treatment so PostgreSQL can fall back
// to streaming replication.
func classifyPluginError(err error) restoreErrorKind {
	if err == nil {
		return restoreErrorNone
	}
	st, ok := status.FromError(err)
	if !ok {
		return restoreErrorOther
	}
	switch st.Code() {
	case codes.NotFound:
		return restoreErrorNotFound
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted:
		return restoreErrorTransient
	}
	return restoreErrorOther
}

// classifyBarmanError categorizes an error returned from the in-tree
// barman-cloud path.
//
// barman-cloud-wal-restore exit codes (per barman documentation):
//   - 0  → success
//   - 1  → bucket or WAL not found (exposed as ErrWALNotFound by the
//     vendored library)
//   - 2  → connectivity failure (the only one whose error message says
//     "retrying" — the only one we treat as transient)
//   - 3  → invalid WAL name (programmer error, not transient)
//   - 4+ → generic / unknown (treated as final to mirror legacy behavior;
//     the legacy code would have surfaced these as exit 1 to PostgreSQL,
//     so PG could fall back to streaming)
//
// We string-match the vendored library's connectivity-failure message
// rather than introducing a sentinel because the library wraps everything
// in fmt.Errorf today; the tighter path would be to add a sentinel
// upstream and consume it here.
func classifyBarmanError(err error) restoreErrorKind {
	if err == nil {
		return restoreErrorNone
	}
	if errors.Is(err, barmanRestorer.ErrWALNotFound) {
		return restoreErrorNotFound
	}
	if strings.Contains(err.Error(), "connectivity failure") {
		return restoreErrorTransient
	}
	return restoreErrorOther
}

// restoreErrorKind categorizes the outcome of a single restore attempt so the
// retry loop can decide whether to exit immediately, retry, or surface a
// timeout.
type restoreErrorKind int

const (
	// restoreErrorNone means the attempt succeeded.
	restoreErrorNone restoreErrorKind = iota
	// restoreErrorNotFound means the WAL is definitively absent. Retrying
	// won't change that, and PostgreSQL's own retry mechanics already cover
	// the log-shipping "advance to streaming" transition.
	restoreErrorNotFound
	// restoreErrorTransient means the attempt failed for a reason that
	// is positively known to be retryable (gRPC Unavailable / DeadlineExceeded
	// / ResourceExhausted / Aborted, or barman exit 2 connectivity failure).
	restoreErrorTransient
	// restoreErrorOther means the attempt failed for a reason we cannot
	// classify as either definitively-not-found or known-transient. We
	// surface this as exit 1 to PostgreSQL — the safe legacy behavior —
	// rather than risk blocking the database for a 5-minute retry budget
	// on a permanent error.
	restoreErrorOther
)

// nextBackoff returns the next retry interval, capped at retryBackoffCap.
func nextBackoff(current time.Duration) time.Duration {
	if current <= 0 {
		return retryBackoffInitial
	}
	next := current * 2
	if next > retryBackoffCap {
		return retryBackoffCap
	}
	return next
}

// attemptFunc is the unit of work the retry loop drives. Decoupling the loop
// from the concrete restore implementation keeps the loop unit-testable.
type attemptFunc func(ctx context.Context) error

// retryUntilDeadline runs attempt repeatedly until it returns a non-transient
// result, the context is canceled, or the deadline is reached. Sleeps between
// attempts grow exponentially up to retryBackoffCap.
//
// Returns:
//   - whatever attempt returned (success or non-transient sentinel) if the
//     loop terminated naturally,
//   - ErrRetryTimeoutReached (wrapping the last transient error) if the
//     deadline was hit or the context was canceled mid-wait.
//
// The first call to attempt is made unconditionally, before any sleep, so
// that a happy first attempt incurs zero retry overhead.
func retryUntilDeadline(
	ctx context.Context,
	attempt attemptFunc,
	deadline time.Time,
) error {
	contextLog := log.FromContext(ctx)

	err := attempt(ctx)
	if !isTransientRestoreError(err) {
		return err
	}

	backoff := nextBackoff(0)
	attemptCount := 1
	for {
		if !time.Now().Add(backoff).Before(deadline) {
			return fmt.Errorf("%w after %d attempt(s): last error: %w",
				ErrRetryTimeoutReached, attemptCount, err)
		}
		contextLog.Info("transient WAL restore error, will retry",
			"attempt", attemptCount, "nextRetryIn", backoff, "deadline", deadline,
			"error", err)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return fmt.Errorf("%w: context canceled while retrying: %w: %w",
				ErrRetryTimeoutReached, ctx.Err(), err)
		}
		attemptCount++
		err = attempt(ctx)
		if !isTransientRestoreError(err) {
			return err
		}
		backoff = nextBackoff(backoff)
	}
}
