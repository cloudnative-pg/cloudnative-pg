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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
		ObjectMeta:     *m.ObjectMeta.DeepCopy(),
		TypeMeta:       m.TypeMeta,
		AdmissionError: m.AdmissionError,
		Data:           maps.Clone(m.Data),
	}
}

func (m *mockAdmittableObject) SetAdmissionError(msg string) {
	m.AdmissionError = msg
}

// mockDefaulter is a mock implementation of webhook.CustomDefaulter for testing
type mockDefaulter struct {
	DefaultFunc func(ctx context.Context, obj runtime.Object) error
}

func (m *mockDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	if m.DefaultFunc != nil {
		return m.DefaultFunc(ctx, obj)
	}
	return nil
}

// mockValidator is a mock implementation of webhook.CustomValidator for testing
type mockValidator struct {
	ValidateCreateFunc func(ctx context.Context, obj runtime.Object) (admission.Warnings, error)
	ValidateUpdateFunc func(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error)
	ValidateDeleteFunc func(ctx context.Context, obj runtime.Object) (admission.Warnings, error)
}

func (m *mockValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	if m.ValidateCreateFunc != nil {
		return m.ValidateCreateFunc(ctx, obj)
	}
	return nil, nil
}

func (m *mockValidator) ValidateUpdate(
	ctx context.Context,
	oldObj, newObj runtime.Object,
) (admission.Warnings, error) {
	if m.ValidateUpdateFunc != nil {
		return m.ValidateUpdateFunc(ctx, oldObj, newObj)
	}
	return nil, nil
}

func (m *mockValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	if m.ValidateDeleteFunc != nil {
		return m.ValidateDeleteFunc(ctx, obj)
	}
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

func (m *mockStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, obj, opts...)
	}
	return nil
}

func (m *mockStatusWriter) Patch(
	ctx context.Context,
	obj client.Object,
	patch client.Patch,
	opts ...client.SubResourcePatchOption,
) error {
	return nil
}

func (m *mockStatusWriter) Create(
	ctx context.Context,
	obj client.Object,
	subResource client.Object,
	opts ...client.SubResourceCreateOption,
) error {
	return nil
}

var _ = Describe("Admission Guard", func() {
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

	Describe("ensureResourceIsDefaulted", func() {
		It("should return empty result when no defaulter is set", func() {
			admission := &Admission{
				Defaulter: nil,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: true,
			}

			result, err := admission.ensureResourceIsDefaulted(ctx, params)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
		})

		It("should return empty result when defaulter doesn't change the object", func() {
			defaulter := &mockDefaulter{
				DefaultFunc: func(ctx context.Context, obj runtime.Object) error {
					return nil
				},
			}

			admission := &Admission{
				Defaulter: defaulter,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: true,
			}

			result, err := admission.ensureResourceIsDefaulted(ctx, params)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
		})

		It("should apply changes and requeue when defaulter modifies the object and ApplyChanges is true", func() {
			updateCalled := false
			defaulter := &mockDefaulter{
				DefaultFunc: func(ctx context.Context, obj runtime.Object) error {
					mockObj := obj.(*mockAdmittableObject)
					mockObj.Data["defaulted"] = "true"
					return nil
				},
			}

			mockCl.UpdateFunc = func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
				updateCalled = true
				return nil
			}

			admission := &Admission{
				Defaulter: defaulter,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: true,
			}

			result, err := admission.ensureResourceIsDefaulted(ctx, params)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeFalse())
			Expect(result.RequeueAfter).To(Equal(1 * time.Second))
			Expect(updateCalled).To(BeTrue())
		})

		It("should requeue without applying changes when defaulter modifies the object and ApplyChanges is false", func() {
			updateCalled := false
			defaulter := &mockDefaulter{
				DefaultFunc: func(ctx context.Context, obj runtime.Object) error {
					mockObj := obj.(*mockAdmittableObject)
					mockObj.Data["defaulted"] = "true"
					return nil
				},
			}

			mockCl.UpdateFunc = func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
				updateCalled = true
				return nil
			}

			admission := &Admission{
				Defaulter: defaulter,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: false,
			}

			result, err := admission.ensureResourceIsDefaulted(ctx, params)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeFalse())
			Expect(result.RequeueAfter).To(Equal(5 * time.Second))
			Expect(updateCalled).To(BeFalse())
		})

		It("should return error when defaulter fails", func() {
			expectedError := errors.New("defaulting failed")
			defaulter := &mockDefaulter{
				DefaultFunc: func(ctx context.Context, obj runtime.Object) error {
					return expectedError
				},
			}

			admission := &Admission{
				Defaulter: defaulter,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: true,
			}

			result, err := admission.ensureResourceIsDefaulted(ctx, params)
			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(expectedError))
			Expect(result.IsZero()).To(BeTrue())
		})

		It("should return error when client update fails", func() {
			updateError := errors.New("update failed")
			defaulter := &mockDefaulter{
				DefaultFunc: func(ctx context.Context, obj runtime.Object) error {
					mockObj := obj.(*mockAdmittableObject)
					mockObj.Data["defaulted"] = "true"
					return nil
				},
			}

			mockCl.UpdateFunc = func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
				return updateError
			}

			admission := &Admission{
				Defaulter: defaulter,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: true,
			}

			result, err := admission.ensureResourceIsDefaulted(ctx, params)
			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(updateError))
			Expect(result.IsZero()).To(BeTrue())
		})
	})

	Describe("ensureResourceIsValid", func() {
		It("should return empty result when no validator is set", func() {
			admission := &Admission{
				Validator: nil,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: true,
			}

			result, err := admission.ensureResourceIsValid(ctx, params)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
		})

		It("should clear admission error and return empty result when validation succeeds", func() {
			obj.AdmissionError = "previous error"

			validator := &mockValidator{
				ValidateCreateFunc: func(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
					return nil, nil
				},
			}

			admission := &Admission{
				Validator: validator,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: true,
			}

			result, err := admission.ensureResourceIsValid(ctx, params)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
			Expect(obj.AdmissionError).To(Equal(""))
		})

		It("should return terminal error when validation fails and ApplyChanges is false", func() {
			validationError := errors.New("validation failed")
			validator := &mockValidator{
				ValidateCreateFunc: func(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
					return nil, validationError
				},
			}

			admission := &Admission{
				Validator: validator,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: false,
			}

			result, err := admission.ensureResourceIsValid(ctx, params)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())
			Expect(result.IsZero()).To(BeTrue())
		})

		It("should set admission error and update status when validation fails and ApplyChanges is true", func() {
			validationError := errors.New("validation failed")
			statusUpdateCalled := false

			validator := &mockValidator{
				ValidateCreateFunc: func(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
					return nil, validationError
				},
			}

			mockCl.StatusUpdateFunc = func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				statusUpdateCalled = true
				mockObj := obj.(*mockAdmittableObject)
				Expect(mockObj.AdmissionError).To(Equal("validation failed"))
				return nil
			}

			admission := &Admission{
				Validator: validator,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: true,
			}

			result, err := admission.ensureResourceIsValid(ctx, params)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())
			Expect(result.IsZero()).To(BeTrue())
			Expect(obj.AdmissionError).To(Equal("validation failed"))
			Expect(statusUpdateCalled).To(BeTrue())
		})

		It("should return error when status update fails", func() {
			validationError := errors.New("validation failed")
			updateError := errors.New("status update failed")

			validator := &mockValidator{
				ValidateCreateFunc: func(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
					return nil, validationError
				},
			}

			mockCl.StatusUpdateFunc = func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				return updateError
			}

			admission := &Admission{
				Validator: validator,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: true,
			}

			result, err := admission.ensureResourceIsValid(ctx, params)
			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(updateError))
			Expect(result.IsZero()).To(BeTrue())
		})

		It("should handle warnings from validator", func() {
			warnings := admission.Warnings{"warning1", "warning2"}
			validator := &mockValidator{
				ValidateCreateFunc: func(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
					return warnings, nil
				},
			}

			admission := &Admission{
				Validator: validator,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: true,
			}

			result, err := admission.ensureResourceIsValid(ctx, params)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
			Expect(obj.AdmissionError).To(Equal(""))
		})
	})

	Describe("EnsureResourceIsAdmitted", func() {
		It("should succeed when both defaulting and validation pass", func() {
			defaulter := &mockDefaulter{
				DefaultFunc: func(ctx context.Context, obj runtime.Object) error {
					return nil
				},
			}

			validator := &mockValidator{
				ValidateCreateFunc: func(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
					return nil, nil
				},
			}

			admission := &Admission{
				Defaulter: defaulter,
				Validator: validator,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: true,
			}

			result, err := admission.EnsureResourceIsAdmitted(ctx, params)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
		})

		It("should return early when defaulting requires requeue", func() {
			defaulter := &mockDefaulter{
				DefaultFunc: func(ctx context.Context, obj runtime.Object) error {
					mockObj := obj.(*mockAdmittableObject)
					mockObj.Data["defaulted"] = "true"
					return nil
				},
			}

			validationCalled := false
			validator := &mockValidator{
				ValidateCreateFunc: func(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
					validationCalled = true
					return nil, nil
				},
			}

			mockCl.UpdateFunc = func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
				return nil
			}

			admission := &Admission{
				Defaulter: defaulter,
				Validator: validator,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: true,
			}

			result, err := admission.EnsureResourceIsAdmitted(ctx, params)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeFalse())
			Expect(result.RequeueAfter).To(Equal(1 * time.Second))
			Expect(validationCalled).To(BeFalse())
		})

		It("should return early when defaulting fails", func() {
			defaultingError := errors.New("defaulting failed")
			defaulter := &mockDefaulter{
				DefaultFunc: func(ctx context.Context, obj runtime.Object) error {
					return defaultingError
				},
			}

			validationCalled := false
			validator := &mockValidator{
				ValidateCreateFunc: func(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
					validationCalled = true
					return nil, nil
				},
			}

			admission := &Admission{
				Defaulter: defaulter,
				Validator: validator,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: true,
			}

			result, err := admission.EnsureResourceIsAdmitted(ctx, params)
			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(defaultingError))
			Expect(result.IsZero()).To(BeTrue())
			Expect(validationCalled).To(BeFalse())
		})

		It("should fail when validation fails after successful defaulting", func() {
			defaulter := &mockDefaulter{
				DefaultFunc: func(ctx context.Context, obj runtime.Object) error {
					return nil
				},
			}

			validationError := errors.New("validation failed")
			validator := &mockValidator{
				ValidateCreateFunc: func(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
					return nil, validationError
				},
			}

			admission := &Admission{
				Defaulter: defaulter,
				Validator: validator,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: false,
			}

			result, err := admission.EnsureResourceIsAdmitted(ctx, params)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())
			Expect(result.IsZero()).To(BeTrue())
		})

		It("should work with nil defaulter and validator", func() {
			admission := &Admission{
				Defaulter: nil,
				Validator: nil,
			}

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: true,
			}

			result, err := admission.EnsureResourceIsAdmitted(ctx, params)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
		})
	})

	Describe("Nil Admission Guard", func() {
		It("should handle nil *Admission receiver gracefully", func() {
			var nilAdmission *Admission

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: true,
			}

			result, err := nilAdmission.EnsureResourceIsAdmitted(ctx, params)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
		})

		It("should handle nil *Admission receiver with ApplyChanges false", func() {
			var nilAdmission *Admission

			params := AdmissionParams{
				Object:       obj,
				Client:       mockCl,
				ApplyChanges: false,
			}

			result, err := nilAdmission.EnsureResourceIsAdmitted(ctx, params)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
		})
	})
})
