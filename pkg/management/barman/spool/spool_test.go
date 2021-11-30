/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package spool

import (
	"io/ioutil"
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Spool", func() {
	var tmpDir string
	var spool *WALSpool

	_ = BeforeEach(func() {
		var err error
		tmpDir, err = ioutil.TempDir("", "spool-test-")
		Expect(err).NotTo(HaveOccurred())

		spool, err = New(tmpDir)
		Expect(err).NotTo(HaveOccurred())
	})

	_ = AfterEach(func() {
		Expect(os.RemoveAll(tmpDir)).To(Succeed())
	})

	It("create and removes files from/into the spool", func() {
		var err error
		walFile := "000000020000068A00000001"

		// This WAL file doesn't exist
		Expect(spool.Contains(walFile)).To(BeFalse())

		// If I try to remove a WAL file that doesn't exist, I obtain an error
		err = spool.Remove(walFile)
		Expect(err).To(HaveOccurred())

		// I add it into the spool
		err = spool.Add(walFile)
		Expect(err).NotTo(HaveOccurred())

		// Now the file exists
		Expect(spool.Contains(walFile)).To(BeTrue())

		// I can now remove it
		err = spool.Remove(walFile)
		Expect(err).NotTo(HaveOccurred())

		// And now it doesn't exist again
		Expect(spool.Contains(walFile)).To(BeFalse())
	})
})
