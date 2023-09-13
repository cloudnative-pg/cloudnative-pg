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
package system

import (
	"encoding/asn1"
	"runtime"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Testing set coredump_filter", func() {
	It("properly set the default value for a cluster", func() {
		// Did not split the darwin out from windows as that is not necessary
		// we just check in the test case. for most situation, we build in darwin and
		// test in linux based docker
		if runtime.GOOS != "linux" {
			return
		}

		coredumpFilter := "0x31"
		err := SetCoredumpFilter(coredumpFilter)
		Expect(err).ToNot(HaveOccurred())

		content, err := fileutils.ReadFile("/proc/self/coredump_filter")
		Expect(err).ToNot(HaveOccurred())
		bit := asn1.BitString{Bytes: content, BitLength: 9}
		// string 0x31 it's translated into bits 2 and 3 being on
		// check https://docs.kernel.org/filesystems/proc.html#proc-pid-coredump-filter-core-dump-filtering-settings
		// for more information on the subject
		Expect(bit.At(2)).To(Equal(1))
		Expect(bit.At(3)).To(Equal(1))
	})
})
