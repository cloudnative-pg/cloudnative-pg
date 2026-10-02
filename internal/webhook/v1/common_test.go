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
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func newClusterWithValidationAnnotation(value string) *apiv1.Cluster {
	return &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				utils.WebhookValidationAnnotationName: value,
			},
		},
	}
}

var _ = Describe("Validation webhook validation parser", func() {
	It("ensures that with no annotations the validation checking is enabled", func() {
		cluster := &apiv1.Cluster{}
		Expect(isValidationEnabled(cluster)).To(BeTrue())
	})

	It("ensures that with validation can be explicitly enabled", func() {
		cluster := newClusterWithValidationAnnotation(validationEnabledAnnotationValue)
		Expect(isValidationEnabled(cluster)).To(BeTrue())
	})

	It("ensures that with validation can be explicitly disabled", func() {
		cluster := newClusterWithValidationAnnotation(validationDisabledAnnotationValue)
		Expect(isValidationEnabled(cluster)).To(BeFalse())
	})

	It("ensures that with validation is enabled when the annotation value is unknown", func() {
		cluster := newClusterWithValidationAnnotation("idontknow")
		status, err := isValidationEnabled(cluster)
		Expect(err).To(HaveOccurred())
		Expect(status).To(BeTrue())
	})
})

type fakeCustomValidator struct {
	calls []string

	createWarnings admission.Warnings
	createError    error

	updateWarnings admission.Warnings
	updateError    error

	deleteWarnings admission.Warnings
	deleteError    error
}

func (f *fakeCustomValidator) ValidateCreate(
	_ context.Context,
	_ *apiv1.Cluster,
) (admission.Warnings, error) {
	f.calls = append(f.calls, "create")
	return f.createWarnings, f.createError
}

func (f *fakeCustomValidator) ValidateUpdate(
	_ context.Context,
	_ *apiv1.Cluster,
	_ *apiv1.Cluster,
) (admission.Warnings, error) {
	f.calls = append(f.calls, "update")
	return f.updateWarnings, f.updateError
}

func (f *fakeCustomValidator) ValidateDelete(
	_ context.Context,
	_ *apiv1.Cluster,
) (admission.Warnings, error) {
	f.calls = append(f.calls, "delete")
	return f.deleteWarnings, f.deleteError
}

var _ = Describe("Bypassable validator", func() {
	fakeCreateError := fmt.Errorf("fake error")
	fakeUpdateError := fmt.Errorf("fake error")
	fakeDeleteError := fmt.Errorf("fake error")

	disabledCluster := newClusterWithValidationAnnotation(validationDisabledAnnotationValue)
	enabledCluster := newClusterWithValidationAnnotation(validationEnabledAnnotationValue)
	wrongCluster := newClusterWithValidationAnnotation("dontknow")

	fakeErrorValidator := &fakeCustomValidator{
		createError: fakeCreateError,
		deleteError: fakeDeleteError,
		updateError: fakeUpdateError,
	}

	DescribeTable(
		"validator callbacks",
		func(ctx SpecContext, c *apiv1.Cluster, expectedError, withWarnings bool) {
			b := newBypassableValidator[*apiv1.Cluster](fakeErrorValidator)

			By("creation entrypoint", func() {
				result, err := b.ValidateCreate(ctx, c)
				if expectedError {
					Expect(err).To(Equal(fakeCreateError))
				} else {
					Expect(err).ToNot(HaveOccurred())
				}

				if withWarnings {
					Expect(result).To(HaveLen(1))
				}
			})

			By("update entrypoint", func() {
				result, err := b.ValidateUpdate(ctx, enabledCluster, c)
				if expectedError {
					Expect(err).To(Equal(fakeUpdateError))
				} else {
					Expect(err).ToNot(HaveOccurred())
				}

				if withWarnings {
					Expect(result).To(HaveLen(1))
				}
			})

			By("delete entrypoint", func() {
				result, err := b.ValidateDelete(ctx, c)
				if expectedError {
					Expect(err).To(Equal(fakeDeleteError))
				} else {
					Expect(err).ToNot(HaveOccurred())
				}

				if withWarnings {
					Expect(result).To(HaveLen(1))
				}
			})
		},
		Entry("validation is disabled", disabledCluster, false, true),
		Entry("validation is enabled", enabledCluster, true, false),
		Entry("validation value is not expected", wrongCluster, true, true),
	)
})
