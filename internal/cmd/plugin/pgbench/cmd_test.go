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

package pgbench

import (
	"github.com/spf13/cobra"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NewCmd", func() {
	It("should create a cobra.Command with correct defaults", func() {
		cmd := NewCmd()

		Expect(cmd.Use).To(Equal("pgbench [cluster] [-- pgBenchCommandArgs...]"))
		Expect(cmd.Short).To(Equal("Creates a pgbench job"))
		Expect(cmd.Long).To(Equal("Creates a pgbench job to run against the specified Postgres Cluster."))
		Expect(cmd.Example).To(Equal(jobExample))

		// Test the flags.
		jobNameFlag := cmd.Flag("job-name")
		Expect(jobNameFlag).ToNot(BeNil())
		Expect(jobNameFlag.DefValue).To(Equal(""))

		dbNameFlag := cmd.Flag("db-name")
		Expect(dbNameFlag).ToNot(BeNil())
		Expect(dbNameFlag.DefValue).To(Equal("app"))

		dryRunFlag := cmd.Flag("dry-run")
		Expect(dryRunFlag).ToNot(BeNil())
		Expect(dryRunFlag.DefValue).To(Equal("false"))

		nodeSelectorFlag := cmd.Flag("node-selector")
		Expect(nodeSelectorFlag).ToNot(BeNil())
		Expect(nodeSelectorFlag.DefValue).To(Equal("[]"))

		initializeFlag := cmd.Flag("initialize")
		Expect(initializeFlag).ToNot(BeNil())
		Expect(initializeFlag.DefValue).To(Equal("false"))

		scaleFlag := cmd.Flag("scale")
		Expect(scaleFlag).ToNot(BeNil())
		Expect(scaleFlag.DefValue).To(Equal("1"))
	})

	It("should correctly parse flags and arguments", func() {
		cmd := NewCmd()

		// Create a test run
		testRun := &pgBenchRun{}

		// Replace RunE function with a test function
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			testRun.jobName, _ = cmd.Flags().GetString("job-name")
			testRun.dbName, _ = cmd.Flags().GetString("db-name")
			testRun.dryRun, _ = cmd.Flags().GetBool("dry-run")
			testRun.nodeSelector, _ = cmd.Flags().GetStringSlice("node-selector")
			testRun.initialize, _ = cmd.Flags().GetBool("initialize")
			testRun.scale, _ = cmd.Flags().GetInt("scale")

			testRun.clusterName = args[0]
			testRun.pgBenchCommandArgs = args[1:]
			return nil
		}

		// Set flags and arguments.
		args := []string{
			"mycluster",
			"--job-name=myjob",
			"--db-name=mydb",
			"--dry-run=true",
			"--node-selector=label=value",
			"--initialize=true",
			"--scale=2",
			"arg1",
			"arg2",
		}

		cmd.SetArgs(args)

		// Execute command.
		err := cmd.Execute()
		Expect(err).ToNot(HaveOccurred())

		// Check values.
		Expect(testRun.jobName).To(Equal("myjob"))
		Expect(testRun.clusterName).To(Equal("mycluster"))
		Expect(testRun.dbName).To(Equal("mydb"))
		Expect(testRun.dryRun).To(BeTrue())
		Expect(testRun.nodeSelector).To(Equal([]string{"label=value"}))
		Expect(testRun.initialize).To(BeTrue())
		Expect(testRun.scale).To(Equal(2))
		Expect(testRun.pgBenchCommandArgs).To(Equal([]string{"arg1", "arg2"}))
	})
})
