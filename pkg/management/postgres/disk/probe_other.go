//go:build !linux

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
	"errors"
)

// ErrNotSupported is returned when disk probing is attempted on non-Linux platforms.
var ErrNotSupported = errors.New("disk probing is only supported on Linux")

// Probe probes a filesystem mount point using statfs and returns VolumeStats.
// On non-Linux platforms, this is a stub that returns an error.
type Probe struct{}

// NewProbe creates a new Probe. On non-Linux platforms, this returns a stub.
func NewProbe() *Probe {
	return &Probe{}
}

// GetVolumeStats is not supported on non-Linux platforms.
func (p *Probe) GetVolumeStats(_ string) (*VolumeStats, error) {
	return nil, ErrNotSupported
}

// ProbeVolume is not supported on non-Linux platforms.
func (p *Probe) ProbeVolume(
	_ string,
	_ VolumeType,
	_ string,
) (*VolumeProbeResult, error) {
	return nil, ErrNotSupported
}
