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

package writepath

import (
	"sync"
	"time"
)

// State tracks whether the write path of a primary instance is stalled.
//
// A heartbeat is "in flight" between BeatStarted and BeatFinished. When an
// in-flight heartbeat lasts longer than the configured timeout, the write path
// is considered stalled. The stall is sticky: it is cleared only after a
// configurable number of consecutive successful heartbeats, and never once it
// has been latched (that is, once a failover away from this instance started).
//
// The zero value is ready to use and safe for concurrent use.
type State struct {
	mu sync.Mutex

	stalled   bool
	latched   bool
	successes int

	inflight bool
	started  time.Time
}

// Stalled returns true when the write path is currently considered stalled
func (s *State) Stalled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stalled
}

// Latched returns true when the stall has been latched
func (s *State) Latched() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latched
}

// BeatStarted records the start of a heartbeat
func (s *State) BeatStarted(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inflight = true
	s.started = now
}

// BeatFinished records the end of a heartbeat. It returns true if this call
// cleared the stall. A failed heartbeat is not a stall: it interrupts the
// series of consecutive successes but never sets the stall.
func (s *State) BeatFinished(err error, clearAfter int) (cleared bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.inflight = false
	if err != nil {
		s.successes = 0
		return false
	}

	s.successes++
	if s.stalled && !s.latched && s.successes >= max(clearAfter, 1) {
		s.stalled = false
		s.successes = 0
		return true
	}
	return false
}

// Check evaluates an in-flight heartbeat against the timeout. It returns true
// when this call moved the state from healthy to stalled.
func (s *State) Check(now time.Time, timeout time.Duration) (newlyStalled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.inflight || s.stalled || now.Sub(s.started) <= timeout {
		return false
	}
	s.stalled = true
	s.successes = 0
	return true
}

// Latch makes a stall permanent for the life of this instance manager. It
// has no effect when the write path is not stalled.
func (s *State) Latch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stalled {
		s.latched = true
	}
}

// Reset forgets everything, unless the stall is latched. It is used when the
// check is disabled or the instance is not a primary.
func (s *State) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latched {
		return
	}
	s.stalled = false
	s.successes = 0
	s.inflight = false
}
