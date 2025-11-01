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

package report

import (
	"context"
	"io"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("configureRedactors", func() {
	It("should return redact functions when stopRedaction is false", func() {
		secretRedactor, configMapRedactor := configureRedactors(false)

		// Verify they are actual redactors by testing behavior
		secret := corev1.Secret{Data: map[string][]byte{"key": []byte("value")}}
		redacted := secretRedactor(secret)
		Expect(redacted.Data["key"]).To(Equal([]byte("")))

		configMap := corev1.ConfigMap{Data: map[string]string{"key": "value"}}
		redactedCM := configMapRedactor(configMap)
		Expect(redactedCM.Data["key"]).To(Equal(""))
	})

	It("should return pass-through functions when stopRedaction is true", func() {
		// Capture stdout to avoid polluting test output
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		secretRedactor, configMapRedactor := configureRedactors(true)

		// Restore stdout
		_ = w.Close()
		os.Stdout = oldStdout
		_, _ = io.Copy(io.Discard, r)

		// Verify they are pass-through functions
		secret := corev1.Secret{Data: map[string][]byte{"key": []byte("value")}}
		passed := secretRedactor(secret)
		Expect(passed.Data["key"]).To(Equal([]byte("value")))

		configMap := corev1.ConfigMap{Data: map[string]string{"key": "value"}}
		passedCM := configMapRedactor(configMap)
		Expect(passedCM.Data["key"]).To(Equal("value"))
	})
})

var _ = Describe("tryCollectConfigurations", func() {
	It("should handle errors gracefully", func() {
		// This is a demonstration test - in production these would use mocked clients
		// The function is designed to handle errors without panicking
		deployment := appsv1.Deployment{}

		// Capture warnings to avoid polluting test output
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		secrets, configs := tryCollectConfigurations(
			context.Background(),
			deployment,
			redactSecret,
			redactConfigMap,
		)

		// Restore stdout
		_ = w.Close()
		os.Stdout = oldStdout
		_, _ = io.Copy(io.Discard, r)

		// When errors occur, function returns nil slices (not empty slices)
		// This is acceptable as writeToZip checks for nil/empty
		_ = secrets
		_ = configs
	})
})

var _ = Describe("assembleSections", func() {
	It("should create sections array with at least the main report", func() {
		rep := operatorReport{
			deployment: appsv1.Deployment{},
		}

		sections := assembleSections(
			context.Background(),
			rep,
			nil,
			"yaml",
			false,
			false,
		)

		// Should always have at least the main report section
		Expect(sections).ToNot(BeEmpty())
	})

	It("should add logs section when includeLogs is true", func() {
		rep := operatorReport{
			deployment: appsv1.Deployment{},
		}
		pods := []corev1.Pod{{}}

		sections := assembleSections(
			context.Background(),
			rep,
			pods,
			"yaml",
			true, // includeLogs
			false,
		)

		// Should have main report + logs section
		Expect(len(sections)).To(BeNumerically(">=", 2))
	})
})
