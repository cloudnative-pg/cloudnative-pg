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

package logpipe

import (
	"context"
	"path/filepath"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LineLogPipe", func() {
	When("used as a log pipe", func() {
		It("resolves the initialized condition with the context error when cancelled ", func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			p := &LineLogPipe{
				fileName:    filepath.Join(GinkgoT().TempDir(), "postgres.json"),
				handler:     func([]byte) {},
				initialized: concurrency.NewExecuted(),
				exited:      concurrency.NewExecuted(),
			}

			Expect(p.Start(ctx)).To(Succeed())
			Expect(waitForCondition(p.GetExecutedCondition())).To(MatchError(context.Canceled))
			Expect(waitForCondition(p.GetExitedCondition())).ToNot(HaveOccurred())
		})

		It("keeps the recorded success once the FIFO is ready, even after the context is later cancelled", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			p := &LineLogPipe{
				fileName:    filepath.Join(GinkgoT().TempDir(), "postgres.json"),
				handler:     func([]byte) {},
				initialized: concurrency.NewExecuted(),
				exited:      concurrency.NewExecuted(),
			}

			startExited := make(chan struct{})
			go func() {
				defer close(startExited)
				_ = p.Start(ctx)
			}()

			// The FIFO gets created with no interference here, so initialized
			// must resolve with a nil error before the context is cancelled.
			Expect(waitForCondition(p.GetExecutedCondition())).ToNot(HaveOccurred())

			cancel()
			Eventually(startExited, 5*time.Second, 10*time.Millisecond).Should(BeClosed())

			// The deferred BroadcastError(ctx.Err()) on exit must not clobber
			// the success already recorded above.
			Expect(p.GetExecutedCondition().Err()).ToNot(HaveOccurred())
		})
	})
})
