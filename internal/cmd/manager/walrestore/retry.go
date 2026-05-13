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
	"time"

	barmanRestorer "github.com/cloudnative-pg/barman-cloud/pkg/restorer"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrRetryTimeoutReached is returned when the retry loop has exhausted its
// configurable budget without downloading the WAL.
var ErrRetryTimeoutReached = errors.New("retry timeout reached while restoring WAL")

// retryTimeoutError carries the last attempt error captured by the retry
// loop alongside the ErrRetryTimeoutReached sentinel. We use a typed
// error instead of fmt.Errorf("%w ... %w ...") because that older wrapping
// form makes errors.Unwrap (single-return) return nil, hiding the last
// attempt error from callers that want to surface it. The Unwrap() []error
// method keeps errors.Is matching ErrRetryTimeoutReached, the cause (if
// any), and lastErr — preserving the previous contract.
type retryTimeoutError struct {
	attemptCount int
	// cause is set to ctx.Err() when the context was canceled mid-wait,
	// nil when the deadline was simply hit between attempts.
	cause   error
	lastErr error
}

func (e *retryTimeoutError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%v: context canceled while retrying: %v: %v",
			ErrRetryTimeoutReached, e.cause, e.lastErr)
	}
	return fmt.Sprintf("%v after %d attempt(s): last error: %v",
		ErrRetryTimeoutReached, e.attemptCount, e.lastErr)
}

func (e *retryTimeoutError) Unwrap() []error {
	out := []error{ErrRetryTimeoutReached}
	if e.cause != nil {
		out = append(out, e.cause)
	}
	if e.lastErr != nil {
		out = append(out, e.lastErr)
	}
	return out
}

// LastError extracts the last attempt error captured by retryUntilDeadline
// when the retry budget was exhausted. Returns nil if err is not a
// retry-timeout error produced by this package.
func LastError(err error) error {
	var rt *retryTimeoutError
	if errors.As(err, &rt) {
		return rt.lastErr
	}
	return nil
}

// ErrTransientRestore marks a wal-restore failure that the retry loop should
// treat as transient and retry. We use an explicit opt-in marker rather than
// "anything not in a sentinel list is transient": the latter would loop on
// genuinely-final errors (cache misses, malformed WAL names, programmer
// errors), blocking PostgreSQL for the entire retry budget on each
// invocation. Opt into retries only when we have a positive signal that the
// failure is recoverable.
var ErrTransientRestore = errors.New("transient WAL restore failure")

// DefaultMaxRetryTimeout is the default budget for retrying transient
// WAL-restore failures.
const DefaultMaxRetryTimeout = 5 * time.Minute

// retryBackoffCap is the maximum interval between retry attempts. We start
// from a small interval and grow up to this cap.
//
// Declared as vars (not consts) so tests can shrink them for fast execution.
var retryBackoffCap = 30 * time.Second

// retryBackoffInitial is the interval between the first and the second
// attempt. Subsequent intervals grow exponentially up to retryBackoffCap.
var retryBackoffInitial = 1 * time.Second

// isTransientPluginError reports whether an error returned from a CNPG-i
// plugin should be treated as transient (= retry-worthy).
//
// Only specific gRPC status codes count — the same ones gRPC's own retry
// machinery treats as retryable. Anything else (Internal, InvalidArgument,
// PermissionDenied, NotFound, plain non-gRPC errors, ...) is considered
// final and gets the legacy exit-1 treatment so PostgreSQL can fall back
// to streaming replication. The design philosophy is "opt into retries
// only on a positive signal": retrying on permanent errors would tie up
// PostgreSQL for the entire retry budget on every WAL replay.
func isTransientPluginError(err error) bool {
	if err == nil {
		return false
	}
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted:
		return true
	}
	return false
}

// isTransientBarmanError reports whether an error returned from the in-tree
// barman-cloud path should be treated as transient (= retry-worthy).
//
// Mirrors the classification chosen by the barman-cloud CNPG-i plugin
// (cloudnative-pg/plugin-barman-cloud#927), mapped onto the vendored
// library's sentinels:
//
//   - ErrConnectivity (exit 2)    → transient (Unavailable in the plugin)
//   - ErrGeneric     (exit 4)     → transient (Unavailable in the plugin)
//   - ErrWALNotFound (exit 1)     → terminal  (NotFound)
//   - ErrInvalidWalName (exit 3)  → terminal  (InvalidArgument)
//   - anything else (unrecognized exit code, command-execution failure) →
//     terminal (Internal)
//
// Keeping the two paths in lockstep means a plugin-only setup and an
// in-tree barman-only setup retry on the same exit-code surfaces.
func isTransientBarmanError(err error) bool {
	return errors.Is(err, barmanRestorer.ErrConnectivity) ||
		errors.Is(err, barmanRestorer.ErrGeneric)
}

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
			return &retryTimeoutError{attemptCount: attemptCount, lastErr: err}
		}
		contextLog.Info("transient WAL restore error, will retry",
			"attempt", attemptCount, "nextRetryIn", backoff, "deadline", deadline,
			"error", err)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return &retryTimeoutError{attemptCount: attemptCount, cause: ctx.Err(), lastErr: err}
		}
		attemptCount++
		err = attempt(ctx)
		if !isTransientRestoreError(err) {
			return err
		}
		backoff = nextBackoff(backoff)
	}
}
