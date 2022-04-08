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

package specs

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Secret creation", func() {
	It("create a secret with the right user and password", func() {
		secret := CreateSecret("name", "namespace",
			"*", "thisdb", "thisuser", "thispassword")
		Expect(secret.Name).To(Equal("name"))
		Expect(secret.Namespace).To(Equal("namespace"))
		Expect(secret.StringData["username"]).To(Equal("thisuser"))
		Expect(secret.StringData["password"]).To(Equal("thispassword"))
	})
})
