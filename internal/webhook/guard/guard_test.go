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

package guard

import (
	"context"
	"errors"
	"maps"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// mockAdmittableObject is a mock implementation of AdmittableObject for testing
type mockAdmittableObject struct {
	metav1.ObjectMeta
	metav1.TypeMeta
	AdmissionError string
	Data           map[string]string
}

func (m *mockAdmittableObject) DeepCopyObject() runtime.Object {
	return &mockAdmittableObject{
		ObjectMeta:     *m.DeepCopy(),
		TypeMeta:       m.TypeMeta,
		AdmissionError: m.AdmissionError,
		Data:           maps.Clone(m.Data),
	}
}

func (m *mockAdmittableObject) SetAdmissionError(msg string) {
	m.AdmissionError = msg
}

func (m *mockAdmittableObject) GetAdmissionError() string {
	return m.AdmissionError
}

// mockDefaulter is a mock implementation of admission.Defaulter for testing
type mockDefaulter struct {
	DefaultFunc func(ctx context.Context, obj *mockAdmittableObject) error
}

func (m *mockDefaulter) Default(ctx context.Context, obj *mockAdmittableObject) error {
	if m.DefaultFunc != nil {
		return m.DefaultFunc(ctx, obj)
	}
	return nil
}

// mockValidator is a mock implementation of admission.Validator for testing.
// Only ValidateCreate is exercised by the guard; ValidateUpdate and ValidateDelete
// exist solely to satisfy the interface.
type mockValidator struct {
	ValidateCreateFunc func(ctx context.Context, obj *mockAdmittableObject) (admission.Warnings, error)
}

func (m *mockValidator) ValidateCreate(
	ctx context.Context, obj *mockAdmittableObject,
) (admission.Warnings, error) {
	if m.ValidateCreateFunc != nil {
		return m.ValidateCreateFunc(ctx, obj)
	}
	return nil, nil
}

func (m *mockValidator) ValidateUpdate(
	_ context.Context, _, _ *mockAdmittableObject,
) (admission.Warnings, error) {
	return nil, nil
}

func (m *mockValidator) ValidateDelete(
	_ context.Context, _ *mockAdmittableObject,
) (admission.Warnings, error) {
	return nil, nil
}

// mockClient is a mock implementation of client.Client for testing
type mockClient struct {
	client.Client
	UpdateFunc       func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error
	StatusUpdateFunc func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error
}

func (m *mockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, obj, opts...)
	}
	return nil
}

func (m *mockClient) Status() client.SubResourceWriter {
	return &mockStatusWriter{
		UpdateFunc: m.StatusUpdateFunc,
	}
}

type mockStatusWriter struct {
	UpdateFunc func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error
}

func (m *mockStatusWriter) Update(
	ctx context.Context,
	obj client.Object,
	opts ...client.SubResourceUpdateOption,
) error {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, obj, opts...)
	}
	return nil
}

func (m *mockStatusWriter) Patch(
	_ context.Context,
	_ client.Object,
	_ client.Patch,
	_ ...client.SubResourcePatchOption,
) error {
	return nil
}

func (m *mockStatusWriter) Create(
	_ context.Context,
	_ client.Object,
	_ client.Object,
	_ ...client.SubResourceCreateOption,
) error {
	return nil
}

func (m *mockStatusWriter) Apply(
	_ context.Context,
	_ runtime.ApplyConfiguration,
	_ ...client.SubResourceApplyOption,
) error {
	return nil
}

var _ = Describe("EnsureResourceIsAdmitted", func() {
	var (
		ctx    context.Context
		obj    *mockAdmittableObject
		mockCl *mockClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		obj = &mockAdmittableObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-object",
				Namespace: "default",
			},
			Data: map[string]string{},
		}
		mockCl = &mockClient{}
	})

	It("is a no-op when the guard is nil", func() {
		var nilAdmission *Admission[*mockAdmittableObject]

		result, err := nilAdmission.EnsureResourceIsAdmitted(ctx, AdmissionParams[*mockAdmittableObject]{
			Object: obj,
			Client: mockCl,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.IsZero()).To(BeTrue())
	})

	It("is a no-op when neither defaulter nor validator are set", func() {
		guard := &Admission[*mockAdmittableObject]{}

		result, err := guard.EnsureResourceIsAdmitted(ctx, AdmissionParams[*mockAdmittableObject]{
			Object: obj,
			Client: mockCl,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.IsZero()).To(BeTrue())
	})

	It("succeeds and clears the admission error when defaulting and validation pass", func() {
		obj.AdmissionError = "previous error"

		guard := &Admission[*mockAdmittableObject]{
			Defaulter: &mockDefaulter{},
			Validator: &mockValidator{},
		}

		result, err := guard.EnsureResourceIsAdmitted(ctx, AdmissionParams[*mockAdmittableObject]{
			Object: obj,
			Client: mockCl,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.IsZero()).To(BeTrue())
		Expect(obj.AdmissionError).To(BeEmpty())
	})

	It("persists the cleared admission error when validation passes and ApplyChanges is true", func() {
		obj.AdmissionError = "previous error"

		statusUpdated := false
		mockCl.StatusUpdateFunc = func(
			_ context.Context, statusObj client.Object, _ ...client.SubResourceUpdateOption,
		) error {
			statusUpdated = true
			Expect(statusObj.(*mockAdmittableObject).AdmissionError).To(BeEmpty())
			return nil
		}

		guard := &Admission[*mockAdmittableObject]{
			Defaulter: &mockDefaulter{},
			Validator: &mockValidator{},
		}

		result, err := guard.EnsureResourceIsAdmitted(ctx, AdmissionParams[*mockAdmittableObject]{
			Object:       obj,
			Client:       mockCl,
			ApplyChanges: true,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.IsZero()).To(BeTrue())
		Expect(obj.AdmissionError).To(BeEmpty())
		Expect(statusUpdated).To(BeTrue())
	})

	It("does not write the status when validation passes and there was no error", func() {
		statusUpdated := false
		mockCl.StatusUpdateFunc = func(
			_ context.Context, _ client.Object, _ ...client.SubResourceUpdateOption,
		) error {
			statusUpdated = true
			return nil
		}

		guard := &Admission[*mockAdmittableObject]{
			Defaulter: &mockDefaulter{},
			Validator: &mockValidator{},
		}

		result, err := guard.EnsureResourceIsAdmitted(ctx, AdmissionParams[*mockAdmittableObject]{
			Object:       obj,
			Client:       mockCl,
			ApplyChanges: true,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.IsZero()).To(BeTrue())
		Expect(statusUpdated).To(BeFalse())
	})

	It("updates the object and requeues when defaulting mutates it and ApplyChanges is true", func() {
		updateCalled := false
		mockCl.UpdateFunc = func(_ context.Context, _ client.Object, _ ...client.UpdateOption) error {
			updateCalled = true
			return nil
		}

		guard := &Admission[*mockAdmittableObject]{
			Defaulter: &mockDefaulter{
				DefaultFunc: func(_ context.Context, obj *mockAdmittableObject) error {
					obj.Data["defaulted"] = "true"
					return nil
				},
			},
		}

		result, err := guard.EnsureResourceIsAdmitted(ctx, AdmissionParams[*mockAdmittableObject]{
			Object:       obj,
			Client:       mockCl,
			ApplyChanges: true,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(1 * time.Second))
		Expect(updateCalled).To(BeTrue())
	})

	It("requeues without updating when defaulting mutates the object and ApplyChanges is false", func() {
		updateCalled := false
		mockCl.UpdateFunc = func(_ context.Context, _ client.Object, _ ...client.UpdateOption) error {
			updateCalled = true
			return nil
		}

		guard := &Admission[*mockAdmittableObject]{
			Defaulter: &mockDefaulter{
				DefaultFunc: func(_ context.Context, obj *mockAdmittableObject) error {
					obj.Data["defaulted"] = "true"
					return nil
				},
			},
		}

		result, err := guard.EnsureResourceIsAdmitted(ctx, AdmissionParams[*mockAdmittableObject]{
			Object:       obj,
			Client:       mockCl,
			ApplyChanges: false,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(5 * time.Second))
		Expect(updateCalled).To(BeFalse())
	})

	It("returns the defaulter's error and skips validation", func() {
		defaultingError := errors.New("defaulting failed")
		validationCalled := false

		guard := &Admission[*mockAdmittableObject]{
			Defaulter: &mockDefaulter{
				DefaultFunc: func(_ context.Context, _ *mockAdmittableObject) error {
					return defaultingError
				},
			},
			Validator: &mockValidator{
				ValidateCreateFunc: func(_ context.Context, _ *mockAdmittableObject) (admission.Warnings, error) {
					validationCalled = true
					return nil, nil
				},
			},
		}

		result, err := guard.EnsureResourceIsAdmitted(ctx, AdmissionParams[*mockAdmittableObject]{
			Object: obj,
			Client: mockCl,
		})
		Expect(err).To(Equal(defaultingError))
		Expect(result.IsZero()).To(BeTrue())
		Expect(validationCalled).To(BeFalse())
	})

	It("propagates an error from the client update", func() {
		updateError := errors.New("update failed")
		mockCl.UpdateFunc = func(_ context.Context, _ client.Object, _ ...client.UpdateOption) error {
			return updateError
		}

		guard := &Admission[*mockAdmittableObject]{
			Defaulter: &mockDefaulter{
				DefaultFunc: func(_ context.Context, obj *mockAdmittableObject) error {
					obj.Data["defaulted"] = "true"
					return nil
				},
			},
		}

		result, err := guard.EnsureResourceIsAdmitted(ctx, AdmissionParams[*mockAdmittableObject]{
			Object:       obj,
			Client:       mockCl,
			ApplyChanges: true,
		})
		Expect(err).To(Equal(updateError))
		Expect(result.IsZero()).To(BeTrue())
	})

	It("returns a terminal error without touching status when validation fails and ApplyChanges is false", func() {
		validationError := errors.New("validation failed")
		statusUpdateCalled := false
		mockCl.StatusUpdateFunc = func(
			_ context.Context, _ client.Object, _ ...client.SubResourceUpdateOption,
		) error {
			statusUpdateCalled = true
			return nil
		}

		guard := &Admission[*mockAdmittableObject]{
			Validator: &mockValidator{
				ValidateCreateFunc: func(_ context.Context, _ *mockAdmittableObject) (admission.Warnings, error) {
					return nil, validationError
				},
			},
		}

		result, err := guard.EnsureResourceIsAdmitted(ctx, AdmissionParams[*mockAdmittableObject]{
			Object:       obj,
			Client:       mockCl,
			ApplyChanges: false,
		})
		Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())
		Expect(result.IsZero()).To(BeTrue())
		Expect(statusUpdateCalled).To(BeFalse())
	})

	It("records a sanitized admission error on the status when validation fails and ApplyChanges is true", func() {
		validationError := errors.New("validation failed")
		statusUpdateCalled := false
		mockCl.StatusUpdateFunc = func(
			_ context.Context, statusObj client.Object, _ ...client.SubResourceUpdateOption,
		) error {
			statusUpdateCalled = true
			// The raw validation error is never written to the world-readable status
			Expect(statusObj.(*mockAdmittableObject).AdmissionError).ToNot(BeEmpty())
			Expect(statusObj.(*mockAdmittableObject).AdmissionError).ToNot(ContainSubstring("validation failed"))
			return nil
		}

		guard := &Admission[*mockAdmittableObject]{
			Validator: &mockValidator{
				ValidateCreateFunc: func(_ context.Context, _ *mockAdmittableObject) (admission.Warnings, error) {
					return nil, validationError
				},
			},
		}

		result, err := guard.EnsureResourceIsAdmitted(ctx, AdmissionParams[*mockAdmittableObject]{
			Object:       obj,
			Client:       mockCl,
			ApplyChanges: true,
		})
		Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())
		Expect(result.IsZero()).To(BeTrue())
		Expect(statusUpdateCalled).To(BeTrue())
	})

	It("persists the offending field paths but not their values when validation fails", func() {
		const sensitive = "s3://secret-bucket/backups"
		validationError := apierrors.NewInvalid(
			schema.GroupKind{Group: "postgresql.cnpg.io", Kind: "Cluster"},
			"cluster-example",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec", "backup", "barmanObjectStore"),
					sensitive,
					"missing credentials"),
			},
		)
		var recorded string
		mockCl.StatusUpdateFunc = func(
			_ context.Context, statusObj client.Object, _ ...client.SubResourceUpdateOption,
		) error {
			recorded = statusObj.(*mockAdmittableObject).AdmissionError
			return nil
		}

		guard := &Admission[*mockAdmittableObject]{
			Validator: &mockValidator{
				ValidateCreateFunc: func(_ context.Context, _ *mockAdmittableObject) (admission.Warnings, error) {
					return nil, validationError
				},
			},
		}

		_, err := guard.EnsureResourceIsAdmitted(ctx, AdmissionParams[*mockAdmittableObject]{
			Object:       obj,
			Client:       mockCl,
			ApplyChanges: true,
		})
		Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())
		Expect(recorded).To(ContainSubstring("spec.backup.barmanObjectStore"))
		Expect(recorded).ToNot(ContainSubstring(sensitive))
	})

	It("propagates an error from the status update", func() {
		validationError := errors.New("validation failed")
		updateError := errors.New("status update failed")
		mockCl.StatusUpdateFunc = func(
			_ context.Context, _ client.Object, _ ...client.SubResourceUpdateOption,
		) error {
			return updateError
		}

		guard := &Admission[*mockAdmittableObject]{
			Validator: &mockValidator{
				ValidateCreateFunc: func(_ context.Context, _ *mockAdmittableObject) (admission.Warnings, error) {
					return nil, validationError
				},
			},
		}

		result, err := guard.EnsureResourceIsAdmitted(ctx, AdmissionParams[*mockAdmittableObject]{
			Object:       obj,
			Client:       mockCl,
			ApplyChanges: true,
		})
		Expect(err).To(Equal(updateError))
		Expect(result.IsZero()).To(BeTrue())
	})
})
