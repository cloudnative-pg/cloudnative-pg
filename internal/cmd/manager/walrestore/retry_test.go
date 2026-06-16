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
	"sync/atomic"
	"time"

	barmanRestorer "github.com/cloudnative-pg/barman-cloud/pkg/restorer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("isTransientPluginError", func() {
	DescribeTable("only specific gRPC codes are transient",
		func(err error, expected bool) {
			Expect(isTransientPluginError(err)).To(Equal(expected))
		},
		Entry("nil is not transient", nil, false),
		Entry("gRPC NotFound is not transient",
			status.Error(codes.NotFound, "wal not in archive"), false),
		Entry("NotFound through fmt.Errorf %w is still not transient",
			fmt.Errorf("plugin foo: %w", status.Error(codes.NotFound, "x")), false),
		Entry("Unavailable is transient",
			status.Error(codes.Unavailable, "dial tcp"), true),
		Entry("DeadlineExceeded is transient",
			status.Error(codes.DeadlineExceeded, "deadline"), true),
		Entry("ResourceExhausted is transient",
			status.Error(codes.ResourceExhausted, "rate-limited"), true),
		Entry("Aborted is transient",
			status.Error(codes.Aborted, "concurrency"), true),
		// Crucial: an unclassified gRPC error must NOT be treated as
		// transient — that would tie up PostgreSQL on a permanent error.
		Entry("Internal is not transient",
			status.Error(codes.Internal, "boom"), false),
		Entry("InvalidArgument is not transient",
			status.Error(codes.InvalidArgument, "bad arg"), false),
		Entry("PermissionDenied is not transient",
			status.Error(codes.PermissionDenied, "forbidden"), false),
		Entry("a plain (non-gRPC) error is not transient",
			errors.New("something blew up"), false),
		// Anti-regression: we must not pattern-match on the message.
		Entry("'not found' text without a gRPC status is not transient",
			errors.New("the wal file is not found"), false),
	)
})

var _ = Describe("isTransientBarmanError", func() {
	DescribeTable("ErrConnectivity and ErrGeneric are transient; everything else is final",
		func(err error, expected bool) {
			Expect(isTransientBarmanError(err)).To(Equal(expected))
		},
		Entry("nil is not transient", nil, false),
		// Transient: matches plugin-barman-cloud#927 — connectivity blips
		// and the generic exit-4 bucket are both worth retrying.
		Entry("ErrConnectivity (wrapped) is transient",
			fmt.Errorf("ctx: %w", barmanRestorer.ErrConnectivity), true),
		Entry("ErrGeneric (wrapped) is transient",
			fmt.Errorf("ctx: %w", barmanRestorer.ErrGeneric), true),
		// Terminal sentinels.
		Entry("ErrWALNotFound is not transient",
			fmt.Errorf("ctx: %w", barmanRestorer.ErrWALNotFound), false),
		Entry("ErrInvalidWalName is not transient",
			fmt.Errorf("ctx: %w", barmanRestorer.ErrInvalidWalName), false),
		Entry("ErrUnrecognizedExitCode is not transient",
			fmt.Errorf("ctx: %w", barmanRestorer.ErrUnrecognizedExitCode), false),
		// Anti-regression: we must not pattern-match on the message.
		Entry("plain 'connectivity failure' string without a sentinel is not transient",
			errors.New("connectivity failure while executing barman-cloud-wal-restore, retrying"),
			false),
		Entry("unrelated plain error is not transient",
			errors.New("some unexpected failure"), false),
	)
})

var _ = Describe("isTransientRestoreError", func() {
	DescribeTable("only ErrTransientRestore-marked errors are transient",
		func(err error, expected bool) {
			Expect(isTransientRestoreError(err)).To(Equal(expected))
		},
		Entry("nil is not transient", nil, false),
		Entry("ErrWALNotFound is not transient", barmanRestorer.ErrWALNotFound, false),
		Entry("wrapped ErrWALNotFound is not transient",
			fmt.Errorf("ctx: %w", barmanRestorer.ErrWALNotFound), false),
		Entry("ErrNoBackupConfigured is not transient", ErrNoBackupConfigured, false),
		Entry("ErrEndOfWALStreamReached is not transient", ErrEndOfWALStreamReached, false),
		Entry("ErrExternalClusterNotFound is not transient", ErrExternalClusterNotFound, false),
		Entry("ErrRetryTimeoutReached is not transient (loop terminus)",
			ErrRetryTimeoutReached, false),
		// The crucial change: a generic error is NOT transient. Old design
		// retried for 5 minutes on every unclassified error, blocking PG.
		Entry("a generic error is NOT transient (legacy exit-1 path)",
			errors.New("oops"), false),
		Entry("ErrTransientRestore IS transient", ErrTransientRestore, true),
		Entry("wrapped ErrTransientRestore IS transient",
			fmt.Errorf("ctx: %w", ErrTransientRestore), true),
	)
})

var _ = Describe("resolveMaxRetryTimeout", func() {
	clusterWith := func(d *metav1.Duration) *apiv1.Cluster {
		return &apiv1.Cluster{Spec: apiv1.ClusterSpec{WalRestoreRetryTimeout: d}}
	}

	DescribeTable("picks the retry budget",
		func(cluster *apiv1.Cluster, expected time.Duration) {
			Expect(resolveMaxRetryTimeout(cluster)).To(Equal(expected))
		},
		Entry("nil cluster → default", nil, DefaultMaxRetryTimeout),
		Entry("unset field → default", clusterWith(nil), DefaultMaxRetryTimeout),
		Entry("positive duration is honored",
			clusterWith(&metav1.Duration{Duration: 42 * time.Second}), 42*time.Second),
		// Zero and negative both hit the defense-in-depth branch; one
		// entry per polarity is enough to catch regressions in that guard.
		Entry("zero → default (defense in depth — webhook rejects this)",
			clusterWith(&metav1.Duration{Duration: 0}), DefaultMaxRetryTimeout),
		Entry("negative → default (defense in depth)",
			clusterWith(&metav1.Duration{Duration: -5 * time.Minute}), DefaultMaxRetryTimeout),
	)
})

var _ = Describe("nextBackoff", func() {
	DescribeTable("doubles each step up to the cap",
		func(in, expected time.Duration) {
			Expect(nextBackoff(in)).To(Equal(expected))
		},
		Entry("zero bootstraps to the initial value", time.Duration(0), retryBackoffInitial),
		Entry("negative bootstraps to the initial value", -1*time.Second, retryBackoffInitial),
		Entry("doubles when below the cap", 4*time.Second, 8*time.Second),
		Entry("saturates at the cap", retryBackoffCap, retryBackoffCap),
		Entry("does not overflow past the cap", 2*retryBackoffCap, retryBackoffCap),
	)
})

var _ = Describe("resolveNoBarmanError", func() {
	cluster := &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "c"}}

	It("wraps config errors that aren't ErrNoBackupConfigured", func() {
		original := errors.New("kapow")
		err := resolveNoBarmanError(context.Background(), cluster, "wal", nil, original)
		Expect(errors.Is(err, original)).To(BeTrue())
		Expect(errors.Is(err, ErrNoBackupConfigured)).To(BeFalse())
		Expect(isTransientRestoreError(err)).To(BeFalse())
	})

	It("returns ErrNoBackupConfigured only when there was no plugin attempt", func() {
		// With pluginErr == nil there is genuinely nothing configured — the
		// caller's "tried restoring WALs, but no backup was configured"
		// debug log applies. PostgreSQL sees exit 1 and falls back to
		// streaming.
		err := resolveNoBarmanError(context.Background(), cluster, "wal",
			nil, ErrNoBackupConfigured)
		Expect(errors.Is(err, ErrNoBackupConfigured)).To(BeTrue())
		Expect(isTransientRestoreError(err)).To(BeFalse())
	})

	It("surfaces the plugin error for non-transient plugin failures (no barman fallback)", func() {
		// A non-transient plugin failure must NOT be masked as "backup not
		// configured": backup IS configured (via the plugin), it just
		// failed. The plugin error is surfaced so operators can see the
		// real reason; PostgreSQL still gets exit 1 (no transient marker)
		// and falls back to streaming.
		pluginErr := errors.New("plugin boom")
		err := resolveNoBarmanError(context.Background(), cluster, "wal",
			pluginErr, ErrNoBackupConfigured)
		Expect(errors.Is(err, pluginErr)).To(BeTrue())
		Expect(errors.Is(err, ErrNoBackupConfigured)).To(BeFalse())
		Expect(isTransientRestoreError(err)).To(BeFalse())
	})

	It("opts into retry when the plugin had a known-transient error and no barman fallback", func() {
		// The bug fix: a known-transient plugin error must not be silently
		// downgraded to "no backup configured" → exit 1, which would let
		// PostgreSQL promote on a partial archive.
		pluginErr := status.Error(codes.Unavailable, "dial tcp")
		err := resolveNoBarmanError(context.Background(), cluster, "wal",
			pluginErr, ErrNoBackupConfigured)
		Expect(errors.Is(err, ErrNoBackupConfigured)).To(BeFalse())
		Expect(errors.Is(err, pluginErr)).To(BeTrue())
		Expect(isTransientRestoreError(err)).To(BeTrue())
	})
})

var _ = Describe("combineBarmanFailureWithPluginContext", func() {
	It("returns the barman error untouched when neither path is transient", func() {
		err := combineBarmanFailureWithPluginContext(nil, barmanRestorer.ErrWALNotFound)
		Expect(errors.Is(err, barmanRestorer.ErrWALNotFound)).To(BeTrue())
		Expect(isTransientRestoreError(err)).To(BeFalse())
	})

	It("does NOT opt into retry for a terminal barman sentinel (legacy exit-1)", func() {
		// Critical regression guard: ErrInvalidWalName / ErrUnrecognizedExitCode
		// must surface unwrapped so RunE returns exit 1 and PostgreSQL falls
		// back to streaming. The first-cut design retried for 5 minutes
		// here, blocking PG on every WAL replay.
		barmanErr := fmt.Errorf("ctx: %w", barmanRestorer.ErrInvalidWalName)
		err := combineBarmanFailureWithPluginContext(nil, barmanErr)
		Expect(err).To(Equal(barmanErr))
		Expect(isTransientRestoreError(err)).To(BeFalse())
	})

	It("opts into retry when plugin reports a known-transient error", func() {
		pluginErr := status.Error(codes.Unavailable, "blip")
		barmanErr := fmt.Errorf("ctx: %w", barmanRestorer.ErrInvalidWalName)
		err := combineBarmanFailureWithPluginContext(pluginErr, barmanErr)
		Expect(isTransientRestoreError(err)).To(BeTrue())
	})

	It("opts into retry when barman reports a connectivity failure (exit 2)", func() {
		barmanErr := fmt.Errorf("ctx: %w", barmanRestorer.ErrConnectivity)
		err := combineBarmanFailureWithPluginContext(nil, barmanErr)
		Expect(isTransientRestoreError(err)).To(BeTrue())
	})

	It("opts into retry when barman reports a generic exit-4 failure", func() {
		barmanErr := fmt.Errorf("ctx: %w", barmanRestorer.ErrGeneric)
		err := combineBarmanFailureWithPluginContext(nil, barmanErr)
		Expect(isTransientRestoreError(err)).To(BeTrue())
	})

	It("breaks the NotFound chain when the plugin was transient", func() {
		// Subtle contract: when the plugin had a transient error and barman
		// reports NotFound, we cannot trust the NotFound (the plugin may
		// still hold the WAL on a successful retry). The wrapped error must
		// NOT be errors.Is-detectable as ErrWALNotFound, otherwise the loop
		// would treat it as final and skip retrying.
		pluginErr := status.Error(codes.Unavailable, "blip")
		err := combineBarmanFailureWithPluginContext(pluginErr, barmanRestorer.ErrWALNotFound)
		Expect(errors.Is(err, barmanRestorer.ErrWALNotFound)).To(BeFalse())
		Expect(isTransientRestoreError(err)).To(BeTrue())
	})
})

var _ = Describe("retryUntilDeadline", func() {
	// Shrink the backoff knobs for the duration of these tests so the
	// loop runs in milliseconds. The specific production schedule is
	// already covered by nextBackoff's own test.
	var (
		origInitial time.Duration
		origCap     time.Duration
	)

	BeforeEach(func() {
		origInitial = retryBackoffInitial
		origCap = retryBackoffCap
		retryBackoffInitial = 2 * time.Millisecond
		retryBackoffCap = 10 * time.Millisecond
	})

	AfterEach(func() {
		retryBackoffInitial = origInitial
		retryBackoffCap = origCap
	})

	It("returns success after exactly one attempt", func() {
		var calls int32
		err := retryUntilDeadline(
			context.Background(),
			func(_ context.Context) error {
				atomic.AddInt32(&calls, 1)
				return nil
			},
			time.Now().Add(1*time.Second),
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(atomic.LoadInt32(&calls)).To(Equal(int32(1)))
	})

	It("returns NotFound immediately without retrying", func() {
		var calls int32
		err := retryUntilDeadline(
			context.Background(),
			func(_ context.Context) error {
				atomic.AddInt32(&calls, 1)
				return barmanRestorer.ErrWALNotFound
			},
			time.Now().Add(1*time.Second),
		)
		Expect(errors.Is(err, barmanRestorer.ErrWALNotFound)).To(BeTrue())
		Expect(atomic.LoadInt32(&calls)).To(Equal(int32(1)))
	})

	It("does NOT retry an unmarked error (legacy exit-1 path)", func() {
		// Critical regression guard: a generic error must surface unwrapped
		// after exactly one attempt. The first design retried these for 5
		// minutes on every wal-restore invocation, blocking PG.
		var calls int32
		boom := errors.New("oops")
		err := retryUntilDeadline(
			context.Background(),
			func(_ context.Context) error {
				atomic.AddInt32(&calls, 1)
				return boom
			},
			time.Now().Add(1*time.Second),
		)
		Expect(err).To(Equal(boom))
		Expect(atomic.LoadInt32(&calls)).To(Equal(int32(1)))
	})

	It("retries ErrTransientRestore-marked errors until a success is returned", func() {
		var calls int32
		err := retryUntilDeadline(
			context.Background(),
			func(_ context.Context) error {
				if atomic.AddInt32(&calls, 1) < 3 {
					return fmt.Errorf("blip: %w", ErrTransientRestore)
				}
				return nil
			},
			time.Now().Add(1*time.Second),
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(atomic.LoadInt32(&calls)).To(Equal(int32(3)))
	})

	It("surfaces ErrRetryTimeoutReached wrapping the last transient error when the deadline is hit", func() {
		lastErr := fmt.Errorf("flaky bucket: %w", ErrTransientRestore)
		err := retryUntilDeadline(
			context.Background(),
			func(_ context.Context) error { return lastErr },
			time.Now().Add(20*time.Millisecond),
		)
		Expect(errors.Is(err, ErrRetryTimeoutReached)).To(BeTrue())
		Expect(errors.Is(err, lastErr)).To(BeTrue())
	})

	It("honors a NotFound outcome that arrives mid-retry", func() {
		var calls int32
		err := retryUntilDeadline(
			context.Background(),
			func(_ context.Context) error {
				if atomic.AddInt32(&calls, 1) == 1 {
					return fmt.Errorf("one transient: %w", ErrTransientRestore)
				}
				return barmanRestorer.ErrWALNotFound
			},
			time.Now().Add(1*time.Second),
		)
		Expect(errors.Is(err, barmanRestorer.ErrWALNotFound)).To(BeTrue())
		Expect(atomic.LoadInt32(&calls)).To(Equal(int32(2)))
	})

	It("maps context cancellation to ErrRetryTimeoutReached", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := retryUntilDeadline(
			ctx,
			func(_ context.Context) error {
				return fmt.Errorf("transient: %w", ErrTransientRestore)
			},
			time.Now().Add(1*time.Second),
		)
		Expect(errors.Is(err, ErrRetryTimeoutReached)).To(BeTrue())
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
	})

	It("always makes at least one attempt, even with an already-past deadline", func() {
		var calls int32
		err := retryUntilDeadline(
			context.Background(),
			func(_ context.Context) error {
				atomic.AddInt32(&calls, 1)
				return fmt.Errorf("transient: %w", ErrTransientRestore)
			},
			time.Now().Add(-1*time.Second),
		)
		Expect(errors.Is(err, ErrRetryTimeoutReached)).To(BeTrue())
		Expect(atomic.LoadInt32(&calls)).To(Equal(int32(1)))
	})
})
