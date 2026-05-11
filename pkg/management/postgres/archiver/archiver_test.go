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

package archiver

import (
	"context"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("runForPrimaryCluster", func() {
	var (
		ctx     context.Context
		cluster *apiv1.Cluster
		pgData  string
	)

	BeforeEach(func() {
		ctx = context.Background()
		pgData = GinkgoT().TempDir()
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "pod-1",
				TargetPrimary:  "pod-1",
			},
		}
	})

	Context("when the pod is the CurrentPrimary", func() {
		It("archives WAL (internalRun returns nil when no backup is configured)", func() {
			err := runForPrimaryCluster(ctx, "pod-1", pgData, cluster, "000000010000000000000001")
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("when the pod is not the CurrentPrimary", func() {
		BeforeEach(func() {
			cluster.Status.CurrentPrimary = "pod-1"
			cluster.Status.TargetPrimary = "pod-1"
		})

		Context("and standby.signal is present", func() {
			BeforeEach(func() {
				Expect(os.WriteFile(filepath.Join(pgData, "standby.signal"), []byte{}, 0o600)).To(Succeed())
			})

			It("skips archiving silently (HA replica)", func() {
				err := runForPrimaryCluster(ctx, "pod-2", pgData, cluster, "000000010000000000000001")
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("and standby.signal is absent", func() {
			It("refuses to archive (former primary during switchover)", func() {
				err := runForPrimaryCluster(ctx, "pod-2", pgData, cluster, "000000010000000000000001")
				Expect(err).To(MatchError(errSwitchoverInProgress))
			})
		})

		Context("and standby.signal cannot be checked", func() {
			It("returns errStandbySignalCheck", func() {
				// Make pgData itself a regular file so that joining "standby.signal"
				// onto it produces an ENOTDIR error, which FileExists propagates.
				badPgData := filepath.Join(pgData, "not-a-dir")
				Expect(os.WriteFile(badPgData, []byte{}, 0o600)).To(Succeed())
				err := runForPrimaryCluster(ctx, "pod-2", badPgData, cluster, "000000010000000000000001")
				Expect(err).To(MatchError(errStandbySignalCheck))
			})
		})
	})
})

var _ = Describe("runForReplicaCluster", func() {
	var (
		ctx     context.Context
		cluster *apiv1.Cluster
	)

	BeforeEach(func() {
		ctx = context.Background()
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Primary: "upstream-cluster",
				},
			},
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "pod-1",
				TargetPrimary:  "pod-1",
			},
		}
	})

	Context("when the pod is neither CurrentPrimary nor TargetPrimary", func() {
		It("skips archiving silently", func() {
			err := runForReplicaCluster(ctx, "pod-2", "/pgdata", cluster, "000000010000000000000001")
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("when the pod is the CurrentPrimary", func() {
		It("attempts to archive (internalRun is called, backup not configured so returns nil)", func() {
			// internalRun returns nil when no backup is configured
			err := runForReplicaCluster(ctx, "pod-1", "/pgdata", cluster, "000000010000000000000001")
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("when the pod is the TargetPrimary but not yet CurrentPrimary", func() {
		BeforeEach(func() {
			cluster.Status.CurrentPrimary = "pod-1"
			cluster.Status.TargetPrimary = "pod-2"
		})

		It("attempts to archive for the incoming designated primary", func() {
			err := runForReplicaCluster(ctx, "pod-2", "/pgdata", cluster, "000000010000000000000001")
			Expect(err).ToNot(HaveOccurred())
		})

		It("skips archiving for a non-designated pod", func() {
			err := runForReplicaCluster(ctx, "pod-3", "/pgdata", cluster, "000000010000000000000001")
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
