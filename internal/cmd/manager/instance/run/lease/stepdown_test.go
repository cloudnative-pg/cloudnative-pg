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

package lease

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// recordingLock is a resource lock whose writes succeed (or fail on demand)
// without touching an API server, so the confirmingLock decorator can be
// exercised on its own.
type recordingLock struct {
	identity string
	failWith error
}

func (l *recordingLock) Get(context.Context) (*resourcelock.LeaderElectionRecord, []byte, error) {
	return &resourcelock.LeaderElectionRecord{HolderIdentity: l.identity}, nil, l.failWith
}

func (l *recordingLock) Create(context.Context, resourcelock.LeaderElectionRecord) error {
	return l.failWith
}

func (l *recordingLock) Update(context.Context, resourcelock.LeaderElectionRecord) error {
	return l.failWith
}

func (*recordingLock) RecordEvent(string) {}
func (l *recordingLock) Identity() string { return l.identity }
func (*recordingLock) Describe() string   { return "recordingLock" }

var _ = Describe("confirmingLock", func() {
	const ourIdentity = "test-cluster-1"

	var (
		stamp   *atomic.Int64
		inner   *recordingLock
		decided *confirmingLock
	)

	BeforeEach(func() {
		stamp = &atomic.Int64{}
		inner = &recordingLock{identity: ourIdentity}
		decided = &confirmingLock{Interface: inner, lastConfirmed: stamp}
	})

	ourRecord := resourcelock.LeaderElectionRecord{
		HolderIdentity: ourIdentity,
		RenewTime:      metav1.NewTime(time.Now()),
	}

	It("stamps a successful write that names us", func(ctx context.Context) {
		Expect(decided.Update(ctx, ourRecord)).To(Succeed())
		Expect(stamp.Load()).NotTo(BeZero())
	})

	It("stamps a successful create that names us", func(ctx context.Context) {
		Expect(decided.Create(ctx, ourRecord)).To(Succeed())
		Expect(stamp.Load()).NotTo(BeZero())
	})

	It("does not stamp a failed write", func(ctx context.Context) {
		inner.failWith = errors.New("api server unreachable")

		Expect(decided.Update(ctx, ourRecord)).NotTo(Succeed())
		Expect(stamp.Load()).To(BeZero())
	})

	It("does not stamp a write that clears the holder, which is a release", func(ctx context.Context) {
		Expect(decided.Update(ctx, resourcelock.LeaderElectionRecord{
			RenewTime: metav1.NewTime(time.Now()),
		})).To(Succeed())
		Expect(stamp.Load()).To(BeZero())
	})

	It("does not stamp a write that names another holder", func(ctx context.Context) {
		Expect(decided.Update(ctx, resourcelock.LeaderElectionRecord{
			HolderIdentity: "test-cluster-2",
			RenewTime:      metav1.NewTime(time.Now()),
		})).To(Succeed())
		Expect(stamp.Load()).To(BeZero())
	})

	It("does not stamp a successful read, even one that names us", func(ctx context.Context) {
		// A read proves the record still names us, not that it is fresh. This is
		// the distinction the whole staleness measurement rests on.
		_, _, err := decided.Get(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(stamp.Load()).To(BeZero())
	})
})

// readCommands mounts a blocking reader on the instance command channel and
// returns the commands it picks up. A real blocking receive is required: the
// monitor's send is non-blocking, so it can only meet a receiver already
// waiting on the channel, and polling the channel (as Eventually does when
// handed one directly) would never see the command. The reader touches only
// locals so it does not race with the next spec replacing the Runnable.
func readCommands(r *Runnable) chan postgres.InstanceCommand {
	commands := r.instance.GetInstanceCommandChan()
	received := make(chan postgres.InstanceCommand, 2)
	go func() {
		for command := range commands {
			received <- command
		}
	}()

	return received
}

var _ = Describe("Runnable.monitorLeaseAge", func() {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		thisPod     = "test-cluster-1"
	)

	// The monitor seeds the stamp to "now" when it starts, so with a renew
	// deadline this short the lease goes stale a few ticks in without anything
	// having to fake a clock.
	const (
		retryPeriod   = 5 * time.Millisecond
		renewDeadline = 25 * time.Millisecond
	)

	var (
		r      *Runnable
		checks *atomic.Int32
	)

	BeforeEach(func() {
		instance := postgres.NewInstance().
			WithNamespace(namespace).
			WithPodName(thisPod).
			WithClusterName(clusterName)
		r = New(fake.NewClientset(), instance)
		r.config.RetryPeriod = retryPeriod
		r.config.RenewDeadline = renewDeadline
		checks = &atomic.Int32{}
	})

	// verdict installs a step-down check that counts its calls.
	verdict := func(stepDown bool, reason error) {
		r.checkStepDown = func(*apiv1.Cluster, string) (bool, error) {
			checks.Add(1)
			return stepDown, reason
		}
	}

	// start runs the monitor for the duration of the spec.
	start := func(ctx context.Context) {
		monitorCtx, cancel := context.WithCancel(ctx)
		DeferCleanup(cancel)
		go r.monitorLeaseAge(monitorCtx)
	}

	It("does nothing while the lease is still fresh", func(ctx context.Context) {
		r.config.RenewDeadline = time.Hour
		verdict(true, nil)

		start(ctx)

		Consistently(checks.Load, 100*time.Millisecond).Should(BeEquivalentTo(0))
	})

	It("does not act on a zero stamp: the monitor seeds it when it starts", func(ctx context.Context) {
		// Re-adopting our own lease across an in-place upgrade writes nothing, so
		// without the seeding the very first tick would see an age of decades.
		r.config.RenewDeadline = time.Hour
		r.lastConfirmed.Store(0)
		verdict(true, nil)

		start(ctx)

		Consistently(checks.Load, 100*time.Millisecond).Should(BeEquivalentTo(0))
	})

	It("requests an immediate shutdown once the lease is stale and the peers agree", func(ctx context.Context) {
		verdict(true, nil)
		received := readCommands(r)

		start(ctx)

		Eventually(received).Should(Receive(Equal(postgres.InstanceCommand("ShutDownImmediate"))))
	})

	It("keeps running without requesting a shutdown when the peers say this is still the primary",
		func(ctx context.Context) {
			verdict(false, nil)
			// A reader is mounted, so a shutdown request would be observed. This is
			// what separates "the send was declined" from "no send was attempted".
			received := readCommands(r)

			start(ctx)

			// The check does run, repeatedly: the condition can still develop.
			Eventually(checks.Load).Should(BeNumerically(">", 1))
			Expect(received).NotTo(Receive())
		})

	It("fails open when the step-down check itself errors", func(ctx context.Context) {
		verdict(false, errors.New("failed to read CA certificate"))
		received := readCommands(r)

		start(ctx)

		Eventually(checks.Load).Should(BeNumerically(">", 1))
		Expect(received).NotTo(Receive())
	})

	It("evaluates the step-down condition only once, then retries the delivery alone",
		func(ctx context.Context) {
			// No reader on the command channel: every send is declined, exactly as
			// it is while the lifecycle manager runs an earlier command.
			verdict(true, nil)

			start(ctx)

			// Several ticks worth of declined sends, and the verdict is not
			// re-derived: re-reading the CA and rebuilding the TLS stack on every
			// tick would be pure waste for a conclusion that cannot reverse.
			Eventually(checks.Load).Should(BeEquivalentTo(1))
			Consistently(checks.Load, 100*time.Millisecond).Should(BeEquivalentTo(1))

			// The monitor is still alive and delivers as soon as a reader appears.
			// The reader has to be a real blocking receive: a non-blocking send
			// can only meet a receiver already waiting on the channel, so polling
			// it (as Eventually does on a channel) would never see the command and
			// a declined send would be indistinguishable from one never attempted.
			received := readCommands(r)

			Eventually(received).Should(Receive(Equal(postgres.InstanceCommand("ShutDownImmediate"))))

			// And exactly once: the real consumer exits after handling a shutdown,
			// so a second send would park the monitor forever. The reader above is
			// still waiting, so a second command would be picked up here.
			Consistently(received, 100*time.Millisecond).ShouldNot(Receive())
		})

	It("stays responsive while the shutdown request cannot be delivered", func(ctx context.Context) {
		// With nothing reading the command channel, an unconditional send would
		// park this goroutine inside the send and it would never see the
		// cancellation. That a declined send leaves the monitor free to return is
		// the only externally observable difference between declining and
		// blocking, so it is what this asserts.
		verdict(true, nil)
		monitorCtx, cancel := context.WithCancel(ctx)
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			r.monitorLeaseAge(monitorCtx)
		}()

		Eventually(checks.Load).Should(BeEquivalentTo(1))
		cancel()

		Eventually(stopped).Should(BeClosed())
	})

	It("stops when its context is cancelled", func(ctx context.Context) {
		verdict(false, nil)
		monitorCtx, cancel := context.WithCancel(ctx)
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			r.monitorLeaseAge(monitorCtx)
		}()

		Eventually(checks.Load).Should(BeNumerically(">", 0))
		cancel()

		Eventually(stopped).Should(BeClosed())
	})
})
