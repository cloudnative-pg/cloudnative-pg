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
	"bytes"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// buildTestCore constructs a zapcore.Core that writes JSON to buf.
// When sampled is true it wraps the core in the same sampler that
// controller-runtime adds in production mode (100 initial, 100 thereafter
// per second). When sampled is false no sampler is applied — this mirrors
// what buildUnsampledLogger does.
func buildTestCore(buf *bytes.Buffer, sampled bool) zapcore.Core {
	enc := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	ws := zapcore.AddSync(buf)
	core := zapcore.NewCore(enc, ws, zap.InfoLevel)
	if sampled {
		return zapcore.NewSamplerWithOptions(core, time.Second, 100, 100)
	}
	return core
}

var _ = Describe("LogRecordWriter", func() {
	const burst = 200 // must exceed the sampler's initial-pass threshold of 100

	Context("unsampled logger (the fix)", func() {
		It("forwards every audit record even during a burst", func() {
			buf := &bytes.Buffer{}
			logger := zap.New(buildTestCore(buf, false))

			for i := 0; i < burst; i++ {
				logger.Info(logRecordKey)
			}
			Expect(logger.Sync()).To(Succeed())

			lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
			Expect(lines).To(HaveLen(burst),
				"unsampled logger must emit all %d records; got %d — sampler may still be active", burst, len(lines))
		})
	})

	Context("sampled logger (the bug)", func() {
		It("drops audit records after the first 100 within a second", func() {
			buf := &bytes.Buffer{}
			logger := zap.New(buildTestCore(buf, true))

			for i := 0; i < burst; i++ {
				logger.Info(logRecordKey)
			}
			Expect(logger.Sync()).To(Succeed())

			lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
			// Sampler allows first 100 through, then 1-in-100 afterwards.
			// With 200 identical messages in a single second we expect well
			// under 200 lines — the exact count varies but is always < burst.
			Expect(len(lines)).To(BeNumerically("<", burst),
				"sampled logger should drop messages beyond 100/s; got %d lines for %d messages", len(lines), burst)
		})
	})
})
