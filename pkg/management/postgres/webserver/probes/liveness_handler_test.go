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

			received := make(chan postgres.InstanceCommand, 1)
			go func() {
				received <- <-instance.GetInstanceCommandChan()
			}()

			w := httptest.NewRecorder()
			executor.IsHealthy(ctx, w)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			Eventually(received).Should(Receive(Equal(isolatedShutdownCommand)))
		})

		It("issues no second request once one has been handed over", func() {
			executor := &livenessExecutor{instance: instance, cache: unreachableCache(isolatedCluster(1))}

			received := make(chan postgres.InstanceCommand, 1)
			go func() {
				received <- <-instance.GetInstanceCommandChan()
			}()

			w := httptest.NewRecorder()
			executor.IsHealthy(ctx, w)
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			Eventually(received).Should(Receive(Equal(isolatedShutdownCommand)))

			w2 := httptest.NewRecorder()
			executor.IsHealthy(ctx, w2)

			Expect(w2.Code).To(Equal(http.StatusInternalServerError))
			expectNoShutdownRequest()
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

			received := make(chan postgres.InstanceCommand, 1)
			go func() {
				received <- <-instance.GetInstanceCommandChan()
			}()

			w3 := httptest.NewRecorder()
			executor.IsHealthy(ctx, w3)
			Expect(w3.Code).To(Equal(http.StatusInternalServerError))
			Eventually(received).Should(Receive(Equal(isolatedShutdownCommand)))
		})
	})
})
