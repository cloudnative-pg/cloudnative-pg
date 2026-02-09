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

// Package disk provides filesystem-level disk usage probing using statfs.
package disk

// VolumeType represents the type of volume being probed.
type VolumeType string

const (
	// VolumeTypeData represents the PGDATA volume.
	VolumeTypeData VolumeType = "data"
	// VolumeTypeWAL represents the WAL volume.
	VolumeTypeWAL VolumeType = "wal"
	// VolumeTypeTablespace represents a tablespace volume.
	VolumeTypeTablespace VolumeType = "tablespace"
)

// VolumeStats contains disk usage statistics for a single volume,
// gathered via statfs syscall.
type VolumeStats struct {
	// TotalBytes is the total capacity of the volume in bytes.
	TotalBytes uint64 `json:"totalBytes"`
	// UsedBytes is the number of bytes currently in use.
	UsedBytes uint64 `json:"usedBytes"`
	// AvailableBytes is the number of bytes available for use (non-root).
	AvailableBytes uint64 `json:"availableBytes"`
	// PercentUsed is the percentage of the volume in use (0-100).
	PercentUsed float64 `json:"percentUsed"`
	// InodesTotal is the total number of inodes.
	InodesTotal uint64 `json:"inodesTotal"`
	// InodesUsed is the number of inodes in use.
	InodesUsed uint64 `json:"inodesUsed"`
	// InodesFree is the number of free inodes.
	InodesFree uint64 `json:"inodesFree"`
}

// VolumeProbeResult contains the stats for a volume along with metadata.
type VolumeProbeResult struct {
	// VolumeType is the type of volume (data, wal, tablespace).
	VolumeType VolumeType `json:"volumeType"`
	// Tablespace is the tablespace name if VolumeType is tablespace, empty otherwise.
	Tablespace string `json:"tablespace,omitempty"`
	// MountPath is the filesystem mount path that was probed.
	MountPath string `json:"mountPath"`
	// Stats contains the disk usage statistics.
	Stats VolumeStats `json:"stats"`
}
