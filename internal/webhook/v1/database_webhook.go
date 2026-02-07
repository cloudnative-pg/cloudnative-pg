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

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// databaseLog is for logging in this package.
var databaseLog = log.WithName("database-resource").WithValues("version", "v1")

// SetupDatabaseWebhookWithManager registers the webhook for Database in the manager.
func SetupDatabaseWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &apiv1.Database{}).
		WithValidator(newBypassableValidator[*apiv1.Database](&DatabaseCustomValidator{})).
		WithDefaulter(&DatabaseCustomDefaulter{}).
		Complete()
}

// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
//
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},path=/mutate-postgresql-cnpg-io-v1-database,mutating=true,failurePolicy=fail,groups=postgresql.cnpg.io,resources=databases,verbs=create;update,versions=v1,name=mdatabase.cnpg.io,sideEffects=None
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},verbs=create;update,path=/validate-postgresql-cnpg-io-v1-database,mutating=false,failurePolicy=fail,groups=postgresql.cnpg.io,resources=databases,versions=v1,name=vdatabase.cnpg.io,sideEffects=None

// DatabaseCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Database when those are created or updated.
type DatabaseCustomDefaulter struct{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Database.
func (d *DatabaseCustomDefaulter) Default(_ context.Context, database *apiv1.Database) error {
	databaseLog.Info("Defaulting for database", "name", database.GetName(), "namespace", database.GetNamespace())

	// database.Default()

	return nil
}

// DatabaseCustomValidator is responsible for validating the Database
// resource when it is created, updated, or deleted.
type DatabaseCustomValidator struct{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Database .
func (v *DatabaseCustomValidator) ValidateCreate(
	_ context.Context, database *apiv1.Database,
) (admission.Warnings, error) {
	databaseLog.Info(
		"Validation for Database upon creation",
		"name", database.GetName(), "namespace", database.GetNamespace())

	allErrs := v.validate(database)
	allWarnings := v.getAdmissionWarnings(database)

	if len(allErrs) == 0 {
		return allWarnings, nil
	}

	return allWarnings, apierrors.NewInvalid(
		schema.GroupKind{Group: "postgresql.cnpg.io", Kind: "Database "},
		database.Name, allErrs)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Database .
func (v *DatabaseCustomValidator) ValidateUpdate(
	_ context.Context,
	oldDatabase *apiv1.Database, database *apiv1.Database,
) (admission.Warnings, error) {
	databaseLog.Info(
		"Validation for Database upon update",
		"name", database.GetName(), "namespace", database.GetNamespace())

	allErrs := append(
		v.validate(database),
		v.validateDatabaseChanges(database, oldDatabase)...,
	)
	allWarnings := v.getAdmissionWarnings(database)

	if len(allErrs) == 0 {
		return allWarnings, nil
	}

	return allWarnings, apierrors.NewInvalid(
		schema.GroupKind{Group: "database.cnpg.io", Kind: "Database "},
		database.Name, allErrs)
}

func (v *DatabaseCustomValidator) validateDatabaseChanges(_ *apiv1.Database, _ *apiv1.Database) field.ErrorList {
	return nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Database .
func (v *DatabaseCustomValidator) ValidateDelete(
	_ context.Context, database *apiv1.Database,
) (admission.Warnings, error) {
	databaseLog.Info(
		"Validation for Database upon deletion",
		"name", database.GetName(), "namespace", database.GetNamespace())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}

// validateDatabse groups the validation logic for databases returning a list of all encountered errors
func (v *DatabaseCustomValidator) validate(d *apiv1.Database) (allErrs field.ErrorList) {
	type validationFunc func(*apiv1.Database) field.ErrorList
	validations := []validationFunc{
		v.validateExtensions,
		v.validateSchemas,
	}

	for _, validate := range validations {
		allErrs = append(allErrs, validate(d)...)
	}

	return allErrs
}

func (v *DatabaseCustomValidator) getAdmissionWarnings(_ *apiv1.Database) admission.Warnings {
	return nil
}

// validateExtensions validates the database extensions
func (v *DatabaseCustomValidator) validateExtensions(d *apiv1.Database) field.ErrorList {
	var result field.ErrorList

	extensionNames := stringset.New()
	for i, ext := range d.Spec.Extensions {
		name := ext.Name
		if extensionNames.Has(name) {
			result = append(
				result,
				field.Duplicate(
					field.NewPath("spec", "extensions").Index(i).Child("name"),
					name,
				),
			)
		}

		extensionNames.Put(name)
	}

	return result
}

// validateSchemas validates the database schemas
func (v *DatabaseCustomValidator) validateSchemas(d *apiv1.Database) field.ErrorList {
	var result field.ErrorList

	schemaNames := stringset.New()
	for i, schema := range d.Spec.Schemas {
		name := schema.Name
		if schemaNames.Has(name) {
			result = append(
				result,
				field.Duplicate(
					field.NewPath("spec", "schemas").Index(i).Child("name"),
					name,
				),
			)
		}

		schemaNames.Put(name)
	}

	return result
}
