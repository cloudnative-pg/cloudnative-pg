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

package plugin

import (
	"github.com/logrusorgru/aurora/v4"
	"github.com/spf13/cobra"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Configure color", func() {
	var cmd *cobra.Command
	BeforeEach(func() {
		cmd = &cobra.Command{
			Use: "test",
		}

		AddColorControlFlag(cmd)
	})

	It("errors when the flag is invalid", func() {
		err := cmd.ParseFlags([]string{"--color", "invalid"})
		Expect(err).To(HaveOccurred())
	})

	It("when set to auto, turns colorization on or off depending on the terminal attached", func() {
		err := cmd.ParseFlags([]string{"--color", "auto"})
		Expect(err).NotTo(HaveOccurred())

		configureColor(cmd, false)
		Expect(aurora.DefaultColorizer.Config().Colors).To(BeFalse())

		configureColor(cmd, true)
		Expect(aurora.DefaultColorizer.Config().Colors).To(BeTrue())
	})

	It("if the color flag is not set, defaults to auto", func() {
		err := cmd.ParseFlags(nil)
		Expect(err).NotTo(HaveOccurred())

		configureColor(cmd, true)
		Expect(aurora.DefaultColorizer.Config().Colors).To(BeTrue())

		configureColor(cmd, false)
		Expect(aurora.DefaultColorizer.Config().Colors).To(BeFalse())
	})

	It("enables color even on a non-terminal if the flag is set to always", func() {
		err := cmd.ParseFlags([]string{"--color", "always"})
		Expect(err).NotTo(HaveOccurred())

		configureColor(cmd, false)
		Expect(aurora.DefaultColorizer.Config().Colors).To(BeTrue())
	})

	It("disables color if the flag is set to never", func() {
		err := cmd.ParseFlags([]string{"--color", "never"})
		Expect(err).NotTo(HaveOccurred())

		configureColor(cmd, true)
		Expect(aurora.DefaultColorizer.Config().Colors).To(BeFalse())
	})
})
