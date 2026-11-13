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

package utils

import (
	"encoding/json"
	"fmt"
)

// The states of a LeasePosition are the ones of PgDataState, plus two of ours.
// Which of the PgDataState ones are written is up to the writer, see the
// comment of the LeasePosition type below.
const (
	// LeasePositionStateUnknown is written when the position could not be
	// determined, and carries no position
	LeasePositionStateUnknown PgDataState = "unknown"

	// LeasePositionStatePromoting is written by a replica right before it
	// promotes, and carries the position of the replica itself
	LeasePositionStatePromoting PgDataState = "promoting"
)

// LeasePosition is the value of the cnpg.io/position annotation of the primary lease
type LeasePosition struct {
	// LSN is the position in the WAL, in the PostgreSQL text format, up to
	// which the writer has written: the flush position while the primary runs,
	// and once it stopped, the end of the WAL before the REDO location of its
	// latest checkpoint, which after a clean shutdown is all of its data.
	// Empty when unknown.
	LSN string `json:"lsn,omitempty"`

	// Timeline is the PostgreSQL timeline. Zero when unknown.
	Timeline int `json:"timeline,omitempty"`

	// State qualifies the position. Only PgDataStateShutDown is a final one.
	State PgDataState `json:"state"`

	// Pod is the pod that wrote the position. Informational.
	Pod string `json:"pod,omitempty"`
}

// Marshal returns the value to be stored in the annotation
func (p LeasePosition) Marshal() (string, error) {
	result, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(result), nil
}

// ParseLeasePosition parses the value of the annotation. It fails when the
// value is not a JSON document or its state is not a known one, so that a
// reader does not act on something it does not understand.
func ParseLeasePosition(value string) (*LeasePosition, error) {
	var result LeasePosition
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return nil, fmt.Errorf("while parsing the lease position: %w", err)
	}

	switch result.State {
	case PgDataStateShutDown,
		PgDataStateShutDownInRecovery,
		PgDataStateStartingUp,
		PgDataStateShuttingDown,
		PgDataStateInCrashRecovery,
		PgDataStateInArchiveRecovery,
		PgDataStateInProduction,
		LeasePositionStateUnknown,
		LeasePositionStatePromoting:
		return &result, nil
	}

	return nil, fmt.Errorf("unknown lease position state: %q", result.State)
}
