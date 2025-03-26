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

package persistentvolumeclaim

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("EnsureInstancePVCGroupIsDeleted", func() {
	var (
		fakeClient   client.Client
		ctx          context.Context
		cancel       context.CancelFunc
		cluster      *apiv1.Cluster
		instanceName string
		namespace    string
	)

	BeforeEach(func() {
		instanceName = "test-instance"
		namespace = "default"

		fakeClient = fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).Build()
		ctx, cancel = context.WithCancel(context.Background())

		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: namespace,
			},
		}
	})

	AfterEach(func() {
		cancel()
	})

	It("should delete all PVCS without error", func() {
		// create PVCs for the instance
		for _, pvcName := range getExpectedPVCsFromCluster(cluster, instanceName) {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvcName.name,
					Namespace: namespace,
				},
			}
			err := fakeClient.Create(ctx, pvc)
			Expect(err).NotTo(HaveOccurred())
		}

		err := EnsureInstancePVCGroupIsDeleted(ctx, fakeClient, cluster, instanceName, namespace)
		Expect(err).NotTo(HaveOccurred())

		// check if all PVCs were deleted
		for _, pvcName := range getExpectedPVCsFromCluster(cluster, instanceName) {
			pvc := &corev1.PersistentVolumeClaim{}
			err := fakeClient.Get(ctx, types.NamespacedName{Name: pvcName.name, Namespace: namespace}, pvc)
			Expect(apierrs.IsNotFound(err)).To(BeTrue())
		}
	})
})
