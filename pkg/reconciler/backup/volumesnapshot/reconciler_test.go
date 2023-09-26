package volumesnapshot

import (
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getSnapshotName", func() {
	It("should return only the backup name when the role is PVCRolePgData", func() {
		name, err := getSnapshotName("backup123", utils.PVCRolePgData)
		Expect(err).NotTo(HaveOccurred())
		Expect(name).To(Equal("backup123"))
	})

	It("should return only the backup name when the role is an empty string", func() {
		name, err := getSnapshotName("backup123", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(name).To(Equal("backup123"))
	})

	It("should append '-wal' to the backup name when the role is PVCRolePgWal", func() {
		name, err := getSnapshotName("backup123", utils.PVCRolePgWal)
		Expect(err).NotTo(HaveOccurred())
		Expect(name).To(Equal("backup123-wal"))
	})

	It("should return an error for unhandled PVCRole types", func() {
		_, err := getSnapshotName("backup123", "UNKNOWN_ROLE")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("unhandled PVCRole type: UNKNOWN_ROLE"))
	})
})
