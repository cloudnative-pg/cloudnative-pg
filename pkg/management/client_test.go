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

package management

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("waiting for the certificate status", func() {
	clusterObjectKey := client.ObjectKey{Namespace: "default", Name: "cluster"}

	It("returns immediately once the certificate status is populated", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: clusterObjectKey.Namespace, Name: clusterObjectKey.Name},
			Status: apiv1.ClusterStatus{
				Certificates: apiv1.CertificatesStatus{
					CertificatesConfiguration: apiv1.CertificatesConfiguration{
						ServerTLSSecret: "cluster-server",
					},
				},
			},
		}
		cli := fake.NewClientBuilder().WithScheme(Scheme).WithObjects(cluster).Build()

		Expect(WaitForClusterCertificates(ctx, cli, clusterObjectKey)).To(Succeed())
	})

	It("keeps waiting, and gives up when the context is done, while the status is unset", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: clusterObjectKey.Namespace, Name: clusterObjectKey.Name},
		}
		cli := fake.NewClientBuilder().WithScheme(Scheme).WithObjects(cluster).Build()

		waitCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		Expect(WaitForClusterCertificates(waitCtx, cli, clusterObjectKey)).ToNot(Succeed())
	})
})
