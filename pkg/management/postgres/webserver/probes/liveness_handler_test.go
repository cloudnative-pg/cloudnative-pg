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

package probes

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// isolatedShutdownCommand is the command the lifecycle manager expects on the instance
// command channel once the liveness handler hands over a shutdown request for an isolated
// primary. The type carrying it is exported, but the value is an unexported constant of the
// postgres package, so it is reproduced here from the literal the package defines it with.
const isolatedShutdownCommand = postgres.InstanceCommand("ShutDownFastImmediateIsolated")

var _ = Describe("IsHealthy", func() {
	var (
		ctx      context.Context
		instance *postgres.Instance
	)

	BeforeEach(func() {
		ctx = context.Background()
		instance = postgres.NewInstance().
			WithNamespace("test-namespace").
			WithClusterName("test-cluster")
	})

	// isolatedCluster builds a cluster with the isolation check enabled, a peer that will
	// never answer, and the given liveness failure threshold.
	isolatedCluster := func(threshold int32) *apiv1.Cluster {
		enabled := true
		return &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "test-namespace"},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				Probes: &apiv1.ProbesConfiguration{
					Liveness: &apiv1.LivenessProbe{
						Probe: apiv1.Probe{FailureThreshold: threshold},
						IsolationCheck: &apiv1.IsolationCheckConfiguration{
							Enabled:           &enabled,
							RequestTimeout:    200,
							ConnectionTimeout: 200,
						},
					},
				},
			},
			Status: apiv1.ClusterStatus{
				InstancesReportedState: map[apiv1.PodName]apiv1.InstanceReportedState{
					"unreachable-instance": {IP: "203.0.113.1"},
				},
			},
		}
	}

	// reachableCache builds a cache that can fetch cluster straight from the fake client,
	// the shape a healthy connection to the API server leaves behind.
	reachableCache := func(cluster *apiv1.Cluster) *ClusterCache {
		cli := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			Build()

		return NewClusterCache(cli, client.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		})
	}

	// unreachableCache builds a cache that starts out able to fetch the given cluster, so
	// that a first refresh succeeds and populates the cache, and is then repointed at a
	// name the fake client does not have, so that every following refresh fails while still
	// handing back the stale, populated cluster. This is the shape
	// tryGetLatestClusterWithTimeout leaves behind once the API server stops answering
	// after having answered at least once, which is what an isolated primary sees.
	unreachableCache := func(cluster *apiv1.Cluster) *ClusterCache {
		cache := reachableCache(cluster)

		var primed apiv1.Cluster
		Expect(cache.tryGetLatestClusterWithTimeout(ctx, &primed)).To(Succeed())
		Expect(primed.Name).To(Equal(cluster.Name))

		cache.key = client.ObjectKey{Namespace: cluster.Namespace, Name: "does-not-exist"}
		return cache
	}

	// expectNoShutdownRequest fails if a command shows up on the instance's command
	// channel within a short grace period, which is more than enough time for a handler
	// that issues the request synchronously and within a bounded timeout to have done so.
	expectNoShutdownRequest := func() {
		select {
		case cmd := <-instance.GetInstanceCommandChan():
			Fail(fmt.Sprintf("did not expect a shutdown request, got %q", cmd))
		case <-time.After(200 * time.Millisecond):
		}
	}

	// drainCommands keeps taking whatever is put on the instance's command channel for the
	// rest of the spec, forwarding it to the returned channel. It has to outlive every call
	// under test rather than take a single command: a handover nobody is there to receive
	// looks the same from the outside as one the handler declined to issue.
	drainCommands := func() <-chan postgres.InstanceCommand {
		received := make(chan postgres.InstanceCommand, 8)
		done := make(chan struct{})
		DeferCleanup(func() { close(done) })

		go func() {
			for {
				select {
				case cmd := <-instance.GetInstanceCommandChan():
					received <- cmd
				case <-done:
					return
				}
			}
		}()

		return received
	}

	It("returns 200 and requests no shutdown for a fenced instance", func() {
		instance.SetFencing(true)
		executor := &livenessExecutor{instance: instance, cache: reachableCache(isolatedCluster(1))}

		w := httptest.NewRecorder()
		executor.IsHealthy(ctx, w)

		Expect(w.Code).To(Equal(http.StatusOK))
		expectNoShutdownRequest()
	})

	It("returns 200 and requests no shutdown for a replica", func() {
		instance.PgData = GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(instance.PgData, "standby.signal"), nil, 0o600)).To(Succeed())
		executor := &livenessExecutor{instance: instance, cache: reachableCache(isolatedCluster(1))}

		w := httptest.NewRecorder()
		executor.IsHealthy(ctx, w)

		Expect(w.Code).To(Equal(http.StatusOK))
		expectNoShutdownRequest()
	})

	It("returns 200 and requests no shutdown when the instance role cannot be determined", func() {
		// A PgData that is itself a regular file, rather than a directory, makes the
		// os.Stat call inside IsPrimary fail with something other than "does not
		// exist", which is the only way to make IsPrimary return an error.
		file, err := os.CreateTemp(GinkgoT().TempDir(), "pgdata")
		Expect(err).ToNot(HaveOccurred())
		instance.PgData = file.Name()
		executor := &livenessExecutor{instance: instance, cache: reachableCache(isolatedCluster(1))}

		w := httptest.NewRecorder()
		executor.IsHealthy(ctx, w)

		Expect(w.Code).To(Equal(http.StatusOK))
		expectNoShutdownRequest()
	})

	Context("primary instance", func() {
		BeforeEach(func() {
			instance.PgData = GinkgoT().TempDir()
		})

		It("returns 200 even when the isolation check fails, as long as the API server is reachable", func() {
			executor := &livenessExecutor{instance: instance, cache: reachableCache(isolatedCluster(1))}

			w := httptest.NewRecorder()
			executor.IsHealthy(ctx, w)

			Expect(w.Code).To(Equal(http.StatusOK))
			expectNoShutdownRequest()
		})

		It("returns 500 without requesting a shutdown while the failure count is below the threshold", func() {
			executor := &livenessExecutor{instance: instance, cache: unreachableCache(isolatedCluster(3))}

			w := httptest.NewRecorder()
			executor.IsHealthy(ctx, w)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			expectNoShutdownRequest()
		})

		It("returns 500 and hands the shutdown request over once the threshold is reached", func() {
			executor := &livenessExecutor{instance: instance, cache: unreachableCache(isolatedCluster(1))}
			received := drainCommands()

			w := httptest.NewRecorder()
			executor.IsHealthy(ctx, w)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			Eventually(received).Should(Receive(Equal(isolatedShutdownCommand)))
		})

		It("spends the handover budget and gives the claim back when nobody takes the request", func() {
			// Nothing reads the command channel here, which is what makes this the test
			// that tells a synchronous handover from a backgrounded one: the handler has
			// to sit on the unbuffered send until its own budget runs out. A handler that
			// dispatched the request and returned would come back immediately and would
			// still be holding the claim.
			executor := &livenessExecutor{instance: instance, cache: unreachableCache(isolatedCluster(1))}

			w := httptest.NewRecorder()
			start := time.Now()
			executor.IsHealthy(ctx, w)
			elapsed := time.Since(start)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			Expect(elapsed).To(BeNumerically(">=", isolationShutdownHandoverTimeout))

			// The claim went back, so the next failing probe asks again rather than
			// leaving the instance isolated with nothing coming for it.
			received := drainCommands()
			w2 := httptest.NewRecorder()
			executor.IsHealthy(ctx, w2)

			Expect(w2.Code).To(Equal(http.StatusInternalServerError))
			Eventually(received).Should(Receive(Equal(isolatedShutdownCommand)))
		})

		It("reports failure promptly when the context is cancelled, and gives the claim back", func() {
			// Nobody reads the command channel here either, so the handover has
			// nothing to succeed against: the only way it can return is by seeing
			// ctx.Done(), and it has to see it well before the 500ms timer would
			// have fired on its own. This is the branch a kubelet's own probe
			// timeout takes: the request context it hands the handler dies mid
			// handover, and the shutdown must not be left dangling on the claim.
			cancelledCtx, cancel := context.WithCancel(ctx)
			cancel()

			executor := &livenessExecutor{instance: instance, cache: unreachableCache(isolatedCluster(1))}

			w := httptest.NewRecorder()
			start := time.Now()
			executor.IsHealthy(cancelledCtx, w)
			elapsed := time.Since(start)

			// The isolation check itself still spends up to the fixture's 200ms
			// connection timeout dialling the unreachable peer, so the bound below
			// leaves room for that and still lands comfortably clear of the 500ms
			// handover timer: this margin is what tells a context cancellation
			// apart from the timer having fired.
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			Expect(elapsed).To(BeNumerically("<", 400*time.Millisecond))

			// The claim went back, so the next failing probe asks again rather than
			// leaving the instance isolated with nothing coming for it.
			received := drainCommands()
			w2 := httptest.NewRecorder()
			executor.IsHealthy(ctx, w2)

			Expect(w2.Code).To(Equal(http.StatusInternalServerError))
			Eventually(received).Should(Receive(Equal(isolatedShutdownCommand)))
		})

		It("issues no second request once one has been handed over", func() {
			// The reader stays alive across both calls on purpose. With a reader that
			// stops after the first command, a second request would simply time out on
			// the unbuffered channel and go unnoticed, so the assertion below would hold
			// even without the claim that is supposed to prevent it.
			executor := &livenessExecutor{instance: instance, cache: unreachableCache(isolatedCluster(1))}
			received := drainCommands()

			w := httptest.NewRecorder()
			executor.IsHealthy(ctx, w)
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			Eventually(received).Should(Receive(Equal(isolatedShutdownCommand)))

			w2 := httptest.NewRecorder()
			executor.IsHealthy(ctx, w2)

			Expect(w2.Code).To(Equal(http.StatusInternalServerError))
			Consistently(received, 200*time.Millisecond).ShouldNot(Receive())
		})

		It("resets the failure count on a success in between, so the threshold needs a fresh run", func() {
			executor := &livenessExecutor{instance: instance, cache: unreachableCache(isolatedCluster(2))}

			w1 := httptest.NewRecorder()
			executor.IsHealthy(ctx, w1)
			Expect(w1.Code).To(Equal(http.StatusInternalServerError))

			// A success in between clears the accumulated failure, so the very next
			// failure is not the one that reaches the threshold of two.
			executor.isolationCheckSucceeded()

			w2 := httptest.NewRecorder()
			executor.IsHealthy(ctx, w2)
			Expect(w2.Code).To(Equal(http.StatusInternalServerError))
			expectNoShutdownRequest()

			received := drainCommands()

			w3 := httptest.NewRecorder()
			executor.IsHealthy(ctx, w3)
			Expect(w3.Code).To(Equal(http.StatusInternalServerError))
			Eventually(received).Should(Receive(Equal(isolatedShutdownCommand)))
		})
	})
})
