package webserver

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("createSetPgStatusArchiveRequestBody", func() {
	It("returns a valid io.Reader and no errors when errMessage is empty", func() {
		client := &localClient{}
		body, err := client.createSetPgStatusArchiveRequestBody("")
		Expect(err).ToNot(HaveOccurred())
		Expect(body).NotTo(BeNil())
	})

	It("returns a valid io.Reader and no errors when errMessage is non-empty", func() {
		client := &localClient{}
		body, err := client.createSetPgStatusArchiveRequestBody("some error")
		Expect(err).ToNot(HaveOccurred())
		Expect(body).NotTo(BeNil())
	})
})
