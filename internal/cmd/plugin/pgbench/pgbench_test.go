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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PGBench init", func() {
	It("should return args with --initialize when initialize is true", func() {
		cmd := &pgBenchRun{
			initialize: true,
		}

		result := cmd.buildPGBenchInitArgs()
		Expect(result).To(Equal([]string{"--initialize", "--scale", "0"}))
	})

	It("should return args without --initialize when initialize is false", func() {
		cmd := &pgBenchRun{
			initialize: false,
		}

		result := cmd.buildPGBenchInitArgs()
		Expect(result).To(Equal([]string{"--scale", "0"}))
	})

	It("should properly detect the scale value", func() {
		cmd := &pgBenchRun{
			scale: 5,
		}

		result := cmd.buildPGBenchInitArgs()
		Expect(result).To(Equal([]string{"--scale", "5"}))
	})
})
