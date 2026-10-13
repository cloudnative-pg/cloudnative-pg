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

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/utils/ptr"

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
	// delay is how long a write takes before it returns, standing for a slow
	// response from the API server.
	delay time.Duration
}

func (l *recordingLock) Get(context.Context) (*resourcelock.LeaderElectionRecord, []byte, error) {
	return &resourcelock.LeaderElectionRecord{HolderIdentity: l.identity}, nil, l.failWith
}

func (l *recordingLock) Create(context.Context, resourcelock.LeaderElectionRecord) error {
	time.Sleep(l.delay)
	return l.failWith
}

func (l *recordingLock) Update(context.Context, resourcelock.LeaderElectionRecord) error {
	time.Sleep(l.delay)
	return l.failWith
}

func (*recordingLock) RecordEvent(string) {}
func (l *recordingLock) Identity() string { return l.identity }
func (*recordingLock) Describe() string   { return "recordingLock" }

var _ = Describe("confirmingLock", func() {
	const ourIdentity = "test-cluster-1"

	var (
		stamp   *atomic.Pointer[time.Time]
		inner   *recordingLock
		decided *confirmingLock
	)

	BeforeEach(func() {
		stamp = &atomic.Pointer[time.Time]{}
		inner = &recordingLock{identity: ourIdentity}
		decided = &confirmingLock{Interface: inner, lastConfirmed: stamp}
	})

	ourRecord := resourcelock.LeaderElectionRecord{
		HolderIdentity: ourIdentity,
		RenewTime:      metav1.NewTime(time.Now()),
	}

	It("stamps a successful write that names us", func(ctx context.Context) {
		Expect(decided.Update(ctx, ourRecord)).To(Succeed())
		Expect(stamp.Load()).NotTo(BeNil())
	})

	It("stamps a successful create that names us", func(ctx context.Context) {
		Expect(decided.Create(ctx, ourRecord)).To(Succeed())
		Expect(stamp.Load()).NotTo(BeNil())
	})

	It("stamps the time the write started, not the time it returned", func(ctx context.Context) {
		// The API server may commit the write at any point during the call, and
		// a replica times the lease from when it sees the commit. A response
		// stamped on arrival would make us think the lease is fresher than the
		// replicas do.
		const delay = 100 * time.Millisecond
		inner.delay = delay

		before := time.Now()
		Expect(decided.Update(ctx, ourRecord)).To(Succeed())
		after := time.Now()

		Expect(after.Sub(before)).To(BeNumerically(">=", delay))
		Expect(stamp.Load().Sub(before)).To(BeNumerically(">=", 0))
		Expect(after.Sub(*stamp.Load())).To(BeNumerically(">=", delay))
	})

	It("does not stamp a failed write", func(ctx context.Context) {
		inner.failWith = errors.New("api server unreachable")

		Expect(decided.Update(ctx, ourRecord)).NotTo(Succeed())
		Expect(stamp.Load()).To(BeNil())
	})

	It("does not stamp a write that clears the holder, which is a release", func(ctx context.Context) {
		Expect(decided.Update(ctx, resourcelock.LeaderElectionRecord{
			RenewTime: metav1.NewTime(time.Now()),
		})).To(Succeed())
		Expect(stamp.Load()).To(BeNil())
	})

	It("does not stamp a write that names another holder", func(ctx context.Context) {
		Expect(decided.Update(ctx, resourcelock.LeaderElectionRecord{
			HolderIdentity: "test-cluster-2",
			RenewTime:      metav1.NewTime(time.Now()),
		})).To(Succeed())
		Expect(stamp.Load()).To(BeNil())
	})

	It("does not stamp a successful read, even one that names us", func(ctx context.Context) {
		// A read proves the record still names us, not that it is fresh. This is
		// the distinction the whole staleness measurement rests on.
		_, _, err := decided.Get(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(stamp.Load()).To(BeNil())
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
		r         *Runnable
		checks    *atomic.Int32
		hardStops *atomic.Int32
		stoppedAt *atomic.Pointer[time.Time]
	)

	BeforeEach(func() {
		instance := postgres.NewInstance().
			WithNamespace(namespace).
			WithPodName(thisPod).
			WithClusterName(clusterName)
		r = New(fake.NewClientset(), instance)
		r.config.RetryPeriod = retryPeriod
		r.config.RenewDeadline = renewDeadline
		// Far enough that the specs below never reach the last resort unless
		// they bring the lease duration down on purpose.
		r.config.LeaseDuration = time.Hour
		checks = &atomic.Int32{}
		hardStops = &atomic.Int32{}
		stoppedAt = &atomic.Pointer[time.Time]{}
		r.hardStop = func() {
			stoppedAt.Store(ptr.To(time.Now()))
			hardStops.Add(1)
		}
	})

	// verdict installs a step-down check that counts its calls.
	verdict := func(stepDown bool, reason error) {
		r.checkStepDown = func(*apiv1.Cluster, string) (bool, error) {
			checks.Add(1)
			return stepDown, reason
		}
	}

	// start runs the monitor for the duration of the spec. The cleanup waits for
	// it to return: the monitor keeps running after it delivers its request, so
	// it would otherwise outlive the spec and race with the next BeforeEach.
	start := func(ctx context.Context) {
		monitorCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		DeferCleanup(func() {
			cancel()
			<-done
		})
		go func() {
			defer close(done)
			r.monitorLeaseAge(monitorCtx)
		}()
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
		r.lastConfirmed.Store(nil)
		verdict(true, nil)

		start(ctx)

		Consistently(checks.Load, 100*time.Millisecond).Should(BeEquivalentTo(0))
	})

	It("keeps the stamp of a write that was confirmed before the monitor started", func(ctx context.Context) {
		r.config.RenewDeadline = time.Hour
		confirmed := ptr.To(time.Now())
		r.lastConfirmed.Store(confirmed)
		verdict(true, nil)

		start(ctx)

		Consistently(r.lastConfirmed.Load, 100*time.Millisecond).Should(BeIdenticalTo(confirmed))
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

	It("forces a stop before the lease expires when nobody takes the shutdown request",
		func(ctx context.Context) {
			// No reader: the lifecycle manager is busy with an earlier command for
			// longer than the lease lasts. The tick is long enough that a loaded
			// machine does not delay the monitor by more than a tick, which is the
			// margin this spec measures.
			r.config.RetryPeriod = 100 * time.Millisecond
			r.config.LeaseDuration = 500 * time.Millisecond
			verdict(true, nil)

			start(ctx)

			Eventually(hardStops.Load).Should(BeEquivalentTo(1))
			// No write is confirmed, so the stamp is still the one the monitor
			// seeded: the stop must land before the lease can have expired.
			Expect(stoppedAt.Load().Sub(*r.lastConfirmed.Load())).To(BeNumerically("<", r.config.LeaseDuration))
		})

	It("forces a stop before the lease expires when the shutdown request does not end PostgreSQL",
		func(ctx context.Context) {
			// The request is taken, but the postmaster never exits, so ctx stays
			// live. This is pg_ctl failing against a hung backend.
			r.config.LeaseDuration = 50 * time.Millisecond
			verdict(true, nil)
			received := readCommands(r)

			start(ctx)

			Eventually(received).Should(Receive(Equal(postgres.InstanceCommand("ShutDownImmediate"))))
			Eventually(hardStops.Load).Should(BeEquivalentTo(1))
			Consistently(received, 100*time.Millisecond).ShouldNot(Receive())
		})

	It("does not force a stop while the peers say this is still the primary", func(ctx context.Context) {
		r.config.LeaseDuration = 50 * time.Millisecond
		verdict(false, nil)

		start(ctx)

		Eventually(checks.Load).Should(BeNumerically(">", 1))
		Consistently(hardStops.Load, 100*time.Millisecond).Should(BeEquivalentTo(0))
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

var _ = Describe("Runnable.runLeaderElection", func() {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		thisPod     = "test-cluster-1"
	)

	It("keeps the step-down monitor running after the runnable's context is cancelled",
		func(ctx context.Context) {
			// The manager cancels the runnable's context when any runnable fails,
			// while the lifecycle manager is still stopping PostgreSQL. From then
			// on the lease is not renewed, and the monitor is what fences.
			kubeClient := fake.NewClientset(&coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: clusterName},
				Spec:       coordinationv1.LeaseSpec{HolderIdentity: ptr.To(thisPod)},
			})
			r := New(kubeClient, postgres.NewInstance().
				WithNamespace(namespace).
				WithPodName(thisPod).
				WithClusterName(clusterName))
			r.config.RetryPeriod = 5 * time.Millisecond
			r.config.RenewDeadline = 25 * time.Millisecond
			r.config.LeaseDuration = time.Hour

			var checks atomic.Int32
			r.checkStepDown = func(*apiv1.Cluster, string) (bool, error) {
				checks.Add(1)
				return false, nil
			}

			lifecycleCtx, endLifecycle := context.WithCancel(ctx)
			r.SetMonitorContext(lifecycleCtx)

			runCtx, cancelRun := context.WithCancel(ctx)
			returned := make(chan struct{})
			go func() {
				defer close(returned)
				_ = r.runLeaderElection(runCtx)
			}()
			Eventually(r.heldCh).Should(BeClosed())

			cancelRun()
			Eventually(returned).Should(BeClosed())

			// Nothing renews the lease any more, so it goes stale and the monitor
			// keeps running its check.
			seen := checks.Load()
			Eventually(checks.Load).Should(BeNumerically(">", seen))

			endLifecycle()
			stopped := checks.Load()
			Consistently(checks.Load, 100*time.Millisecond).Should(BeNumerically("<=", stopped+1))
		})
})
