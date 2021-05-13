/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package pool

import (
	_ "github.com/lib/pq"

	. "github.com/onsi/ginkgo"
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
