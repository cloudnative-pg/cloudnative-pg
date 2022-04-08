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

package configfile

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("configuration file parser", func() {
	It("return an empty list of lines when the content is empty", func() {
		Expect(splitLines("")).To(Equal([]string{}))
		Expect(splitLines("\n")).To(Equal([]string{}))
	})

	It("correctly split in lines", func() {
		Expect(splitLines("one\ntwo\nthree\n")).To(Equal([]string{"one", "two", "three"}))
	})
})
