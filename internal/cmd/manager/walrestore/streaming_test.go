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
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("streamingRetryState", func() {
	start := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

	// restoreEvery runs a restore every second from the given time until the
	// first one that fails, and returns its time
	restoreEvery := func(state *streamingRetryState, from time.Time) time.Time {
		for now := from; now.Before(from.Add(time.Hour)); now = now.Add(time.Second) {
			var fail bool
			if *state, fail = state.next(now); fail {
				return now
			}
		}
		Fail("no restore failed")
		return time.Time{}
	}

	It("fails the restores for a while once the standby restored for the whole interval", func() {
		var state streamingRetryState
		failedAt := restoreEvery(&state, start)
		Expect(failedAt).To(Equal(start.Add(streamingRetryInterval)))

		var fail bool
		state, fail = state.next(failedAt.Add(streamingRetryFailFor - time.Millisecond))
		Expect(fail).To(BeTrue())
	})

	It("backs off when the standby is back on the archive soon after the forced failures", func() {
		var state streamingRetryState
		failedAt := restoreEvery(&state, start)

		resumedAt := failedAt.Add(streamingRetryFailFor)
		var fail bool
		state, fail = state.next(resumedAt)
		Expect(fail).To(BeFalse())
		Expect(state.Interval).To(Equal(2 * streamingRetryInterval))

		Expect(restoreEvery(&state, resumedAt)).To(Equal(resumedAt.Add(2 * streamingRetryInterval)))
	})

	It("caps the interval", func() {
		var state streamingRetryState
		now := start
		for range 10 {
			now = restoreEvery(&state, now).Add(streamingRetryFailFor)
		}
		Expect(state.Interval).To(Equal(streamingRetryMaxInterval))
	})

	It("starts over with the base interval after the standby was streaming", func() {
		state := streamingRetryState{
			WindowStart: start,
			LastSeen:    start.Add(time.Minute),
			Interval:    streamingRetryMaxInterval,
			FailUntil:   start.Add(time.Minute + streamingRetryFailFor),
		}

		resumedAt := state.LastSeen.Add(streamingRetryIdleGap)
		Expect(restoreEvery(&state, resumedAt)).To(Equal(resumedAt.Add(streamingRetryInterval)))
	})
})

var _ = Describe("checkStreamingRetry", func() {
	const (
		podName = "cluster-example-2"
		walName = "000000010000000000000003"
	)

	var (
		pgData         string
		stateFile      string
		spoolDirectory string
		cluster        *apiv1.Cluster
	)

	writeState := func(state streamingRetryState) {
		content, err := json.Marshal(state)
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(stateFile, content, 0o600)).To(Succeed())
	}

	BeforeEach(func() {
		tempDir := GinkgoT().TempDir()
		pgData = filepath.Join(tempDir, "pgdata")
		Expect(os.Mkdir(pgData, 0o700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(pgData, "standby.signal"), nil, 0o600)).To(Succeed())

		originalStateFile := streamingRetryStateFile
		stateFile = filepath.Join(tempDir, "state.json")
		streamingRetryStateFile = stateFile
		DeferCleanup(func() { streamingRetryStateFile = originalStateFile })

		originalSpoolDirectory := streamingRetrySpoolDirectory
		spoolDirectory = filepath.Join(tempDir, "spool")
		Expect(os.Mkdir(spoolDirectory, 0o700)).To(Succeed())
		streamingRetrySpoolDirectory = spoolDirectory
		DeferCleanup(func() { streamingRetrySpoolDirectory = originalSpoolDirectory })

		cluster = &apiv1.Cluster{Status: apiv1.ClusterStatus{
			CurrentPrimary: "cluster-example-1",
			TargetPrimary:  "cluster-example-1",
		}}

		// The standby has been restoring WAL files for the whole interval
		now := time.Now()
		writeState(streamingRetryState{
			WindowStart: now.Add(-streamingRetryInterval),
			LastSeen:    now.Add(-time.Second),
			Interval:    streamingRetryInterval,
		})
	})

	It("fails for a standby that restored WAL files for the whole interval", func(ctx SpecContext) {
		Expect(checkStreamingRetry(ctx, cluster, podName, pgData, walName, false)).To(MatchError(ErrStreamingRetry))
		Expect(checkStreamingRetry(ctx, cluster, podName, pgData, walName, false)).To(MatchError(ErrStreamingRetry))
	})

	It("fails for the designated primary of a replica cluster streaming from its source", func(ctx SpecContext) {
		cluster.Status.CurrentPrimary = podName
		cluster.Status.TargetPrimary = podName
		cluster.Spec.ReplicaCluster = &apiv1.ReplicaClusterConfiguration{Enabled: ptr.To(true), Source: "source"}
		cluster.Spec.ExternalClusters = []apiv1.ExternalCluster{
			{Name: "source", ConnectionParameters: map[string]string{"host": "source-rw"}},
		}
		Expect(checkStreamingRetry(ctx, cluster, podName, pgData, walName, false)).To(MatchError(ErrStreamingRetry))
	})

	It("restores a WAL file prefetched in the spool, and fails the next one", func(ctx SpecContext) {
		Expect(os.WriteFile(filepath.Join(spoolDirectory, walName), nil, 0o600)).To(Succeed())
		state, err := fileutils.ReadFile(stateFile)
		Expect(err).ToNot(HaveOccurred())

		Expect(checkStreamingRetry(ctx, cluster, podName, pgData, walName, false)).To(Succeed())
		Expect(fileutils.ReadFile(stateFile)).To(Equal(state))
		Expect(checkStreamingRetry(ctx, cluster, podName, pgData, "000000010000000000000004", false)).
			To(MatchError(ErrStreamingRetry))
	})

	It("starts a new interval when the state cannot be read", func(ctx SpecContext) {
		Expect(os.WriteFile(stateFile, []byte("{"), 0o600)).To(Succeed())
		Expect(checkStreamingRetry(ctx, cluster, podName, pgData, walName, false)).To(Succeed())
	})

	DescribeTable("never fails",
		func(ctx SpecContext, setup func() (walName string, rewindMode bool)) {
			requestedWAL, rewindMode := setup()
			Expect(checkStreamingRetry(ctx, cluster, podName, pgData, requestedWAL, rewindMode)).To(Succeed())
		},
		Entry("on behalf of pg_rewind", func() (string, bool) {
			return walName, true
		}),
		Entry("for a file that is not a WAL segment", func() (string, bool) {
			return "00000002.history", false
		}),
		Entry("without standby.signal, as in a recovery", func() (string, bool) {
			Expect(os.Remove(filepath.Join(pgData, "standby.signal"))).To(Succeed())
			Expect(os.WriteFile(filepath.Join(pgData, "recovery.signal"), nil, 0o600)).To(Succeed())
			return walName, false
		}),
		Entry("while a promotion is pending", func() (string, bool) {
			Expect(os.WriteFile(filepath.Join(pgData, "promote"), nil, 0o600)).To(Succeed())
			return walName, false
		}),
		Entry("while the operator promotes the instance", func() (string, bool) {
			cluster.Status.TargetPrimary = podName
			return walName, false
		}),
		Entry("on the primary", func() (string, bool) {
			cluster.Status.CurrentPrimary = podName
			cluster.Status.TargetPrimary = podName
			return walName, false
		}),
		Entry("during the bootstrap of the first instance", func() (string, bool) {
			cluster.Status.CurrentPrimary = ""
			return walName, false
		}),
		Entry("for the designated primary of a replica cluster without streaming from its source", func() (string, bool) {
			cluster.Status.CurrentPrimary = podName
			cluster.Status.TargetPrimary = podName
			cluster.Spec.ReplicaCluster = &apiv1.ReplicaClusterConfiguration{Enabled: ptr.To(true), Source: "source"}
			cluster.Spec.ExternalClusters = []apiv1.ExternalCluster{{Name: "source"}}
			return walName, false
		}),
		Entry("when the state cannot be stored", func() (string, bool) {
			if os.Geteuid() == 0 {
				Skip("root can write a read-only file")
			}
			Expect(os.Chmod(stateFile, 0o400)).To(Succeed())
			return walName, false
		}),
	)
})
