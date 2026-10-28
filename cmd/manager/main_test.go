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

package main

import (
	stdlog "log"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("logging options of the manager subcommands", func() {
	It("keeps log sampling for the controller subcommand", func() {
		root := newRootCmd()
		controllerCmd, _, err := root.Find([]string{"controller"})
		Expect(err).ToNot(HaveOccurred())
		// only the OpenTelemetry export
		Expect(loggingOptions(controllerCmd)).To(HaveLen(1))
	})

	It("disables log sampling for the instance subcommands", func() {
		root := newRootCmd()
		runCmd, _, err := root.Find([]string{"instance", "run"})
		Expect(err).ToNot(HaveOccurred())
		Expect(loggingOptions(runCmd)).To(HaveLen(2))
	})

	It("disables log sampling three levels below the root command", func() {
		root := newRootCmd()
		executeCmd, _, err := root.Find([]string{"instance", "upgrade", "execute"})
		Expect(err).ToNot(HaveOccurred())
		Expect(loggingOptions(executeCmd)).To(HaveLen(2))
	})

	DescribeTable("exports the records via OpenTelemetry only from the main process of a container",
		func(args []string, exported bool) {
			restoreGlobalLoggers()
			// never contacted: the exporter connects only to send records
			GinkgoT().Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")

			cmd, _, err := newRootCmd().Find(args)
			Expect(err).ToNot(HaveOccurred())
			Expect(cmd.Name()).To(Equal(args[len(args)-1]))
			logFlags := &log.Flags{}
			logFlags.ConfigureLogging(loggingOptions(cmd)...)

			Expect(log.OpenTelemetryLogger().Enabled()).To(Equal(exported))
		},
		Entry("controller", []string{"controller"}, true),
		Entry("instance run", []string{"instance", "run"}, true),
		Entry("instance init", []string{"instance", "init"}, true),
		Entry("instance upgrade execute", []string{"instance", "upgrade", "execute"}, true),
		Entry("pgbouncer run", []string{"pgbouncer", "run"}, true),
		Entry("wal-archive", []string{"wal-archive"}, false),
		Entry("wal-restore", []string{"wal-restore"}, false),
		Entry("backup", []string{"backup"}, false),
	)

	It("configures unsampled logging when running a pod-facing subcommand", func() {
		// must exceed the 100 msgs/s initial-pass threshold of the sampler
		// installed by the controller-runtime zap builder
		const burst = 300

		restoreGlobalLoggers()

		dest := filepath.Join(GinkgoT().TempDir(), "log")
		root := newRootCmd()
		root.SetArgs([]string{"version", "--log-destination", dest})
		Expect(root.Execute()).To(Succeed())

		for range burst {
			log.Info("burst-marker")
		}

		content, err := os.ReadFile(dest) //nolint:gosec
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Count(string(content), `"msg":"burst-marker"`)).To(Equal(burst),
			"a burst of identical messages must not be sampled away")
	})
})

// restoreGlobalLoggers restores, after the spec, the global loggers that
// ConfigureLogging points at its configuration, including the stdlib "log"
// package, so later specs are not affected
func restoreGlobalLoggers() {
	previousLogger := log.GetLogger().GetLogger()
	previousStdLogOutput := stdlog.Writer()
	previousStdLogFlags := stdlog.Flags()
	previousStdLogPrefix := stdlog.Prefix()
	DeferCleanup(func() {
		log.SetLogger(previousLogger)
		ctrl.SetLogger(previousLogger)
		klog.SetLogger(previousLogger)
		stdlog.SetOutput(previousStdLogOutput)
		stdlog.SetFlags(previousStdLogFlags)
		stdlog.SetPrefix(previousStdLogPrefix)
	})
}
