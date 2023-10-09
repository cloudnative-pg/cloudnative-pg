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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("testing primary instance methods", Ordered, func() {
	tempDir, err := os.MkdirTemp("", "primary")
	Expect(err).ToNot(HaveOccurred())

	instance := Instance{
		PgData: filepath.Join(tempDir, "/testdata/primary"),
	}

	signalPath := filepath.Join(instance.PgData, "standby.signal")
	postgresAutoConf := filepath.Join(instance.PgData, "postgresql.auto.conf")
	pgControl := filepath.Join(instance.PgData, "global", "pg_control")
	pgControlOld := pgControl + ".old"

	BeforeEach(func() {
		_, err := fileutils.WriteStringToFile(instance.PgData+"/PG_VERSION", "14")
		Expect(err).ToNot(HaveOccurred())
	})

	assertFileExists := func(path, name string) {
		f, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(f.Name()).To(Equal(name))
	}

	AfterEach(func() {
		_ = os.Remove(signalPath)
		_ = os.Remove(postgresAutoConf)
		_ = os.Remove(pgControl)
		_ = os.Remove(pgControlOld)
	})

	It("should correctly recognize a primary instance", func() {
		isPrimary, err := instance.IsPrimary()
		Expect(err).ToNot(HaveOccurred())
		Expect(isPrimary).To(BeTrue())

		_, err = fileutils.WriteStringToFile(signalPath, "")
		Expect(err).ToNot(HaveOccurred())
		isPrimary, err = instance.IsPrimary()
		Expect(err).ToNot(HaveOccurred())
		Expect(isPrimary).To(BeFalse())
	})

	It("should properly demote a primary", func() {
		err := instance.Demote(&apiv1.Cluster{})
		Expect(err).ToNot(HaveOccurred())

		assertFileExists(signalPath, "standby.signal")
		assertFileExists(postgresAutoConf, "postgresql.auto.conf")
	})

	It("should correctly restore pg_control from the pg_control.old file", func() {
		data := []byte("pgControlFakeData")

		err := fileutils.EnsureParentDirectoryExist(pgControlOld)
		Expect(err).ToNot(HaveOccurred())

		err = os.WriteFile(pgControlOld, data, 0o600)
		Expect(err).ToNot(HaveOccurred())

		err = instance.managePgControlFileBackup()
		Expect(err).ToNot(HaveOccurred())

		assertFileExists(pgControl, "pg_control")
	})

	It("should properly remove pg_control file", func() {
		data := []byte("pgControlFakeData")

		err := fileutils.EnsureParentDirectoryExist(pgControlOld)
		Expect(err).ToNot(HaveOccurred())

		err = os.WriteFile(pgControl, data, 0o600)
		Expect(err).ToNot(HaveOccurred())

		err = instance.removePgControlFileBackup()
		Expect(err).ToNot(HaveOccurred())
	})

	It("should fail if the pg_control file has issues", func() {
		err := fileutils.EnsureParentDirectoryExist(pgControl)
		Expect(err).ToNot(HaveOccurred())

		err = os.WriteFile(pgControl, nil, 0o600)
		Expect(err).ToNot(HaveOccurred())

		err = os.Chmod(filepath.Join(instance.PgData, "global"), 0o000)
		Expect(err).ToNot(HaveOccurred())

		err = instance.managePgControlFileBackup()
		Expect(err).To(HaveOccurred())

		err = os.Chmod(filepath.Join(instance.PgData, "global"), 0o755) //nolint:gosec
		Expect(err).ToNot(HaveOccurred())

		err = instance.managePgControlFileBackup()
		Expect(err).To(HaveOccurred())
	})

	AfterAll(func() {
		err := fileutils.RemoveDirectoryContent(tempDir)
		Expect(err).ToNot(HaveOccurred())

		err = fileutils.RemoveFile(tempDir)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("testing replica instance methods", Ordered, func() {
	tempDir, err := os.MkdirTemp("", "primary")
	Expect(err).ToNot(HaveOccurred())

	instance := Instance{
		PgData: tempDir + "/testdata/replica",
	}
	signalPath := filepath.Join(instance.PgData, "standby.signal")

	BeforeEach(func() {
		_, err := fileutils.WriteStringToFile(signalPath, "")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should correctly recognize a replica instance", func() {
		isPrimary, err := instance.IsPrimary()
		Expect(err).ToNot(HaveOccurred())
		Expect(isPrimary).To(BeFalse())
	})

	AfterAll(func() {
		err := fileutils.RemoveDirectoryContent(tempDir)
		Expect(err).ToNot(HaveOccurred())

		err = fileutils.RemoveFile(tempDir)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("testing environment variables", func() {
	It("should return the default Socket Directory", func() {
		socketDir := GetSocketDir()
		Expect(socketDir).To(BeEquivalentTo(postgres.SocketDirectory))
	})

	It("should return the default or defined PostgreSQL port", func() {
		pgPort := GetServerPort()
		Expect(pgPort).To(BeEquivalentTo(postgres.ServerPort))

		pgPortEnv := 777
		err := os.Setenv("PGPORT", fmt.Sprintf("%v", pgPortEnv))
		Expect(err).ShouldNot(HaveOccurred())
		pgPort = GetServerPort()
		Expect(pgPort).To(BeEquivalentTo(pgPortEnv))

		err = os.Setenv("PGPORT", "peggie")
		Expect(err).ShouldNot(HaveOccurred())
		pgPort = GetServerPort()
		Expect(pgPort).To(BeEquivalentTo(postgres.ServerPort))
	})
})

var _ = Describe("check atomic bool", func() {
	instance := Instance{}
	instance.mightBeUnavailable.Store(true)

	It("should indicate instance might be unavailable after fencing is set", func() {
		isFenced := instance.IsFenced()
		Expect(isFenced).To(BeFalse())

		instance.SetFencing(true)
		isFenced = instance.IsFenced()
		Expect(isFenced).To(BeTrue())
		unAvailable := instance.MightBeUnavailable()
		Expect(unAvailable).To(BeTrue())
	})

	It("should recognize whether readiness can be checked depending on the setting", func() {
		instance.SetCanCheckReadiness(false)
		canBeChecked := instance.CanCheckReadiness()
		Expect(canBeChecked).To(BeFalse())

		instance.SetCanCheckReadiness(true)
		canBeChecked = instance.CanCheckReadiness()
		Expect(canBeChecked).To(BeTrue())
	})

	It("should recognize whether the instance might be unavailable based on the setting", func() {
		instance.SetMightBeUnavailable(false)
		unAvailable := instance.MightBeUnavailable()
		Expect(unAvailable).To(BeFalse())

		instance.SetMightBeUnavailable(true)
		unAvailable = instance.MightBeUnavailable()
		Expect(unAvailable).To(BeTrue())
	})
})
