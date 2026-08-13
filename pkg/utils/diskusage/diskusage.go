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

// Package diskusage reports filesystem space usage for a path via statfs.
package diskusage

import "syscall"

// Usage describes the space usage of the filesystem backing a path.
type Usage struct {
	// TotalBytes is the total size of the filesystem.
	TotalBytes uint64
	// UsedBytes is the space currently in use (total minus free).
	UsedBytes uint64
	// AvailableBytes is the space available to unprivileged users.
	AvailableBytes uint64
	// FilesystemID identifies the backing filesystem; two paths on the
	// same filesystem share the same value. Used to detect volumes that
	// unexpectedly share a host filesystem.
	FilesystemID uint64
}

// PercentUsed returns the percentage (0-100) of the filesystem in use.
func (u Usage) PercentUsed() float64 {
	if u.TotalBytes == 0 {
		return 0
	}
	return 100 * float64(u.UsedBytes) / float64(u.TotalBytes)
}

// Get returns the Usage of the filesystem backing the given path.
func Get(path string) (Usage, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return Usage{}, err
	}

	blockSize := uint64(stat.Bsize) //nolint:gosec // statfs block size is always non-negative
	total := stat.Blocks * blockSize
	avail := stat.Bavail * blockSize
	// Guard against a corrupted statfs reporting more free than total blocks,
	// which would underflow the unsigned subtraction. TotalBytes/UsedBytes are
	// reported in bytes, so they stay within uint64 for any real-world volume.
	var used uint64
	if stat.Blocks > stat.Bfree {
		used = (stat.Blocks - stat.Bfree) * blockSize
	}

	var fsID uint64
	for _, v := range stat.Fsid.X__val {
		fsID = fsID<<32 | uint64(uint32(v)) //nolint:gosec // packing fsid words; the sign bit is intentionally discarded
	}

	return Usage{
		TotalBytes:     total,
		UsedBytes:      used,
		AvailableBytes: avail,
		FilesystemID:   fsID,
	}, nil
}
