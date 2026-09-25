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

package external

import (
	"context"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ConfigureConnectionToServer", func() {
	const namespace = "test-namespace"

	var base string

	BeforeEach(func() {
		base = GinkgoT().TempDir()
		customExternalSecretsPath = base
		DeferCleanup(func() { customExternalSecretsPath = "" })
	})

	// The exhaustive set of rejected values is covered by the
	// validateExternalClusterPaths unit tests; this spec only verifies that the
	// write entry point actually enforces the guard before touching the disk, so
	// the security boundary cannot be silently lost by dropping the call.
	It("rejects a path-traversal server name before writing anything to disk", func() {
		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "cert-secret", Namespace: namespace},
				Data:       map[string][]byte{"tls.crt": []byte("payload")},
			}).
			Build()

		server := &apiv1.ExternalCluster{
			Name: "../pwned",
			SSLCert: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "cert-secret"},
				Key:                  "tls.crt",
			},
		}

		_, err := ConfigureConnectionToServer(context.Background(), fakeClient, namespace, server)
		Expect(err).To(MatchError(ErrInvalidPathComponent))

		// The guard must fire before any directory or file is created, both
		// inside the secrets directory and as a sibling reachable via traversal.
		entries, readErr := os.ReadDir(base)
		Expect(readErr).ToNot(HaveOccurred())
		Expect(entries).To(BeEmpty(), "the external secrets directory must stay empty")
		Expect(filepath.Join(filepath.Dir(base), "pwned")).ToNot(BeAnExistingFile())
	})
})
