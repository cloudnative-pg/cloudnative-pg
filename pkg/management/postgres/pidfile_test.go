/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package postgres

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mitchellh/go-ps"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the detection of a postmaster process using the pid file", func() {
	var pgdata string
	var socketDir string

	_ = BeforeEach(func() {
		var err error
		pgdata, err = ioutil.TempDir("", "cleanup-stale-pid-file-pgdata-")
		Expect(err).NotTo(HaveOccurred())
		socketDir, err = ioutil.TempDir("", "cleanup-stale-pid-file-socketdir-")
		Expect(err).NotTo(HaveOccurred())
	})

	_ = AfterEach(func() {
		Expect(os.RemoveAll(pgdata)).To(Succeed())
		Expect(os.RemoveAll(socketDir)).To(Succeed())
	})

	It("works if the file is not present", func() {
		instance := NewInstance()
		instance.PgData = pgdata
		instance.SocketDirectory = socketDir
		process, err := instance.CheckForExistingPostmaster(postgresName)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(process).To(BeNil())
	})

	It("works if the file is present and does not contain a PID of a valid process", func() {
		instance := NewInstance()
		instance.PgData = pgdata
		instance.SocketDirectory = socketDir

		pidFile := filepath.Join(pgdata, PostgresqlPidFile)
		err := ioutil.WriteFile(pidFile, []byte("1234"), 0o400)
		Expect(err).ShouldNot(HaveOccurred())

		lockFile := filepath.Join(socketDir, PostgresqlPidFile)
		err = ioutil.WriteFile(filepath.Join(socketDir, ".s.PGSQL.5432.lock"), []byte("1234"), 0o400)
		Expect(err).ShouldNot(HaveOccurred())

		process, err := instance.CheckForExistingPostmaster(postgresName)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(process).To(BeNil())

		result, err := fileutils.FileExists(pidFile)
		Expect(err).To(BeNil())
		Expect(result).To(BeFalse())

		result, err = fileutils.FileExists(lockFile)
		Expect(err).To(BeNil())
		Expect(result).To(BeFalse())
	})

	It("works if the file is present and contains a PID of a valid process", func() {
		myPid := os.Getpid()
		instance := NewInstance()
		instance.PgData = pgdata
		instance.SocketDirectory = socketDir
		err := ioutil.WriteFile(
			filepath.Join(pgdata, PostgresqlPidFile),
			[]byte(fmt.Sprintf("%v", myPid)), 0o400)
		Expect(err).ShouldNot(HaveOccurred())
		myProcess, err := ps.FindProcess(myPid)
		Expect(err).ShouldNot(HaveOccurred())
		myExecutable := myProcess.Executable()

		process, err := instance.CheckForExistingPostmaster(myExecutable)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(process).ToNot(BeNil())
	})
})
