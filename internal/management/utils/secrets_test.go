/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
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
