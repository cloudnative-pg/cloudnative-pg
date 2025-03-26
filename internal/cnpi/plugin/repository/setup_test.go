/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

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

	It("fails when adding same plugin name without forceRegistration", func() {
		err := repository.setPluginProtocol("plugin1", newUnitTestProtocol("/tmp/socket"), pluginSetupOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = repository.setPluginProtocol("plugin1", newUnitTestProtocol("/tmp/socket2"), pluginSetupOptions{})
		Expect(err).To(BeEquivalentTo(&ErrPluginAlreadyRegistered{Name: "plugin1"}))
	})

	It("overwrites existing plugin when forceRegistration is true", func() {
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
		err = repository.setPluginProtocol("plugin1", second, pluginSetupOptions{forceRegistration: true})
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
