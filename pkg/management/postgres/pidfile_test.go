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

package postgres

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/mitchellh/go-ps"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the detection of a postmaster process using the pid file", func() {
	var pgdata string
	var socketDir string

	_ = BeforeEach(func() {
		var err error
		pgdata, err = os.MkdirTemp("", "cleanup-stale-pid-file-pgdata-")
		Expect(err).NotTo(HaveOccurred())
		socketDir, err = os.MkdirTemp("", "cleanup-stale-pid-file-socketdir-")
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
		err := os.WriteFile(pidFile, []byte("1234"), 0o400)
		Expect(err).ShouldNot(HaveOccurred())

		lockFile := filepath.Join(socketDir, PostgresqlPidFile)
		err = os.WriteFile(filepath.Join(socketDir, ".s.PGSQL.5432.lock"), []byte("1234"), 0o400)
		Expect(err).ShouldNot(HaveOccurred())

		process, err := instance.CheckForExistingPostmaster(postgresName)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(process).To(BeNil())

		result, err := fileutils.FileExists(pidFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeFalse())

		result, err = fileutils.FileExists(lockFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeFalse())
	})

	It("works if the file is present and contains a PID of a valid process", func() {
		myPid := os.Getpid()
		instance := NewInstance()
		instance.PgData = pgdata
		instance.SocketDirectory = socketDir
		err := os.WriteFile(
			filepath.Join(pgdata, PostgresqlPidFile),
			[]byte(fmt.Sprintf("%v", myPid)), 0o400)
		Expect(err).ShouldNot(HaveOccurred())
		myProcess, err := ps.FindProcess(myPid)
		Expect(err).ShouldNot(HaveOccurred())
		myExecutable := myProcess.Executable()

		process, err := instance.CheckForExistingPostmaster(myExecutable)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(process).ToNot(BeNil())

		_, err = instance.CheckForExistingPostmaster(myExecutable, "not_existent_executable")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(process).ToNot(BeNil())
	})
})
