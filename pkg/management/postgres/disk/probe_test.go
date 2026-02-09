//go:build linux

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

package disk

import (
	"fmt"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Disk Probe", func() {
	Describe("GetVolumeStats", func() {
		It("should return correct stats from a mock statfs", func() {
			mockStatfs := func(_ string, stat *syscall.Statfs_t) error {
				stat.Bsize = 4096
				stat.Blocks = 1000000 // ~4GB total
				stat.Bfree = 300000   // ~1.2GB free (including reserved)
				stat.Bavail = 250000  // ~1GB available to non-root
				stat.Files = 100000
				stat.Ffree = 80000
				return nil
			}

			probe := NewProbeWithStatfs(mockStatfs)
			stats, err := probe.GetVolumeStats("/test/path")
			Expect(err).NotTo(HaveOccurred())
			Expect(stats).NotTo(BeNil())

			// Total = 1000000 * 4096 = 4096000000
			Expect(stats.TotalBytes).To(Equal(uint64(4096000000)))
			// Used = Total - Free = 4096000000 - (300000 * 4096) = 4096000000 - 1228800000 = 2867200000
			Expect(stats.UsedBytes).To(Equal(uint64(2867200000)))
			// Available = 250000 * 4096 = 1024000000
			Expect(stats.AvailableBytes).To(Equal(uint64(1024000000)))
			// Inodes
			Expect(stats.InodesTotal).To(Equal(uint64(100000)))
			Expect(stats.InodesUsed).To(Equal(uint64(20000)))
			Expect(stats.InodesFree).To(Equal(uint64(80000)))
			// Percent used should be > 0 and < 100
			Expect(stats.PercentUsed).To(BeNumerically(">", 0))
			Expect(stats.PercentUsed).To(BeNumerically("<", 100))
		})

		It("should return 0 percent used for an empty volume", func() {
			mockStatfs := func(_ string, stat *syscall.Statfs_t) error {
				stat.Bsize = 4096
				stat.Blocks = 1000000
				stat.Bfree = 1000000
				stat.Bavail = 1000000
				stat.Files = 100000
				stat.Ffree = 100000
				return nil
			}

			probe := NewProbeWithStatfs(mockStatfs)
			stats, err := probe.GetVolumeStats("/test/path")
			Expect(err).NotTo(HaveOccurred())
			Expect(stats.PercentUsed).To(Equal(float64(0)))
			Expect(stats.UsedBytes).To(Equal(uint64(0)))
		})

		It("should return an error when statfs fails", func() {
			mockStatfs := func(_ string, _ *syscall.Statfs_t) error {
				return fmt.Errorf("statfs error: no such path")
			}

			probe := NewProbeWithStatfs(mockStatfs)
			stats, err := probe.GetVolumeStats("/nonexistent/path")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("statfs failed"))
			Expect(stats).To(BeNil())
		})

		It("should handle a nearly full volume", func() {
			mockStatfs := func(_ string, stat *syscall.Statfs_t) error {
				stat.Bsize = 4096
				stat.Blocks = 1000000
				stat.Bfree = 10000 // 1% free
				stat.Bavail = 5000 // 0.5% available
				stat.Files = 100000
				stat.Ffree = 1000
				return nil
			}

			probe := NewProbeWithStatfs(mockStatfs)
			stats, err := probe.GetVolumeStats("/test/path")
			Expect(err).NotTo(HaveOccurred())
			Expect(stats.PercentUsed).To(BeNumerically(">", 95))
			Expect(stats.InodesFree).To(Equal(uint64(1000)))
		})

		It("should handle zero-size volume gracefully", func() {
			mockStatfs := func(_ string, stat *syscall.Statfs_t) error {
				stat.Bsize = 0
				stat.Blocks = 0
				stat.Bfree = 0
				stat.Bavail = 0
				stat.Files = 0
				stat.Ffree = 0
				return nil
			}

			probe := NewProbeWithStatfs(mockStatfs)
			stats, err := probe.GetVolumeStats("/test/path")
			Expect(err).NotTo(HaveOccurred())
			Expect(stats.TotalBytes).To(Equal(uint64(0)))
			Expect(stats.PercentUsed).To(Equal(float64(0)))
		})
	})

	Describe("ProbeVolume", func() {
		It("should return a VolumeProbeResult with correct metadata for data volume", func() {
			mockStatfs := func(_ string, stat *syscall.Statfs_t) error {
				stat.Bsize = 4096
				stat.Blocks = 500000
				stat.Bfree = 200000
				stat.Bavail = 180000
				stat.Files = 50000
				stat.Ffree = 40000
				return nil
			}

			probe := NewProbeWithStatfs(mockStatfs)
			result, err := probe.ProbeVolume("/var/lib/postgresql/data/pgdata", VolumeTypeData, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.VolumeType).To(Equal(VolumeTypeData))
			Expect(result.Tablespace).To(BeEmpty())
			Expect(result.MountPath).To(Equal("/var/lib/postgresql/data/pgdata"))
			Expect(result.Stats.TotalBytes).To(BeNumerically(">", 0))
		})

		It("should return a VolumeProbeResult with correct metadata for WAL volume", func() {
			mockStatfs := func(_ string, stat *syscall.Statfs_t) error {
				stat.Bsize = 4096
				stat.Blocks = 250000
				stat.Bfree = 100000
				stat.Bavail = 90000
				stat.Files = 25000
				stat.Ffree = 20000
				return nil
			}

			probe := NewProbeWithStatfs(mockStatfs)
			result, err := probe.ProbeVolume("/var/lib/postgresql/wal", VolumeTypeWAL, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.VolumeType).To(Equal(VolumeTypeWAL))
			Expect(result.Tablespace).To(BeEmpty())
		})

		It("should return a VolumeProbeResult with tablespace name", func() {
			mockStatfs := func(_ string, stat *syscall.Statfs_t) error {
				stat.Bsize = 4096
				stat.Blocks = 100000
				stat.Bfree = 50000
				stat.Bavail = 45000
				stat.Files = 10000
				stat.Ffree = 8000
				return nil
			}

			probe := NewProbeWithStatfs(mockStatfs)
			result, err := probe.ProbeVolume(
				"/var/lib/postgresql/tablespaces/myts",
				VolumeTypeTablespace,
				"myts",
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.VolumeType).To(Equal(VolumeTypeTablespace))
			Expect(result.Tablespace).To(Equal("myts"))
		})

		It("should propagate statfs errors", func() {
			mockStatfs := func(_ string, _ *syscall.Statfs_t) error {
				return fmt.Errorf("permission denied")
			}

			probe := NewProbeWithStatfs(mockStatfs)
			result, err := probe.ProbeVolume("/test/path", VolumeTypeData, "")
			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
		})
	})
})
