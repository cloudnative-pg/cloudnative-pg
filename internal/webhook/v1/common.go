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

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	// validationEnabledAnnotationValue is the value of that "validation"
	// annotation that is set when the validation is enabled
	validationEnabledAnnotationValue = "enabled"

	// validationDisabledAnnotationValue is the value of that "validation"
	// annotation that is set when the validation is disabled
	validationDisabledAnnotationValue = "disabled"
)

// isValidationEnabled checks whether validation webhooks are
// enabled or disabled
func isValidationEnabled(obj client.Object) (bool, error) {
	value := obj.GetAnnotations()[utils.WebhookValidationAnnotationName]
	switch value {
	case validationEnabledAnnotationValue, "":
		return true, nil

	case validationDisabledAnnotationValue:
		return false, nil

	default:
		return true, fmt.Errorf(
			`invalid %q annotation: %q (expected "enabled" or "disabled")`,
			utils.WebhookValidationAnnotationName, value)
	}
}

// bypassableValidator implements a custom validator that enables an
// existing custom validator to be enabled or disabled via an annotation.
type bypassableValidator[T client.Object] struct {
	validator admission.Validator[T]
}

// newBypassableValidator creates a new custom validator that enables an
// existing custom validator to be enabled or disabled via an annotation.
func newBypassableValidator[T client.Object](validator admission.Validator[T]) *bypassableValidator[T] {
	return &bypassableValidator[T]{
		validator: validator,
	}
}

// ValidateCreate validates the object on creation.
// The optional warnings will be added to the response as warning messages.
// Return an error if the object is invalid.
func (b bypassableValidator[T]) ValidateCreate(
	ctx context.Context,
	obj T,
) (admission.Warnings, error) {
	return validate(obj, func() (admission.Warnings, error) {
		return b.validator.ValidateCreate(ctx, obj)
	})
}

// ValidateUpdate validates the object on update.
// The optional warnings will be added to the response as warning messages.
// Return an error if the object is invalid.
func (b bypassableValidator[T]) ValidateUpdate(
	ctx context.Context,
	oldObj T,
	newObj T,
) (admission.Warnings, error) {
	return validate(newObj, func() (admission.Warnings, error) {
		return b.validator.ValidateUpdate(ctx, oldObj, newObj)
	})
}

// ValidateDelete validates the object on deletion.
// The optional warnings will be added to the response as warning messages.
// Return an error if the object is invalid.
func (b bypassableValidator[T]) ValidateDelete(
	ctx context.Context,
	obj T,
) (admission.Warnings, error) {
	return validate(obj, func() (admission.Warnings, error) {
		return b.validator.ValidateDelete(ctx, obj)
	})
}

const validationDisabledWarning = "validation webhook is disabled — all changes are accepted without validation. " +
	"This may lead to unsafe or destructive operations. Proceed with extreme caution."

func validate(obj client.Object, validator func() (admission.Warnings, error)) (admission.Warnings, error) {
	var warnings admission.Warnings

	validationEnabled, err := isValidationEnabled(obj)
	if err != nil {
		// If the validation annotation value is unexpected, we continue validating
		// the object but we warn the user that the value was wrong
		warnings = append(warnings, err.Error())
	}

	if !validationEnabled {
		warnings = append(warnings, validationDisabledWarning)
		return warnings, nil
	}

	validationWarnings, err := validator()
	warnings = append(warnings, validationWarnings...)
	return warnings, err
}
