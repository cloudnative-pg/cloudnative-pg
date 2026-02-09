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

	"github.com/cloudnative-pg/machinery/pkg/log"
)

var diskLog = log.WithName("disk")

// StatfsFunc is the function signature for statfs system calls.
// This is exposed for testing purposes to allow mocking.
type StatfsFunc func(path string, stat *syscall.Statfs_t) error

// defaultStatfs is the production statfs implementation.
func defaultStatfs(path string, stat *syscall.Statfs_t) error {
	return syscall.Statfs(path, stat)
}

// Probe probes a filesystem mount point using statfs and returns VolumeStats.
type Probe struct {
	statfsFunc StatfsFunc
}

// NewProbe creates a new Probe with the default statfs syscall.
func NewProbe() *Probe {
	return &Probe{
		statfsFunc: defaultStatfs,
	}
}

// NewProbeWithStatfs creates a new Probe with a custom statfs function.
// This is intended for testing.
func NewProbeWithStatfs(fn StatfsFunc) *Probe {
	return &Probe{
		statfsFunc: fn,
	}
}

// GetVolumeStats probes the filesystem at the given path and returns
// disk usage statistics.
func (p *Probe) GetVolumeStats(mountPath string) (*VolumeStats, error) {
	var stat syscall.Statfs_t
	if err := p.statfsFunc(mountPath, &stat); err != nil {
		return nil, fmt.Errorf("statfs failed for path %s: %w", mountPath, err)
	}

	totalBytes := stat.Blocks * uint64(stat.Bsize)
	availableBytes := stat.Bavail * uint64(stat.Bsize)
	freeBytes := stat.Bfree * uint64(stat.Bsize)
	usedBytes := totalBytes - freeBytes

	var percentUsed float64
	if totalBytes > 0 {
		// Calculate percent used based on space available to non-root users
		// (totalBytes - freeBytes + availableBytes) is the effective total
		usableTotal := totalBytes - freeBytes + availableBytes
		if usableTotal > 0 {
			percentUsed = float64(usedBytes) / float64(usableTotal) * 100
		}
	}

	stats := &VolumeStats{
		TotalBytes:     totalBytes,
		UsedBytes:      usedBytes,
		AvailableBytes: availableBytes,
		PercentUsed:    percentUsed,
		InodesTotal:    stat.Files,
		InodesUsed:     stat.Files - stat.Ffree,
		InodesFree:     stat.Ffree,
	}

	diskLog.Debug("disk probe completed",
		"mountPath", mountPath,
		"totalBytes", stats.TotalBytes,
		"usedBytes", stats.UsedBytes,
		"availableBytes", stats.AvailableBytes,
		"percentUsed", stats.PercentUsed,
	)

	return stats, nil
}

// ProbeVolume probes a volume and returns a VolumeProbeResult with metadata.
func (p *Probe) ProbeVolume(
	mountPath string,
	volumeType VolumeType,
	tablespace string,
) (*VolumeProbeResult, error) {
	stats, err := p.GetVolumeStats(mountPath)
	if err != nil {
		return nil, err
	}

	return &VolumeProbeResult{
		VolumeType: volumeType,
		Tablespace: tablespace,
		MountPath:  mountPath,
		Stats:      *stats,
	}, nil
}
