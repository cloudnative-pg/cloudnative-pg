package promotiontoken

import (
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Promotion Token Validation", func() {
	var (
		validToken *utils.PgControldataTokenContent
	)

	BeforeEach(func() {
		validToken = &utils.PgControldataTokenContent{
			DatabaseSystemIdentifier:     "12345",
			LatestCheckpointTimelineID:   "2",
			LatestCheckpointREDOLocation: "0/16D68D0",
		}
	})

	Describe("ValidateAgainstInstanceStatus", func() {
		Context("with valid token", func() {
			It("returns no error", func() {
				err := ValidateAgainstInstanceStatus(validToken, "12345", "2", "0/16D68D0")
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Describe("ValidateAgainstLSN", func() {
		Context("with valid LSN", func() {
			It("returns no error", func() {
				err := ValidateAgainstLSN(validToken, "0/16D68D0")
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("with earlier LSN in the token", func() {
			It("returns permanent failure", func() {
				err := ValidateAgainstLSN(validToken, "0/FFFFFFF")
				Expect(err).To(HaveOccurred())
				Expect(err.(*TokenVerificationError).IsRetryable()).To(BeFalse())
			})
		})
		Context("with later LSN in the token", func() {
			It("returns retryable failure", func() {
				err := ValidateAgainstLSN(validToken, "0/0000000")
				Expect(err).To(HaveOccurred())
				Expect(err.(*TokenVerificationError).IsRetryable()).To(BeTrue())
			})
		})
	})

	Describe("ValidateAgainstTimelineID", func() {
		Context("with valid timeline ID", func() {
			It("returns no error", func() {
				err := ValidateAgainstTimelineID(validToken, "2")
				Expect(err).NotTo(HaveOccurred())
			})
		})
		Context("with earlier timeline ID in the token", func() {
			It("returns permanent failure", func() {
				err := ValidateAgainstTimelineID(validToken, "3")
				Expect(err).To(HaveOccurred())
				Expect(err.(*TokenVerificationError).IsRetryable()).To(BeFalse())
			})
		})
		Context("with later timeline ID in the token", func() {
			It("returns retryable failure", func() {
				err := ValidateAgainstTimelineID(validToken, "1")
				Expect(err).To(HaveOccurred())
				Expect(err.(*TokenVerificationError).IsRetryable()).To(BeTrue())
			})
		})
	})

	Describe("ValidateAgainstSystemIdentifier", func() {
		Context("with valid system identifier", func() {
			It("returns no error", func() {
				err := ValidateAgainstSystemIdentifier(validToken, "12345")
				Expect(err).NotTo(HaveOccurred())
			})
		})
		Context("with invalid system identifier", func() {
			It("returns permanent failure", func() {
				err := ValidateAgainstSystemIdentifier(validToken, "54321")
				Expect(err).To(HaveOccurred())
				Expect(err.(*TokenVerificationError).IsRetryable()).To(BeFalse())
			})
		})
	})
})
