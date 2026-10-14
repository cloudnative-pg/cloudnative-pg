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
	"io"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

var _ = Describe("getReplicaStatusFromPodViaHTTP", func() {
	var pod corev1.Pod

	BeforeEach(func() {
		pod = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
			Status:     corev1.PodStatus{PodIP: "192.0.2.1"},
		}
	})

	It("does not retry an HTTP error and preserves its status code and body", func(ctx SpecContext) {
		const body = "PostgreSQL socket is absent"
		requests := 0
		client := instanceClientImpl{Client: &http.Client{
			Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			}),
		}}

		status := client.getReplicaStatusFromPodViaHTTP(ctx, pod)

		Expect(status.Error).To(HaveOccurred())
		var statusErr *StatusError
		Expect(errors.As(status.Error, &statusErr)).To(BeTrue())
		Expect(statusErr.StatusCode).To(Equal(http.StatusInternalServerError))
		Expect(statusErr.Body).To(Equal(body))
		Expect(requests).To(Equal(1))
	})

	It("retries a transient transport error and decodes the successful response", func(ctx SpecContext) {
		requests := 0
		client := instanceClientImpl{Client: &http.Client{
			Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
				requests++
				if requests == 1 {
					return nil, io.EOF
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"systemID":"12345","isPrimary":true}`)),
				}, nil
			}),
		}}

		status := client.getReplicaStatusFromPodViaHTTP(ctx, pod)

		Expect(requests).To(Equal(2))
		Expect(status.Error).ToNot(HaveOccurred())
		Expect(status.SystemID).To(Equal("12345"))
		Expect(status.IsPrimary).To(BeTrue())
	})
})
