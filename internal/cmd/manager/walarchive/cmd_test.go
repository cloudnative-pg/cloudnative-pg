package walarchive

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("gatherWALFiles", func() {
	var (
		ctx              context.Context
		tempDir          string
		pgWalDir         string
		archiveStatusDir string
	)

	BeforeEach(func() {
		ctx = context.TODO()
		tempDir = GinkgoT().TempDir()
		err := os.Setenv("PGDATA", tempDir)
		Expect(err).ToNot(HaveOccurred())
		pgWalDir = filepath.Join(tempDir, "pg_wal")
		archiveStatusDir = filepath.Join(pgWalDir, "archive_status")
		Expect(os.MkdirAll(archiveStatusDir, 0o750)).To(Succeed())
	})

	It("returns an empty list when no WAL files are found", func() {
		result := gatherWALFiles(ctx)
		Expect(result).To(BeEmpty())
	})

	It("returns a list of WAL files excluding empty strings", func() {
		Expect(os.WriteFile(filepath.Join(archiveStatusDir, "000000010000000000000002.ready"), []byte{}, 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(archiveStatusDir, "000000010000000000000003.ready"), []byte{}, 0o600)).To(Succeed())

		result := gatherWALFiles(ctx)
		Expect(result).To(Equal([]string{"pg_wal/000000010000000000000002", "pg_wal/000000010000000000000003"}))
	})

	It("returns a list of WAL files when all are valid", func() {
		Expect(os.WriteFile(filepath.Join(archiveStatusDir, "000000010000000000000002.ready"), []byte{}, 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(archiveStatusDir, "000000010000000000000003.ready"), []byte{}, 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(archiveStatusDir, "000000010000000000000004.ready"), []byte{}, 0o600)).To(Succeed())

		result := gatherWALFiles(ctx)
		Expect(result).To(Equal(
			[]string{
				"pg_wal/000000010000000000000002",
				"pg_wal/000000010000000000000003",
				"pg_wal/000000010000000000000004",
			}))
	})
})
