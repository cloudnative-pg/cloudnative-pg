/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package plugin

import (
	"github.com/logrusorgru/aurora/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/spf13/cobra"
)

var _ = FDescribe("Configure color", func() {
	var cmd *cobra.Command
	BeforeEach(func() {
		cmd = &cobra.Command{
			Use: "test",
		}

		AddColorControlFlags(cmd)
	})

	It("enables color as the a terminal is attached", func() {
		err := cmd.ParseFlags(nil)
		Expect(err).NotTo(HaveOccurred())

		err = configureColor(cmd, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(aurora.DefaultColorizer.Config().Colors).To(BeTrue())
	})

	It("disables color as the a terminal is not attached", func() {
		err := cmd.ParseFlags(nil)
		Expect(err).NotTo(HaveOccurred())

		err = configureColor(cmd, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(aurora.DefaultColorizer.Config().Colors).To(BeFalse())
	})

	It("enables color as the flag is set", func() {
		err := cmd.ParseFlags([]string{"--colors"})
		Expect(err).NotTo(HaveOccurred())

		err = configureColor(cmd, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(aurora.DefaultColorizer.Config().Colors).To(BeTrue())
	})

	It("disables color as the flag is set", func() {
		err := cmd.ParseFlags([]string{"--no-colors"})
		Expect(err).NotTo(HaveOccurred())

		err = configureColor(cmd, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(aurora.DefaultColorizer.Config().Colors).To(BeFalse())
	})
})
