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
	"errors"
	"fmt"

	barmanRestorer "github.com/cloudnative-pg/barman-cloud/pkg/restorer"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/cache"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeCacheClient is an in-memory implementation of local.CacheClient for tests.
type fakeCacheClient struct {
	envs   map[string][]string
	stores map[string]*apiv1.BarmanObjectStoreConfiguration
}

func (f fakeCacheClient) GetCluster() (*apiv1.Cluster, error) {
	return nil, errors.New("GetCluster not implemented in fakeCacheClient")
}

func (f fakeCacheClient) GetEnv(key string) ([]string, error) {
	if v, ok := f.envs[key]; ok {
		return v, nil
	}
	return nil, cache.ErrCacheMiss
}

func (f fakeCacheClient) GetBarmanObjectStore(key string) (*apiv1.BarmanObjectStoreConfiguration, error) {
	if v, ok := f.stores[key]; ok {
		return v, nil
	}
	return nil, cache.ErrCacheMiss
}

var _ = Describe("Function isStreamingAvailable", func() {
	It("returns false if cluster is nil", func() {
		Expect(isStreamingAvailable(nil, "testPod")).To(BeFalse())
	})

	It("returns true if current primary does not match the given pod name", func() {
		cluster := apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primaryPod",
			},
		}
		Expect(isStreamingAvailable(&cluster, "replicaPod")).To(BeTrue())
	})

	It("returns false if current primary matches the given pod name and this is not a replica cluster", func() {
		cluster := apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primaryPod",
			},
		}
		Expect(isStreamingAvailable(&cluster, "primaryPod")).To(BeFalse())
	})

	It("returns false if there are not connection parameters and this is a replica cluster", func() {
		cluster := apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primaryPod",
			},
			Spec: apiv1.ClusterSpec{
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "clusterSource",
					},
				},
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
					Source:  "clusterSource",
				},
			},
		}
		Expect(isStreamingAvailable(&cluster, "primaryPod")).To(BeFalse())
	})

	It("returns false if this is a replica cluster, "+
		"but replica cluster source does not match external cluster name", func() {
		cluster := apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primaryPod",
			},
			Spec: apiv1.ClusterSpec{
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "wrongNameClusterSource",
					},
				},
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
					Source:  "clusterSource",
				},
			},
		}
		Expect(isStreamingAvailable(&cluster, "primaryPod")).To(BeFalse())
	})

	It("returns true if the external cluster has streaming connection and this is a replica cluster", func() {
		cluster := apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primaryPod",
			},
			Spec: apiv1.ClusterSpec{
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name:                 "clusterSource",
						ConnectionParameters: map[string]string{"dbname": "test"},
					},
				},
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
					Source:  "clusterSource",
				},
			},
		}
		Expect(isStreamingAvailable(&cluster, "primaryPod")).To(BeTrue())
	})
})

var _ = Describe("Function shouldUseEndOfWALStreamFlag", func() {
	cluster := apiv1.Cluster{
		Status: apiv1.ClusterStatus{
			CurrentPrimary: "primaryPod",
		},
	}

	It("returns false in rewind mode, even when streaming is available", func() {
		Expect(isStreamingAvailable(&cluster, "replicaPod")).To(BeTrue())
		Expect(shouldUseEndOfWALStreamFlag(&cluster, "replicaPod", true)).To(BeFalse())
	})

	It("follows streaming availability when not in rewind mode", func() {
		Expect(shouldUseEndOfWALStreamFlag(&cluster, "replicaPod", false)).To(BeTrue())
		Expect(shouldUseEndOfWALStreamFlag(&cluster, "primaryPod", false)).To(BeFalse())
	})
})

var _ = Describe("Function isEndOfWALStream", func() {
	It("returns true when the requested WAL was not found", func() {
		results := []barmanRestorer.Result{
			{Err: barmanRestorer.ErrWALNotFound},
		}
		Expect(isEndOfWALStream(results)).To(BeTrue())
	})

	It("returns true when only a prefetched WAL was not found", func() {
		results := []barmanRestorer.Result{
			{Err: nil},
			{Err: fmt.Errorf("while restoring the prefetched WAL: %w", barmanRestorer.ErrWALNotFound)},
		}
		Expect(isEndOfWALStream(results)).To(BeTrue())
	})

	It("returns false when every WAL was restored", func() {
		results := []barmanRestorer.Result{
			{Err: nil},
			{Err: nil},
		}
		Expect(isEndOfWALStream(results)).To(BeFalse())
	})

	It("returns false when a restore failed with an error other than not-found", func() {
		results := []barmanRestorer.Result{
			{Err: errors.New("connection reset by peer")},
		}
		Expect(isEndOfWALStream(results)).To(BeFalse())
	})
})

var _ = Describe("getWALRestoreSettings", func() {
	const podName = "cluster-1"

	clusterWithOwnBackup := func(currentPrimary string) *apiv1.Cluster {
		return &apiv1.Cluster{
			Status: apiv1.ClusterStatus{CurrentPrimary: currentPrimary},
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
						BarmanCredentials: apiv1.BarmanCredentials{AWS: &apiv1.S3Credentials{}},
						DestinationPath:   "s3://own-backup/path",
						ServerName:        "own-server",
						Wal:               &apiv1.WalBackupConfiguration{MaxParallel: 3},
					},
				},
			},
		}
	}

	It("uses the cached bootstrap source store and credentials during a recovery Job", func(ctx SpecContext) {
		// No primary elected yet, and the cached source store carries no
		// parallelism setting, so prefetch defaults to a single segment.
		cluster := &apiv1.Cluster{Status: apiv1.ClusterStatus{CurrentPrimary: ""}}
		sourceStore := &apiv1.BarmanObjectStoreConfiguration{
			BarmanCredentials: apiv1.BarmanCredentials{AWS: &apiv1.S3Credentials{}},
			EndpointURL:       "https://source",
			DestinationPath:   "s3://source/path",
			ServerName:        "source-server",
		}
		creds := []string{"AWS_ACCESS_KEY_ID=source-key"}
		cacheClient := fakeCacheClient{
			envs:   map[string][]string{cache.WALRestoreKey: creds},
			stores: map[string]*apiv1.BarmanObjectStoreConfiguration{cache.WALRestoreConfigKey: sourceStore},
		}

		options, env, maxParallel, err := getWALRestoreSettings(ctx, cacheClient, cluster, podName, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(options).To(ContainElement("s3://source/path"))
		Expect(options).To(ContainElement("source-server"))
		Expect(options).To(ContainElement("https://source"))
		Expect(env).To(Equal(creds))
		Expect(maxParallel).To(Equal(1))
	})

	It("honors the cached source store Wal config during a recovery Job", func(ctx SpecContext) {
		// Prefetch parallelism and the additional command-line arguments come from
		// the cached source store's Wal config, just like a running instance
		// derives them from the store it restores from.
		cluster := &apiv1.Cluster{Status: apiv1.ClusterStatus{CurrentPrimary: ""}}
		sourceStore := &apiv1.BarmanObjectStoreConfiguration{
			DestinationPath: "s3://source/path",
			ServerName:      "source-server",
			Wal: &apiv1.WalBackupConfiguration{
				MaxParallel:                  2,
				RestoreAdditionalCommandArgs: []string{"--read-timeout=60"},
			},
		}
		cacheClient := fakeCacheClient{
			envs:   map[string][]string{cache.WALRestoreKey: {"AWS_ACCESS_KEY_ID=source-key"}},
			stores: map[string]*apiv1.BarmanObjectStoreConfiguration{cache.WALRestoreConfigKey: sourceStore},
		}

		_, _, maxParallel, err := getWALRestoreSettings(ctx, cacheClient, cluster, podName, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(maxParallel).To(Equal(2))
	})

	It("disables prefetch in rewind mode, even with a configured maxParallel", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{Status: apiv1.ClusterStatus{CurrentPrimary: ""}}
		sourceStore := &apiv1.BarmanObjectStoreConfiguration{
			DestinationPath: "s3://source/path",
			ServerName:      "source-server",
			Wal:             &apiv1.WalBackupConfiguration{MaxParallel: 2},
		}
		cacheClient := fakeCacheClient{
			envs:   map[string][]string{cache.WALRestoreKey: {"AWS_ACCESS_KEY_ID=source-key"}},
			stores: map[string]*apiv1.BarmanObjectStoreConfiguration{cache.WALRestoreConfigKey: sourceStore},
		}

		_, _, maxParallel, err := getWALRestoreSettings(ctx, cacheClient, cluster, podName, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(maxParallel).To(Equal(1))
	})

	It("ignores the cached bootstrap store once a primary has been elected", func(ctx SpecContext) {
		cluster := clusterWithOwnBackup(podName)
		cacheClient := fakeCacheClient{
			envs: map[string][]string{cache.WALRestoreKey: {"AWS_ACCESS_KEY_ID=own-key"}},
			// This must never be used: a running instance resolves the store from
			// the cluster spec, not from the bootstrap cache.
			stores: map[string]*apiv1.BarmanObjectStoreConfiguration{
				cache.WALRestoreConfigKey: {DestinationPath: "s3://POISON-MUST-NOT-BE-USED"},
			},
		}

		options, _, maxParallel, err := getWALRestoreSettings(ctx, cacheClient, cluster, podName, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(options).ToNot(ContainElement("s3://POISON-MUST-NOT-BE-USED"))
		Expect(options).To(ContainElement("s3://own-backup/path"))
		Expect(maxParallel).To(Equal(3))
	})

	It("falls back to the cluster store during bootstrap when no store is cached", func(ctx SpecContext) {
		cluster := clusterWithOwnBackup("")
		cacheClient := fakeCacheClient{
			envs: map[string][]string{cache.WALRestoreKey: {"AWS_ACCESS_KEY_ID=own-key"}},
		}

		options, _, maxParallel, err := getWALRestoreSettings(ctx, cacheClient, cluster, podName, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(options).To(ContainElement("s3://own-backup/path"))
		Expect(maxParallel).To(Equal(3))
	})

	It("disables prefetch in rewind mode for a cluster with its own backup configured", func(ctx SpecContext) {
		cluster := clusterWithOwnBackup(podName)
		cacheClient := fakeCacheClient{envs: map[string][]string{
			cache.WALRestoreKey: {"AWS_ACCESS_KEY_ID=own-key"},
		}}

		_, _, maxParallel, err := getWALRestoreSettings(ctx, cacheClient, cluster, podName, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(maxParallel).To(Equal(1))
	})

	It("returns ErrNoBackupConfigured when nothing is available", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{Status: apiv1.ClusterStatus{CurrentPrimary: podName}}
		cacheClient := fakeCacheClient{}

		_, _, _, err := getWALRestoreSettings(ctx, cacheClient, cluster, podName, false)
		Expect(err).To(MatchError(ErrNoBackupConfigured))
	})
})

var _ = Describe("validateTimelineHistoryFile", func() {
	It("should allow regular WAL files to pass through", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     5,
			},
		}

		err := validateTimelineHistoryFile(ctx, "000000010000000000000001", cluster, "replica-pod")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow invalid history filenames to pass through", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     5,
			},
		}

		err := validateTimelineHistoryFile(ctx, "invalid.history", cluster, "replica-pod")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow primary to download any timeline", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     5,
			},
		}

		err := validateTimelineHistoryFile(ctx, "00000064.history", cluster, "primary-pod")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow target primary (being promoted) to download any timeline", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "old-primary",
				TargetPrimary:  "new-primary",
				TimelineID:     5,
			},
		}

		err := validateTimelineHistoryFile(ctx, "00000064.history", cluster, "new-primary")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow replica to download current timeline", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     33,
			},
		}

		err := validateTimelineHistoryFile(ctx, "00000021.history", cluster, "replica-pod")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow replica to download past timeline", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     33,
			},
		}

		err := validateTimelineHistoryFile(ctx, "00000010.history", cluster, "replica-pod")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should reject future timeline for replica", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     33,
			},
		}

		err := validateTimelineHistoryFile(ctx, "00000022.history", cluster, "replica-pod")
		Expect(err).To(Equal(barmanRestorer.ErrWALNotFound))
	})

	It("should reject future timeline for replica with large timeline difference", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     5,
			},
		}

		err := validateTimelineHistoryFile(ctx, "00000064.history", cluster, "replica-pod")
		Expect(err).To(Equal(barmanRestorer.ErrWALNotFound))
	})

	It("should reject a future timeline history file for an established replica",
		func(ctx SpecContext) {
			cluster := &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					CurrentPrimary: "primary-pod",
					TargetPrimary:  "primary-pod",
					TimelineID:     20,
				},
			}

			err := validateTimelineHistoryFile(ctx, "00000015.history", cluster, "replica-pod")
			Expect(err).To(Equal(barmanRestorer.ErrWALNotFound))
		})

	It("should allow any history file when cluster timeline is not yet established", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     0,
			},
		}

		err := validateTimelineHistoryFile(ctx, "00000014.history", cluster, "replica-pod")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow any history file when no primary has been elected yet (empty cluster status)", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				TimelineID: 0,
			},
		}

		err := validateTimelineHistoryFile(ctx, "00000014.history", cluster, "restore-pod")
		Expect(err).ToNot(HaveOccurred())
	})
})
