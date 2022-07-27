package postgres

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("testing primary instance methods", func() {
	instance := Instance{
		PgData: "testdata/primary",
	}
	signalPath := filepath.Clean(filepath.Join(instance.PgData, "standby.signal"))
	postgresAutoConf := filepath.Clean(filepath.Join(instance.PgData, "postgresql.auto.conf"))
	pgControl := filepath.Clean(filepath.Join(instance.PgData, "global", "pg_control"))
	pgControlOld := pgControl + ".old"

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
	})

	It("should properly demote a primary", func() {
		err := instance.Demote()
		Expect(err).ToNot(HaveOccurred())

		assertFileExists(signalPath, "standby.signal")
		assertFileExists(postgresAutoConf, "postgresql.auto.conf")
	})

	It("should correctly restore pg_control from the pg_control.old file", func() {
		data := []byte("pgControlFakeData")

		err := os.WriteFile(pgControlOld, data, 0o600)
		Expect(err).ToNot(HaveOccurred())

		err = instance.managePgControlFileBackup()
		Expect(err).ToNot(HaveOccurred())

		assertFileExists(pgControl, "pg_control")
	})
})

var _ = Describe("testing replica instance methods", func() {
	It("should correctly recognize a replica instance", func() {
		instance := Instance{
			PgData: "testdata/replica",
		}
		isPrimary, err := instance.IsPrimary()
		Expect(err).ToNot(HaveOccurred())
		Expect(isPrimary).To(BeFalse())
	})
})
