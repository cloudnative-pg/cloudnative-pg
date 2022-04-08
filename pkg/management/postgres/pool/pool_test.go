/*
Copyright 2019-2022 The CloudNativePG Contributors

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

package pool

import (
	_ "github.com/lib/pq"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Connection pool test", func() {
	It("can create a new connection", func() {
		pool := NewConnectionPool("host=127.0.0.1")

		conn, err := pool.newConnection("test")
		Expect(err).ToNot(HaveOccurred())
		Expect(conn).ToNot(BeNil())
		_ = conn.Close()
	})

	It("is initially empty", func() {
		pool := NewConnectionPool("host=127.0.0.1")
		Expect(len(pool.connectionMap)).To(Equal(0))
	})

	It("stores created connections", func() {
		pool := NewConnectionPool("host=127.0.0.1")
		Expect(pool.Connection("test")).ToNot(BeNil())
		Expect(len(pool.connectionMap)).To(Equal(1))
	})

	It("shut down connections on request", func() {
		pool := NewConnectionPool("host=127.0.0.1")
		Expect(pool.Connection("test")).ToNot(BeNil())
		pool.ShutdownConnections()
		Expect(len(pool.connectionMap)).To(Equal(0))
	})
})
