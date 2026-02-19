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
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/hash"
	"github.com/cloudnative-pg/machinery/pkg/log"
)

// AdmittableObject represents a Kubernetes object that can be admitted through
// the guard admission control process, allowing admission errors to be set.
type AdmittableObject interface {
	client.Object

	SetAdmissionError(msg string)
}

// Admission provides admission control capabilities by wrapping defaulting
// and validation webhooks for use in controller reconciliation loops.
type Admission struct {
	Defaulter webhook.CustomDefaulter
	Validator webhook.CustomValidator
}

// AdmissionParams contains the parameters needed to perform admission control
// on a resource during reconciliation.
type AdmissionParams struct {
	Object       AdmittableObject
	Client       client.Client
	ApplyChanges bool
}

// EnsureResourceIsAdmitted ensures that a resource has been properly defaulted and validated
// according to the admission webhooks, applying changes if necessary when webhooks are not installed.
func (g *Admission) EnsureResourceIsAdmitted(ctx context.Context, params AdmissionParams) (ctrl.Result, error) {
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

func (g *Admission) ensureResourceIsDefaulted(ctx context.Context, params AdmissionParams) (ctrl.Result, error) {
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
		contextLogger.Error(err, "Unable to applying the defaulting logic to resource")
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

func (g *Admission) ensureResourceIsValid(ctx context.Context, params AdmissionParams) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if g.Validator == nil {
		return ctrl.Result{}, nil
	}

	// Important: in this situation, we don't have access to the old version
	// of the cluster. We validate is as the object was created from scratch - that's
	// the best approximation we have.
	warnings, validationErr := g.Validator.ValidateCreate(ctx, params.Object)
	if validationErr == nil {
		params.Object.SetAdmissionError("")
		return ctrl.Result{}, nil
	}

	contextLogger.Info("Detected invalid resource, stopping reconciliation", "warnings", warnings)
	if !params.ApplyChanges {
		return ctrl.Result{}, reconcile.TerminalError(validationErr)
	}

	params.Object.SetAdmissionError(validationErr.Error())
	if err := params.Client.Status().Update(ctx, params.Object); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, reconcile.TerminalError(validationErr)
}
