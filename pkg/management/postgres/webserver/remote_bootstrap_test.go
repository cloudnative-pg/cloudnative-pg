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

package webserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("status endpoints during an in-process bootstrap", func() {
	Describe("isServerStartedUp", func() {
		It("skips the startup probe while a bootstrap is in progress", func() {
			instance := &postgres.Instance{}
			instance.StartBootstrap("initdb")
			ws := remoteWebserverEndpoints{instance: instance}

			req := httptest.NewRequest(http.MethodGet, "/startupz", nil)
			w := httptest.NewRecorder()

			ws.isServerStartedUp(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Body.String()).To(Equal("Skipped"))
		})
	})

	Describe("pgStatus", func() {
		It("returns a 503 with a recognizable body while bootstrapping", func() {
			instance := &postgres.Instance{}
			instance.StartBootstrap("restore")
			ws := remoteWebserverEndpoints{instance: instance}

			req := httptest.NewRequest(http.MethodGet, "/pg/status", nil)
			w := httptest.NewRecorder()

			ws.pgStatus(w, req)

			Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
			Expect(w.Header().Get("Content-Type")).To(Equal("application/json"))

			var body struct {
				Error string `json:"error"`
				Mode  string `json:"mode"`
				Since string `json:"since"`
			}
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body.Error).To(Equal("bootstrapping"))
			Expect(body.Mode).To(Equal("restore"))
			Expect(body.Since).ToNot(BeEmpty())
		})
	})

	Describe("updateInstanceManager", func() {
		It("rejects the update with a 409 while bootstrapping", func() {
			instance := &postgres.Instance{}
			instance.StartBootstrap("initdb")
			ws := remoteWebserverEndpoints{instance: instance}

			handler := ws.updateInstanceManager(nil, nil)
			req := httptest.NewRequest(http.MethodPut, "/update", http.NoBody)
			w := httptest.NewRecorder()

			handler(w, req)

			Expect(w.Code).To(Equal(http.StatusConflict))
		})

		It("does not take the bootstrap branch once it is cleared", func() {
			// With the flag cleared the request continues past the bootstrap
			// guard; a non-PUT request then falls into the usual 405 path,
			// which proves the guard was skipped without touching the binary.
			instance := &postgres.Instance{}
			ws := remoteWebserverEndpoints{instance: instance}

			handler := ws.updateInstanceManager(nil, nil)
			req := httptest.NewRequest(http.MethodGet, "/update", http.NoBody)
			w := httptest.NewRecorder()

			handler(w, req)

			Expect(w.Code).To(Equal(http.StatusMethodNotAllowed))
			body, _ := io.ReadAll(w.Body)
			Expect(string(body)).ToNot(ContainSubstring("bootstrapping"))
		})
	})
})
