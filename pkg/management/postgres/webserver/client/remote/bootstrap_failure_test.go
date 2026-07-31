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

package remote

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BootstrapFailure", func() {
	statusWithBody := func(code int, body any) postgres.PostgresqlStatus {
		encoded, err := json.Marshal(body)
		Expect(err).ToNot(HaveOccurred())
		return postgres.PostgresqlStatus{
			Error: &StatusError{StatusCode: code, Body: string(encoded)},
		}
	}

	It("reports a failed bootstrap with its reason", func() {
		status := statusWithBody(http.StatusServiceUnavailable, postgres.BootstrapStatusResponse{
			Error:  postgres.BootstrapStatusFailed,
			Mode:   "join",
			Reason: "primary unreachable",
		})

		reason, failed := BootstrapFailure(status)
		Expect(failed).To(BeTrue())
		Expect(reason).To(Equal("primary unreachable"))
	})

	It("does not report a still-running bootstrap as failed", func() {
		status := statusWithBody(http.StatusServiceUnavailable, postgres.BootstrapStatusResponse{
			Error: postgres.BootstrapStatusRunning,
			Mode:  "join",
		})

		reason, failed := BootstrapFailure(status)
		Expect(failed).To(BeFalse())
		Expect(reason).To(BeEmpty())
	})

	It("ignores a healthy status with no error", func() {
		reason, failed := BootstrapFailure(postgres.PostgresqlStatus{})
		Expect(failed).To(BeFalse())
		Expect(reason).To(BeEmpty())
	})

	It("ignores a non-503 status error", func() {
		status := postgres.PostgresqlStatus{
			Error: &StatusError{StatusCode: http.StatusInternalServerError, Body: "boom"},
		}
		_, failed := BootstrapFailure(status)
		Expect(failed).To(BeFalse())
	})

	It("ignores an error that is not a StatusError", func() {
		status := postgres.PostgresqlStatus{Error: errors.New("connection refused")}
		_, failed := BootstrapFailure(status)
		Expect(failed).To(BeFalse())
	})

	It("ignores a 503 whose body is not a bootstrap status", func() {
		status := postgres.PostgresqlStatus{
			Error: &StatusError{StatusCode: http.StatusServiceUnavailable, Body: "not json"},
		}
		_, failed := BootstrapFailure(status)
		Expect(failed).To(BeFalse())
	})
})
