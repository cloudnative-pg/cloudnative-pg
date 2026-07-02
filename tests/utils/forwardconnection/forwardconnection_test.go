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

package forwardconnection

import (
	"fmt"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("isExpectedPortForwardError", func() {
	It("returns true for closing listener errors", func() {
		err := fmt.Errorf("error closing listener: close tcp4 127.0.0.1:45407: use of closed network connection")
		Expect(isExpectedPortForwardError(err)).To(BeTrue())
	})

	It("returns true for forwarding errors with connection reset", func() {
		err := fmt.Errorf("an error occurred forwarding 45407 -> 5432: error forwarding port 5432 to pod abc: " +
			"read: connection reset by peer")
		Expect(isExpectedPortForwardError(err)).To(BeTrue())
	})

	It("returns false for unrelated errors", func() {
		err := fmt.Errorf("connection refused")
		Expect(isExpectedPortForwardError(err)).To(BeFalse())
	})

	It("returns false for nil error", func() {
		Expect(isExpectedPortForwardError(nil)).To(BeFalse())
	})
})

var _ = Describe("ForwardConnection Close", func() {
	It("does not panic when called multiple times", func() {
		fc := &ForwardConnection{
			stopChannel: make(chan struct{}),
			done:        make(chan struct{}),
		}
		fc.started.Store(true)
		// Simulate a goroutine that exits when stopChannel is closed
		go func() {
			<-fc.stopChannel
			close(fc.done)
		}()

		Expect(func() {
			fc.Close()
			fc.Close()
		}).ToNot(Panic())
	})

	It("signals stopChannel and waits for done", func() {
		fc := &ForwardConnection{
			stopChannel: make(chan struct{}),
			done:        make(chan struct{}),
		}
		fc.started.Store(true)

		var stopped atomic.Bool
		go func() {
			<-fc.stopChannel
			stopped.Store(true)
			close(fc.done)
		}()

		fc.Close()
		Expect(stopped.Load()).To(BeTrue(), "Close should have signaled stopChannel")
	})

	It("works when the goroutine has already exited", func() {
		fc := &ForwardConnection{
			stopChannel: make(chan struct{}),
			done:        make(chan struct{}),
		}
		fc.started.Store(true)
		// Simulate a goroutine that already finished (e.g. StartAndWait
		// returned via errChan)
		close(fc.done)

		Expect(func() { fc.Close() }).ToNot(Panic())
	})

	It("does not block when StartAndWait was never called", func() {
		fc := &ForwardConnection{
			stopChannel: make(chan struct{}),
			done:        make(chan struct{}),
		}

		Expect(func() { fc.Close() }).ToNot(Panic())
	})
})
