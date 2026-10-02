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
	"fmt"
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/hash"
)

// AdmittableObject represents a Kubernetes object that can be admitted through
// the guard admission control process, allowing admission errors to be set.
type AdmittableObject interface {
	client.Object

	// SetAdmissionError records the admission validation error on the status,
	// or clears it when msg is empty.
	SetAdmissionError(msg string)

	// GetAdmissionError returns the admission validation error currently
	// recorded on the status, when the guard is responsible for clearing it.
	// Types whose admission error is cleared by their own reconciler (for
	// example through the phase machinery) return an empty string, so the
	// guard does not race that logic by persisting the clear itself.
	GetAdmissionError() string
}

// Admission provides admission control capabilities by wrapping defaulting
// and validation webhooks for use in controller reconciliation loops.
type Admission[T AdmittableObject] struct {
	Defaulter admission.Defaulter[T]
	Validator admission.Validator[T]
}

// AdmissionParams contains the parameters needed to perform admission control
// on a resource during reconciliation.
type AdmissionParams[T AdmittableObject] struct {
	Object T
	Client client.Client

	// ApplyChanges must be true only in the reconciler that owns writes to the
	// object. When true, defaulting changes are persisted and validation
	// failures are recorded in the status. When false (for example the instance
	// manager reconciling a Cluster it does not own), the guard works in memory
	// only and waits for the owning reconciler to apply the changes.
	ApplyChanges bool
}

// EnsureResourceIsAdmitted ensures that a resource has been properly defaulted and validated
// according to the admission webhooks, applying changes if necessary when webhooks are not installed.
func (g *Admission[T]) EnsureResourceIsAdmitted(ctx context.Context, params AdmissionParams[T]) (ctrl.Result, error) {
	if g == nil {
		return ctrl.Result{}, nil
	}

	if result, err := g.ensureResourceIsDefaulted(ctx, params); !result.IsZero() || err != nil {
		return result, err
	}

	if result, err := g.ensureResourceIsValid(ctx, params); !result.IsZero() || err != nil {
		return result, err
	}

	return ctrl.Result{}, nil
}

func (g *Admission[T]) ensureResourceIsDefaulted(ctx context.Context, params AdmissionParams[T]) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if g.Defaulter == nil {
		return ctrl.Result{}, nil
	}

	hashBeforeDefaulting, err := hash.ComputeHash(params.Object)
	if err != nil {
		contextLogger.Error(err, "Unable to compute hash for resource before applying the defaulting webhook, skipping")
		return ctrl.Result{}, err
	}

	if err := g.Defaulter.Default(ctx, params.Object); err != nil {
		contextLogger.Error(err, "Unable to apply the defaulting logic to resource")
		return ctrl.Result{}, err
	}

	hashAfterDefaulting, err := hash.ComputeHash(params.Object)
	if err != nil {
		contextLogger.Error(err, "Unable to compute hash for resource after applying the defaulting webhook, skipping")
		return ctrl.Result{}, err
	}

	if hashBeforeDefaulting != hashAfterDefaulting {
		if params.ApplyChanges {
			contextLogger.Info("Mutating webhook seems not installed, applying changes")
			if err := params.Client.Update(ctx, params.Object); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}

		contextLogger.Info("Mutating webhook seems not installed, waiting for changes to be applied")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func (g *Admission[T]) ensureResourceIsValid(ctx context.Context, params AdmissionParams[T]) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if g.Validator == nil {
		return ctrl.Result{}, nil
	}

	// Important: in this situation, we don't have access to the old version
	// of the cluster. We validate it as if the object was created from scratch - that's
	// the best approximation we have.
	warnings, validationErr := g.Validator.ValidateCreate(ctx, params.Object)
	if validationErr == nil {
		// Clear a previously recorded admission error. The defaulting path
		// persists its own changes, but the validation success path must
		// persist the cleared status itself: the controllers either skip the
		// status write when nothing else changed, or build their patch base
		// from this already-mutated object, so an in-memory clear would never
		// reach the API server and the stale error would stick.
		hadError := params.ApplyChanges && params.Object.GetAdmissionError() != ""
		params.Object.SetAdmissionError("")
		if hadError {
			if err := params.Client.Status().Update(ctx, params.Object); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// The full validation error can embed field values such as connection
	// strings, object-store URLs or secret names; keep it in the logs only and
	// persist just a sanitized summary to the world-readable status.
	contextLogger.Info("Detected invalid resource, stopping reconciliation",
		"warnings", warnings, "validationError", validationErr.Error())
	if !params.ApplyChanges {
		// We do not own writes to this object, so we can neither fix its spec
		// nor record the error in its status: only wait for the owning
		// reconciler to apply a correction. Requeue instead of returning a
		// TerminalError, which would stop this controller from retrying and
		// stall reconciliation until an unrelated event happened to wake it up.
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	params.Object.SetAdmissionError(sanitizeValidationError(validationErr))
	if err := params.Client.Status().Update(ctx, params.Object); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, reconcile.TerminalError(validationErr)
}

// sanitizeValidationError returns a summary of a validation error that is safe
// to store in the world-readable resource status. It lists only the offending
// field paths, never their values, which may contain sensitive data such as
// connection strings, object-store URLs or secret names.
func sanitizeValidationError(err error) string {
	var statusErr *apierrors.StatusError
	if errors.As(err, &statusErr) && statusErr.ErrStatus.Details != nil {
		var fields []string
		for _, cause := range statusErr.ErrStatus.Details.Causes {
			if cause.Field != "" {
				fields = append(fields, cause.Field)
			}
		}
		if len(fields) > 0 {
			return fmt.Sprintf(
				"the resource failed admission validation on: %s (see the operator logs for details)",
				strings.Join(fields, ", "))
		}
	}

	return "the resource failed admission validation (see the operator logs for details)"
}
