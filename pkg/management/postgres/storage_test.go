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

package postgres

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

var _ = Describe("Storage writability check", func() {
	var (
		tempDir  string
		instance *Instance
	)

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "storage-test-*")
		Expect(err).ToNot(HaveOccurred())

		instance = &Instance{
			PgData: tempDir,
		}
	})

	AfterEach(func() {
		_ = os.RemoveAll(tempDir)
	})

	It("succeeds on a writable directory", func() {
		err := instance.CheckStorageWritability(context.Background())
		Expect(err).ToNot(HaveOccurred())
	})

	It("succeeds when pg_wal is a symlink to another writable directory", func() {
		walDir, err := os.MkdirTemp("", "wal-test-*")
		Expect(err).ToNot(HaveOccurred())
		defer func() { _ = os.RemoveAll(walDir) }()

		err = os.Symlink(walDir, filepath.Join(tempDir, "pg_wal"))
		Expect(err).ToNot(HaveOccurred())

		err = instance.CheckStorageWritability(context.Background())
		Expect(err).ToNot(HaveOccurred())
	})

	It("fails when directory is not writable", func() {
		readOnlyDir := filepath.Join(tempDir, "readonly")
		err := os.Mkdir(readOnlyDir, 0o500)
		Expect(err).ToNot(HaveOccurred())

		roInstance := &Instance{
			PgData: readOnlyDir,
		}
		err = roInstance.CheckStorageWritability(context.Background())
		Expect(err).To(HaveOccurred())
	})

	It("returns nil when disabled in cluster configuration", func() {
		readOnlyDir := filepath.Join(tempDir, "readonly-disabled")
		err := os.Mkdir(readOnlyDir, 0o500)
		Expect(err).ToNot(HaveOccurred())

		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Probes: &apiv1.ProbesConfiguration{
					Liveness: &apiv1.LivenessProbe{
						DataVolumeCheck: &apiv1.DataVolumeCheckConfiguration{
							Enabled: ptr.To(false),
						},
					},
				},
			},
		}

		roInstance := &Instance{
			PgData: readOnlyDir,
		}
		roInstance.SetCluster(cluster)

		err = roInstance.CheckStorageWritability(context.Background())
		Expect(err).ToNot(HaveOccurred())
	})

	It("returns nil when PgData is empty", func() {
		emptyInstance := &Instance{
			PgData: "",
		}
		err := emptyInstance.CheckStorageWritability(context.Background())
		Expect(err).ToNot(HaveOccurred())
	})

	It("caches successful check within cache duration", func() {
		err := instance.CheckStorageWritability(context.Background())
		Expect(err).ToNot(HaveOccurred())

		firstCheckTime := instance.storageCheckTracker.lastChecked
		Expect(firstCheckTime).ToNot(BeZero())

		// Second check immediately after should use cache and not change lastChecked
		err = instance.CheckStorageWritability(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(instance.storageCheckTracker.lastChecked).To(Equal(firstCheckTime))
	})

	It("honors custom timeout configuration", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Probes: &apiv1.ProbesConfiguration{
					Liveness: &apiv1.LivenessProbe{
						DataVolumeCheck: &apiv1.DataVolumeCheckConfiguration{
							Enabled:        ptr.To(true),
							TimeoutSeconds: ptr.To(int32(10)),
						},
					},
				},
			},
		}
		instance.SetCluster(cluster)
		Expect(instance.GetClusterOrDefault().GetDataVolumeCheckTimeout()).To(Equal(10 * time.Second))
	})
})
