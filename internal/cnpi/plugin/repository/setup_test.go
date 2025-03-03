package repository

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Set Plugin Protocol", func() {
	var repository *data

	BeforeEach(func() {
		repository = &data{}
	})

	It("creates connection pool for new plugin", func() {
		err := repository.setPluginProtocol("plugin1", newUnitTestProtocol("test"), pluginSetupOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(repository.pluginConnectionPool).To(HaveKey("plugin1"))
	})

	It("fails when adding same plugin name without force", func() {
		err := repository.setPluginProtocol("plugin1", newUnitTestProtocol("/tmp/socket"), pluginSetupOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = repository.setPluginProtocol("plugin1", newUnitTestProtocol("/tmp/socket2"), pluginSetupOptions{})
		Expect(err).To(BeEquivalentTo(&ErrPluginAlreadyRegistered{Name: "plugin1"}))
	})

	It("overwrites existing plugin when force is true", func() {
		first := newUnitTestProtocol("/tmp/socket")
		err := repository.setPluginProtocol("plugin1", first, pluginSetupOptions{})
		Expect(err).NotTo(HaveOccurred())
		pool1 := repository.pluginConnectionPool["plugin1"]

		ctx1, cancel := context.WithCancel(context.Background())
		conn1, err := pool1.Acquire(ctx1)
		Expect(err).NotTo(HaveOccurred())
		Expect(conn1).NotTo(BeNil())
		cancel()
		conn1.Release()

		second := newUnitTestProtocol("/tmp/socket2")
		err = repository.setPluginProtocol("plugin1", second, pluginSetupOptions{force: true})
		Expect(err).NotTo(HaveOccurred())
		pool2 := repository.pluginConnectionPool["plugin1"]

		ctx2, cancel := context.WithCancel(context.Background())
		conn2, err := pool2.Acquire(ctx2)
		Expect(err).NotTo(HaveOccurred())
		Expect(conn2).NotTo(BeNil())
		cancel()
		conn2.Release()

		Expect(pool1).NotTo(Equal(pool2))
		Expect(first.mockHandlers).To(HaveLen(1))
		Expect(first.mockHandlers[0].closed).To(BeTrue())
		Expect(second.mockHandlers).To(HaveLen(1))
		Expect(second.mockHandlers[0].closed).To(BeFalse())
	})
})
