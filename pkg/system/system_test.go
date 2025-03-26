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

package system

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Testing set coredump_filter", Ordered, func() {
	var origFilter string
	BeforeAll(func() {
		// Did not split the darwin out from windows as that is not necessary
		// we just check in the test case. for most situation, we build in darwin and
		// test in linux based docker
		if runtime.GOOS != "linux" {
			Skip("coredump_filter is supported only on linux systems")
		}

		filter, err := fileutils.ReadFile("/proc/self/coredump_filter")
		Expect(err).ToNot(HaveOccurred())
		origFilter, err = parseCoredumpFilter(filter)
		Expect(err).ToNot(HaveOccurred())
	})
	AfterAll(func() {
		if origFilter == "" {
			return
		}
		err := os.WriteFile("/proc/self/coredump_filter", []byte(origFilter), 0o600)
		Expect(err).ToNot(HaveOccurred())
	})
	It("properly set the default value for a cluster", func() {
		coredumpFilter := "0x31"
		err := SetCoredumpFilter(coredumpFilter)
		Expect(err).ToNot(HaveOccurred())

		content, err := fileutils.ReadFile("/proc/self/coredump_filter")
		Expect(err).ToNot(HaveOccurred())
		Expect(parseCoredumpFilter(content)).To(Equal(coredumpFilter))
	})
})

func parseCoredumpFilter(content []byte) (string, error) {
	filterInt, err := strconv.ParseInt(strings.TrimSpace(string(content)), 16, 0)
	if err != nil {
		return "", err
	}

	filterStr := fmt.Sprintf("0x%x", filterInt)
	return filterStr, nil
}
