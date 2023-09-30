package v1

import (
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("", func() {
	It("doesn't complain if VolumeSnapshot CRD is present", func() {
		backup := &Backup{
			Spec: BackupSpec{
				Method: BackupMethodVolumeSnapshot,
			},
		}
		utils.SetVolumeSnapshot(true)
		result := backup.validate()
		Expect(result).To(BeEmpty())
	})

	It("complains if VolumeSnapshot CRD is not present", func() {
		backup := &Backup{
			Spec: BackupSpec{
				Method: BackupMethodVolumeSnapshot,
			},
		}
		utils.SetVolumeSnapshot(false)
		result := backup.validate()
		Expect(result).To(HaveLen(1))
		Expect(result[0].Field).To(Equal("spec.method"))
	})
})
