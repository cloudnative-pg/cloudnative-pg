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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pooler type tests", func() {
	It("pgbouncer pools are not paused by default", func() {
		pgbouncer := PgBouncerSpec{}
		Expect(pgbouncer.IsPaused()).To(BeFalse())
	})

	It("pgbouncer pools can be paused", func() {
		trueVal := true
		pgbouncer := PgBouncerSpec{
			Paused: &trueVal,
		}
		Expect(pgbouncer.IsPaused()).To(BeTrue())
	})
})

var _ = Describe("Pooler GetServiceAccountName", func() {
	It("returns pooler name when serviceAccountName is not specified", func() {
		pooler := &Pooler{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-pooler",
			},
			Spec: PoolerSpec{},
		}
		Expect(pooler.GetServiceAccountName()).To(Equal("my-pooler"))
	})

	It("returns custom serviceAccountName when specified", func() {
		pooler := &Pooler{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-pooler",
			},
			Spec: PoolerSpec{
				ServiceAccountName: ptr.To("shared-service-account"),
			},
		}
		Expect(pooler.GetServiceAccountName()).To(Equal("shared-service-account"))
	})

	It("returns pooler name when serviceAccountName is nil", func() {
		pooler := &Pooler{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-pooler",
			},
			Spec: PoolerSpec{
				ServiceAccountName: nil,
			},
		}
		Expect(pooler.GetServiceAccountName()).To(Equal("my-pooler"))
	})
})
