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
	"net/http"
	"net/http/httptest"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/cache"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("serveCache", func() {
	var ws localWebserverEndpoints

	get := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, url.PathCache+key, nil)
		w := httptest.NewRecorder()
		ws.serveCache(w, req)
		return w
	}

	AfterEach(func() {
		cache.Delete(cache.WALRestoreConfigKey)
	})

	It("returns not-found when no recovery source store is cached", func() {
		w := get(cache.WALRestoreConfigKey)
		Expect(w.Code).To(Equal(http.StatusNotFound))
	})

	It("serves the cached recovery source store as JSON the cache client can decode", func() {
		stored := &apiv1.BarmanObjectStoreConfiguration{
			BarmanCredentials: apiv1.BarmanCredentials{
				AWS: &apiv1.S3Credentials{
					AccessKeyIDReference: &apiv1.SecretKeySelector{
						LocalObjectReference: apiv1.LocalObjectReference{Name: "source-creds"},
						Key:                  "ACCESS_KEY_ID",
					},
					SecretAccessKeyReference: &apiv1.SecretKeySelector{
						LocalObjectReference: apiv1.LocalObjectReference{Name: "source-creds"},
						Key:                  "SECRET_ACCESS_KEY",
					},
				},
			},
			EndpointCA: &apiv1.SecretKeySelector{
				LocalObjectReference: apiv1.LocalObjectReference{Name: "source-ca"},
				Key:                  "ca.crt",
			},
			EndpointURL:     "https://source-endpoint:9000",
			DestinationPath: "s3://source/path",
			ServerName:      "source-server",
			Wal: &apiv1.WalBackupConfiguration{
				MaxParallel:                  4,
				RestoreAdditionalCommandArgs: []string{"--read-timeout=60"},
			},
		}
		cache.Store(cache.WALRestoreConfigKey, stored)

		w := get(cache.WALRestoreConfigKey)
		Expect(w.Code).To(Equal(http.StatusOK))

		// Decode exactly like the local cache client does, so this covers the
		// serialization contract between the Job webserver and wal-restore.
		decoded := &apiv1.BarmanObjectStoreConfiguration{}
		Expect(json.Unmarshal(w.Body.Bytes(), decoded)).To(Succeed())
		Expect(decoded).To(Equal(stored))
	})
})
