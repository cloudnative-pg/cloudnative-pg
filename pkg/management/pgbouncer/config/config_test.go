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

package config

import (
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BuildConfigurationFiles", func() {
	iniPath := filepath.Join(ConfigsDir, PgBouncerIniFileName)
	userListPath := filepath.Join(ConfigsDir, PgBouncerUserListFileName)

	// newSecrets returns a Secrets value backed by a basic-auth auth query
	// secret. BuildConfigurationFiles dereferences the crypto-material secrets
	// unconditionally, so they are populated with placeholder, non-nil content.
	newSecrets := func() *Secrets {
		return &Secrets{
			AuthQuery: &corev1.Secret{
				Type: corev1.SecretTypeBasicAuth,
				Data: map[string][]byte{
					corev1.BasicAuthUsernameKey: []byte("test-username"),
					corev1.BasicAuthPasswordKey: []byte("test-password"),
				},
			},
			ServerCA:  &corev1.Secret{Data: map[string][]byte{}},
			ClientCA:  &corev1.Secret{Data: map[string][]byte{}},
			ClientTLS: &corev1.Secret{Data: map[string][]byte{}},
		}
	}

	// newPooler returns a minimal Pooler carrying the given generic parameters.
	newPooler := func(parameters map[string]string) *apiv1.Pooler {
		return &apiv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: "pooler-example"},
			Spec: apiv1.PoolerSpec{
				Cluster: apiv1.LocalObjectReference{Name: "cluster-example"},
				Type:    apiv1.PoolerTypeRW,
				PgBouncer: &apiv1.PgBouncerSpec{
					PoolMode:   apiv1.PgBouncerPoolModeSession,
					Parameters: parameters,
				},
			},
		}
	}

	It("derives auth_user from the auth query secret when no override is set", func() {
		files, err := BuildConfigurationFiles(newPooler(nil), newSecrets())
		Expect(err).ToNot(HaveOccurred())

		ini := string(files[iniPath])
		Expect(ini).To(ContainSubstring("auth_user = test-username"))
		Expect(strings.Count(ini, "auth_user")).To(Equal(1))
		Expect(string(files[userListPath])).To(ContainSubstring(`"test-username" "test-password"`))
	})

	It("applies the auth_user override without emitting the key twice", func() {
		files, err := BuildConfigurationFiles(
			newPooler(map[string]string{"auth_user": "pgbouncer"}),
			newSecrets(),
		)
		Expect(err).ToNot(HaveOccurred())

		ini := string(files[iniPath])
		Expect(ini).To(ContainSubstring("auth_user = pgbouncer"))
		// auth_user has a dedicated template line, so it must appear exactly
		// once and never leak through the generic parameters block.
		Expect(strings.Count(ini, "auth_user")).To(Equal(1))
		// The password is still taken from the auth query secret.
		Expect(string(files[userListPath])).To(ContainSubstring(`"pgbouncer" "test-password"`))
	})

	It("ignores an empty auth_user override and keeps the derived user", func() {
		files, err := BuildConfigurationFiles(
			newPooler(map[string]string{"auth_user": ""}),
			newSecrets(),
		)
		Expect(err).ToNot(HaveOccurred())

		ini := string(files[iniPath])
		Expect(ini).To(ContainSubstring("auth_user = test-username"))
		Expect(strings.Count(ini, "auth_user")).To(Equal(1))
		Expect(ini).ToNot(ContainSubstring("auth_user = \n"))
	})
})
