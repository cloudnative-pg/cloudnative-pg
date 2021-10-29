/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("the detection of a postmaster process using the pid file", func() {
	var tmpDir string

	_ = BeforeEach(func() {
		var err error
		tmpDir, err = ioutil.TempDir("", "cleanup-stale-pid-file-")
		Expect(err).NotTo(HaveOccurred())
	})

	_ = AfterEach(func() {
		Expect(os.RemoveAll(tmpDir)).To(Succeed())
	})

	It("works if the file is not present", func() {
		instance := &Instance{PgData: tmpDir}
		process, err := instance.CheckForExistingPostmaster()
		Expect(err).ShouldNot(HaveOccurred())
		Expect(process).To(BeNil())
	})

	It("works if the file is present", func() {
		instance := &Instance{PgData: tmpDir}
		err := ioutil.WriteFile(filepath.Join(tmpDir, PostgresqlPidFile), []byte("1234"), 0o400)
		Expect(err).ShouldNot(HaveOccurred())

		process, err := instance.CheckForExistingPostmaster()
		Expect(err).ShouldNot(HaveOccurred())
		Expect(process).To(BeNil())
	})

	It("works if the file is present and contains a PID of a valid process", func() {
		instance := &Instance{PgData: tmpDir}
		err := ioutil.WriteFile(
			filepath.Join(tmpDir, PostgresqlPidFile),
			[]byte(fmt.Sprintf("%v", os.Getpid())), 0o400)
		Expect(err).ShouldNot(HaveOccurred())

		process, err := instance.CheckForExistingPostmaster()
		Expect(err).ShouldNot(HaveOccurred())
		Expect(process).ToNot(BeNil())
	})
})
