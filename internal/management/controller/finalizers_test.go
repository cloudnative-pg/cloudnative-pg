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

package controller

import (
	"context"
	"errors"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("finalizerReconciler", func() {
	const finalizerName = "test.cnpg.io/finalizer"

	var (
		fakeClient  client.Client
		database    *apiv1.Database
		removeCalls int
		removeErr   error
		reconciler  *finalizerReconciler[*apiv1.Database]
	)

	BeforeEach(func() {
		removeCalls = 0
		removeErr = nil
		database = &apiv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "db-one",
				Namespace: "default",
			},
		}
		fakeClient = fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(database).
			Build()
		reconciler = newFinalizerReconciler(
			fakeClient,
			finalizerName,
			func(_ context.Context, _ *apiv1.Database) error {
				removeCalls++
				return removeErr
			},
		)
	})

	It("adds the finalizer when the object is not being deleted", func(ctx SpecContext) {
		Expect(reconciler.reconcile(ctx, database)).To(Succeed())

		Expect(controllerutil.ContainsFinalizer(database, finalizerName)).To(BeTrue())
		Expect(removeCalls).To(BeZero())
	})

	It("does nothing when the finalizer is already present", func(ctx SpecContext) {
		Expect(controllerutil.AddFinalizer(database, finalizerName)).To(BeTrue())
		Expect(fakeClient.Update(ctx, database)).To(Succeed())

		Expect(reconciler.reconcile(ctx, database)).To(Succeed())

		Expect(controllerutil.ContainsFinalizer(database, finalizerName)).To(BeTrue())
		Expect(removeCalls).To(BeZero())
	})

	It("runs onRemove and removes the finalizer when the object is being deleted", func(ctx SpecContext) {
		Expect(controllerutil.AddFinalizer(database, finalizerName)).To(BeTrue())
		Expect(fakeClient.Update(ctx, database)).To(Succeed())
		Expect(fakeClient.Delete(ctx, database)).To(Succeed())
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(database), database)).To(Succeed())

		Expect(reconciler.reconcile(ctx, database)).To(Succeed())

		Expect(removeCalls).To(Equal(1))
		// With the finalizer gone, the deleted object is garbage collected.
		err := fakeClient.Get(ctx, client.ObjectKeyFromObject(database), database)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("keeps the finalizer when onRemove fails", func(ctx SpecContext) {
		removeErr = errors.New("drop failed")
		Expect(controllerutil.AddFinalizer(database, finalizerName)).To(BeTrue())
		Expect(fakeClient.Update(ctx, database)).To(Succeed())
		Expect(fakeClient.Delete(ctx, database)).To(Succeed())
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(database), database)).To(Succeed())

		Expect(reconciler.reconcile(ctx, database)).To(MatchError(removeErr))

		Expect(removeCalls).To(Equal(1))
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(database), database)).To(Succeed())
		Expect(controllerutil.ContainsFinalizer(database, finalizerName)).To(BeTrue())
	})

	It("does nothing when a deleted object does not carry the finalizer", func(ctx SpecContext) {
		const otherFinalizer = "other.cnpg.io/keep"
		Expect(controllerutil.AddFinalizer(database, otherFinalizer)).To(BeTrue())
		Expect(fakeClient.Update(ctx, database)).To(Succeed())
		Expect(fakeClient.Delete(ctx, database)).To(Succeed())
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(database), database)).To(Succeed())

		Expect(reconciler.reconcile(ctx, database)).To(Succeed())

		Expect(removeCalls).To(BeZero())
		Expect(controllerutil.ContainsFinalizer(database, otherFinalizer)).To(BeTrue())
	})
})
