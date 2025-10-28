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
	"bytes"
	"fmt"
	"io"
	"os"

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

var _ = Describe("logWarning", func() {
	It("should format warning message correctly", func() {
		// Capture stdout
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		testErr := fmt.Errorf("test error")
		logWarning("test operation", testErr, "Additional context here")

		// Restore stdout and read output
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, err := io.Copy(&buf, r)
		Expect(err).ToNot(HaveOccurred())

		output := buf.String()
		Expect(output).To(ContainSubstring("WARNING: test operation: test error"))
		Expect(output).To(ContainSubstring("         Additional context here"))
	})

	It("should handle permission error messages", func() {
		// Capture stdout
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		testErr := fmt.Errorf("permission denied")
		logWarning("could not get webhooks", testErr,
			"Continuing without webhook information. This is expected if you don't have cluster-level permissions.")

		// Restore stdout and read output
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, err := io.Copy(&buf, r)
		Expect(err).ToNot(HaveOccurred())

		output := buf.String()
		Expect(output).To(ContainSubstring("WARNING: could not get webhooks: permission denied"))
		Expect(output).To(ContainSubstring("         Continuing without webhook information"))
		Expect(output).To(ContainSubstring("cluster-level permissions"))
	})
})
