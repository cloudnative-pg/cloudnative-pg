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
	"errors"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("waiting for the certificate status", func() {
	const serverTLSSecret = "cluster-server"

	clusterObjectKey := client.ObjectKey{Namespace: "default", Name: "cluster"}

	newCluster := func() *apiv1.Cluster {
		return &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: clusterObjectKey.Namespace, Name: clusterObjectKey.Name},
		}
	}

	withCertificates := func(cluster *apiv1.Cluster) *apiv1.Cluster {
		cluster.Status.Certificates.ServerTLSSecret = serverTLSSecret
		return cluster
	}

	// Bound the wait: specs have no timeout of their own, so a broken
	// implementation would hang the whole suite instead of just failing.
	boundedContext := func(ctx context.Context) context.Context {
		waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		DeferCleanup(cancel)
		return waitCtx
	}

	It("returns the Cluster it found the certificates in, without polling", func(ctx SpecContext) {
		cli := fake.NewClientBuilder().WithScheme(Scheme).WithObjects(withCertificates(newCluster())).Build()

		start := time.Now()
		cluster, err := WaitForClusterCertificates(boundedContext(ctx), cli, clusterObjectKey)
		Expect(err).ToNot(HaveOccurred())
		Expect(time.Since(start)).To(BeNumerically("<", time.Second))
		Expect(cluster.Name).To(Equal(clusterObjectKey.Name))
		Expect(cluster.Status.Certificates.ServerTLSSecret).To(Equal(serverTLSSecret))
	})

	It("keeps reading until the certificate status appears", func(ctx SpecContext) {
		var reads int
		cli := fake.NewClientBuilder().
			WithScheme(Scheme).
			WithObjects(newCluster()).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey,
					obj client.Object, opts ...client.GetOption,
				) error {
					if err := cl.Get(ctx, key, obj, opts...); err != nil {
						return err
					}
					// Simulate the status showing up after the first read.
					reads++
					if reads > 1 {
						withCertificates(obj.(*apiv1.Cluster))
					}
					return nil
				},
			}).
			Build()

		cluster, err := WaitForClusterCertificates(boundedContext(ctx), cli, clusterObjectKey)
		Expect(err).ToNot(HaveOccurred())
		Expect(reads).To(BeNumerically(">", 1))
		Expect(cluster.Status.Certificates.ServerTLSSecret).To(Equal(serverTLSSecret))
	})

	It("retries a failed read instead of giving up on it", func(ctx SpecContext) {
		var reads int
		cli := fake.NewClientBuilder().
			WithScheme(Scheme).
			WithObjects(withCertificates(newCluster())).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey,
					obj client.Object, opts ...client.GetOption,
				) error {
					reads++
					if reads == 1 {
						return errors.New("the API server is not reachable")
					}
					return cl.Get(ctx, key, obj, opts...)
				},
			}).
			Build()

		cluster, err := WaitForClusterCertificates(boundedContext(ctx), cli, clusterObjectKey)
		Expect(err).ToNot(HaveOccurred())
		Expect(reads).To(BeNumerically(">", 1))
		Expect(cluster.Status.Certificates.ServerTLSSecret).To(Equal(serverTLSSecret))
	})

	It("gives up when the context is done, while the status is unset", func(ctx SpecContext) {
		cli := fake.NewClientBuilder().WithScheme(Scheme).WithObjects(newCluster()).Build()

		waitCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		cluster, err := WaitForClusterCertificates(waitCtx, cli, clusterObjectKey)
		Expect(err).To(HaveOccurred())
		Expect(cluster).To(BeNil())
	})
})
