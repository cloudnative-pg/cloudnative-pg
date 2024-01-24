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

package utils

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("working with PIDs", func() {
	It("should not fail with a zero or negative pid", func() {
		_, err := GetProcessByPid(0)
		Expect(err).ToNot(HaveOccurred())
		_, err = GetProcessByPid(-1)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should get the proper status for our own pid", func() {
		process, err := GetProcessByPid(os.Getpid())
		Expect(err).ToNot(HaveOccurred())
		Expect(process.Name).To(BeEquivalentTo("utils.test"))
		Expect(process.Ppid).To(BeZero())
		Expect(process.Ngid).To(BeZero())
	})

	It("should work getting all the pods", func() {
		processes, err := GetAllProcesses()
		Expect(err).ToNot(HaveOccurred())
		Expect(processes).ToNot(BeEmpty())
	})
})
