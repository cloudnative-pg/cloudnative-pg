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

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// buildTestCore constructs a sampled zapcore.Core writing JSON to buf, using
// the same sampler controller-runtime installs in production mode (100
// initial, 100 thereafter per second). It demonstrates the bug this PR fixes:
// it does not exercise buildUnsampledLogger itself.
func buildTestCore(buf *bytes.Buffer) zapcore.Core {
	enc := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	ws := zapcore.AddSync(buf)
	core := zapcore.NewCore(enc, ws, zap.InfoLevel)
	return zapcore.NewSamplerWithOptions(core, time.Second, 100, 100)
}

var _ = Describe("LogRecordWriter", func() {
	const burst = 200 // must exceed the sampler's initial-pass threshold of 100

	Context("sampled logger (the bug)", func() {
		It("drops audit records after the first 100 within a second", func() {
			buf := &bytes.Buffer{}
			logger := zap.New(buildTestCore(buf))

			for range burst {
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

	Context("buildUnsampledLogger (the fix)", func() {
		It("forwards every audit record even during a burst", func() {
			buf := &bytes.Buffer{}
			logger := buildUnsampledLogger(zapcore.AddSync(buf))

			for range burst {
				logger.Info(logRecordKey)
			}

			lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
			Expect(lines).To(HaveLen(burst),
				"unsampled logger must emit all %d records; got %d", burst, len(lines))
		})

		It("honors the effective level of the global instance-manager logger", func() {
			originalLogger := log.GetLogger().GetLogger()
			DeferCleanup(func() {
				log.SetLogger(originalLogger)
			})

			errorOnlyCore := zapcore.NewCore(
				zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
				zapcore.AddSync(&bytes.Buffer{}),
				zap.ErrorLevel,
			)
			log.SetLogger(zapr.NewLogger(zap.New(errorOnlyCore)))

			buf := &bytes.Buffer{}
			logger := buildUnsampledLogger(zapcore.AddSync(buf))
			logger.Info(logRecordKey)

			Expect(buf.String()).To(BeEmpty(),
				"an Info-level record should have been filtered out by the global logger's Error level")
		})
	})
})
