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
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("executeRequestWithError", func() {
	newServer := func(statusCode int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, body, statusCode)
		}))
	}

	It("surfaces a 503 instance manager rejection as a transient StatusError", func(ctx SpecContext) {
		srv := newServer(http.StatusServiceUnavailable, "operator certificate not recognized")
		defer srv.Close()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		Expect(err).ToNot(HaveOccurred())

		_, err = executeRequestWithError[webserver.BackupResultData](ctx, srv.Client(), req, true)
		Expect(err).To(HaveOccurred())

		var statusErr *StatusError
		Expect(errors.As(err, &statusErr)).To(BeTrue())
		Expect(statusErr.StatusCode).To(Equal(http.StatusServiceUnavailable))
		Expect(IsTransientAuthError(err)).To(BeTrue())
	})

	It("surfaces a 401 no-client-certificate rejection as a non-transient StatusError", func(ctx SpecContext) {
		srv := newServer(http.StatusUnauthorized, "client certificate required")
		defer srv.Close()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		Expect(err).ToNot(HaveOccurred())

		_, err = executeRequestWithError[webserver.BackupResultData](ctx, srv.Client(), req, true)
		Expect(err).To(HaveOccurred())

		var statusErr *StatusError
		Expect(errors.As(err, &statusErr)).To(BeTrue())
		Expect(statusErr.StatusCode).To(Equal(http.StatusUnauthorized))
		Expect(IsTransientAuthError(err)).To(BeFalse())
	})

	It("does not wrap a successful response in a StatusError", func(ctx SpecContext) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		Expect(err).ToNot(HaveOccurred())

		result, err := executeRequestWithError[webserver.BackupResultData](ctx, srv.Client(), req, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())
	})
})
