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

package url

import (
	"net"
	neturl "net/url"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("URL", func() {
	cases := []struct {
		scheme string
		host   string
		port   int32
		path   string
		expect string
	}{
		{
			// path has leading slash
			scheme: "http",
			host:   "localhost",
			port:   1234,
			path:   "/foo",
			expect: "http://localhost:1234/foo",
		},
		{
			// path has no leading slash
			scheme: "http",
			host:   "localhost",
			port:   1234,
			path:   "foo",
			expect: "http://localhost:1234/foo",
		},
		{
			// ipv4 address
			scheme: "http",
			host:   "1.2.3.4",
			port:   1234,
			path:   "foo",
			expect: "http://1.2.3.4:1234/foo",
		},
		{
			// ipv6 address
			scheme: "http",
			host:   "1:2:3::4",
			port:   1234,
			path:   "foo",
			expect: "http://[1:2:3::4]:1234/foo",
		},
	}

	It("generates the expected url", func() {
		for _, unit := range cases {
			Expect(Build(unit.scheme, unit.host, unit.path, unit.port)).To(Equal(unit.expect))
		}
	})

	It("roundtrips", func() {
		for _, unit := range cases {
			url := Build(unit.scheme, unit.host, unit.path, unit.port)
			parsed, err := neturl.Parse(url)
			Expect(err).ToNot(HaveOccurred())
			Expect(parsed.Scheme).To(Equal(unit.scheme))
			port := strconv.Itoa(int(unit.port))
			Expect(parsed.Host).To(Equal(net.JoinHostPort(unit.host, port)))
			Expect(parsed.Port()).To(Equal(port))
			// parsed.Path always has a leading slash
			Expect(parsed.Path).To(Equal("/" + strings.TrimPrefix(unit.path, "/")))
		}
	})
})
