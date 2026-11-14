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

// Package writepath detects a stalled write path on a primary instance.
package writepath

import (
	"context"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/utils/clock"
)

// checkInterval is how often an in-flight heartbeat is compared with the timeout
const checkInterval = time.Second

// Config is the effective configuration of the write path check
type Config struct {
	Enabled             bool
	Period              time.Duration
	Timeout             time.Duration
	ClearAfterSuccesses int
}

// Monitor runs the heartbeat loop and evaluates the State
type Monitor struct {
	// State is the observed state
	State *State

	// Clock is the time source, replaceable in tests
	Clock clock.Clock

	// Config returns the current configuration
	Config func() Config

	// ShouldRun tells whether a heartbeat makes sense now (the instance is a
	// running primary)
	ShouldRun func() bool

	// Beat performs one heartbeat
	Beat func(ctx context.Context) error

	// FailoverStarted tells whether the cluster moved the target primary away
	// from this instance. When it is true during a stall, the stall is latched.
	FailoverStarted func() bool

	// OnStalled is called once when the write path becomes stalled
	OnStalled func()

	// OnCleared is called once when the stall is cleared
	OnCleared func()
}

// NeedLeaderElection implements the controller-runtime LeaderElectionRunnable
// interface: the monitor must run on every instance
func (m *Monitor) NeedLeaderElection() bool {
	return false
}

// Start runs the monitor until the context is done
func (m *Monitor) Start(ctx context.Context) error {
	go m.watchLoop(ctx)
	m.beatLoop(ctx)
	return nil
}

// beatLoop starts a heartbeat, waits for it to complete, then waits one period.
// A hung heartbeat blocks this loop by design: a new heartbeat is started only
// when the previous one is done. Detection is done by watchLoop.
func (m *Monitor) beatLoop(ctx context.Context) {
	contextLogger := log.FromContext(ctx).WithName("write-path-check")

	for {
		cfg := m.Config()
		wait := cfg.Period
		if wait <= 0 {
			wait = checkInterval
		}

		select {
		case <-ctx.Done():
			return
		case <-m.Clock.After(wait):
		}

		// The configuration may have changed while waiting
		cfg = m.Config()
		if !cfg.Enabled || !m.ShouldRun() {
			m.State.Reset()
			continue
		}

		m.State.BeatStarted(m.Clock.Now())
		err := m.Beat(ctx)
		if err != nil && ctx.Err() == nil {
			contextLogger.Warning("Write path heartbeat failed, this is not considered a stall", "err", err.Error())
		}
		if m.State.BeatFinished(err, cfg.ClearAfterSuccesses) {
			contextLogger.Info("Write path of the instance is not stalled anymore")
			if m.OnCleared != nil {
				m.OnCleared()
			}
		}
		m.latchIfFailingOver()
	}
}

// watchLoop compares the in-flight heartbeat with the timeout
func (m *Monitor) watchLoop(ctx context.Context) {
	contextLogger := log.FromContext(ctx).WithName("write-path-check")

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.Clock.After(checkInterval):
		}

		cfg := m.Config()
		if !cfg.Enabled {
			continue
		}
		if m.State.Check(m.Clock.Now(), cfg.Timeout) {
			contextLogger.Warning("Write path heartbeat did not complete in time, "+
				"the write path of this instance is considered stalled",
				"timeout", cfg.Timeout.String())
			if m.OnStalled != nil {
				m.OnStalled()
			}
		}
		m.latchIfFailingOver()
	}
}

func (m *Monitor) latchIfFailingOver() {
	if m.State.Stalled() && m.FailoverStarted != nil && m.FailoverStarted() {
		m.State.Latch()
	}
}
