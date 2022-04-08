/*
Copyright 2019-2022 The CloudNativePG Contributors

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

package utils

import (
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("basic-auth secrets decoding", func() {
	It("is able to extract username and password", func() {
		secret := corev1.Secret{
			Data: map[string][]byte{
				corev1.BasicAuthUsernameKey: []byte("user-name-here"),
				corev1.BasicAuthPasswordKey: []byte("this-password"),
			},
		}

		user, pwd, err := GetUserPasswordFromSecret(&secret)
		Expect(err).ToNot(HaveOccurred())
		Expect(user).To(Equal("user-name-here"))
		Expect(pwd).To(Equal("this-password"))
	})

	It("will raise an error if username is missing", func() {
		secret := corev1.Secret{
			Data: map[string][]byte{
				corev1.BasicAuthPasswordKey: []byte("this-password"),
			},
		}

		_, _, err := GetUserPasswordFromSecret(&secret)
		Expect(err).To(HaveOccurred())
	})

	It("will raise an error if password is missing", func() {
		secret := corev1.Secret{
			Data: map[string][]byte{
				corev1.BasicAuthUsernameKey: []byte("user-name-here"),
			},
		}

		_, _, err := GetUserPasswordFromSecret(&secret)
		Expect(err).To(HaveOccurred())
	})
})
