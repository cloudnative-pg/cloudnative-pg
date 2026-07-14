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
		Expect(loggingOptions(controllerCmd)).To(BeEmpty())
	})

	It("disables log sampling for the instance subcommands", func() {
		root := newRootCmd()
		runCmd, _, err := root.Find([]string{"instance", "run"})
		Expect(err).ToNot(HaveOccurred())
		Expect(loggingOptions(runCmd)).To(HaveLen(1))
	})

	It("configures unsampled logging when running a pod-facing subcommand", func() {
		// must exceed the 100 msgs/s initial-pass threshold of the sampler
		// installed by the controller-runtime zap builder
		const burst = 300

		// ConfigureLogging points the global loggers at this spec's
		// destination file: restore them so later specs are not affected
		previousLogger := log.GetLogger().GetLogger()
		DeferCleanup(func() {
			log.SetLogger(previousLogger)
			ctrl.SetLogger(previousLogger)
			klog.SetLogger(previousLogger)
		})

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
