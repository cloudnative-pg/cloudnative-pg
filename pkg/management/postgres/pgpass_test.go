/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package postgres

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pgpass generation", func() {
	It("can generate a .pgpass line", func() {
		entry := PgPassEntry{
			HostName: "thishost",
			Port:     5432,
			DBName:   "testdb",
			Username: "testuser",
			Password: "testpassword",
		}

		Expect(entry.CreatePgPassLine()).To(Equal("thishost:5432:testdb:testuser:testpassword\n"))
	})

	It("can generate a whole pgpass file", func() {
		entries := []PgPassEntry{
			{
				HostName: "thishost",
				Port:     5432,
				DBName:   "testdb",
				Username: "testuser",
				Password: "testpassword",
			},
			{
				HostName: "thishost",
				Port:     5432,
				DBName:   "replication",
				Username: "testuser",
				Password: "testpassword2",
			},
		}

		Expect(CreatePgPassContent(entries)).To(Equal(
			"thishost:5432:testdb:testuser:testpassword\n" +
				"thishost:5432:replication:testuser:testpassword2\n"))
	})
})
