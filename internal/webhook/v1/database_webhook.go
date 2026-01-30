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

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// databaseLog is for logging in this package.
var databaseLog = log.WithName("database-resource").WithValues("version", "v1")

// SetupDatabaseWebhookWithManager registers the webhook for Database in the manager.
func SetupDatabaseWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &apiv1.Database{}).
		WithValidator(newBypassableValidator[*apiv1.Database](&DatabaseCustomValidator{client: mgr.GetClient()})).
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
type DatabaseCustomValidator struct {
	client client.Client
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Database .
func (v *DatabaseCustomValidator) ValidateCreate(
	ctx context.Context, database *apiv1.Database,
) (admission.Warnings, error) {
	databaseLog.Info(
		"Validation for Database upon creation",
		"name", database.GetName(), "namespace", database.GetNamespace())

	allErrs := v.validate(database)
	allErrs = append(allErrs, v.validateCrossNamespaceCluster(ctx, database)...)
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
	ctx context.Context,
	oldDatabase *apiv1.Database, database *apiv1.Database,
) (admission.Warnings, error) {
	databaseLog.Info(
		"Validation for Database upon update",
		"name", database.GetName(), "namespace", database.GetNamespace())

	allErrs := append(
		v.validate(database),
		v.validateDatabaseChanges(database, oldDatabase)...,
	)
	allErrs = append(allErrs, v.validateCrossNamespaceCluster(ctx, database)...)
	allWarnings := v.getAdmissionWarnings(database)

	if len(allErrs) == 0 {
		return allWarnings, nil
	}

	return allWarnings, apierrors.NewInvalid(
		schema.GroupKind{Group: "database.cnpg.io", Kind: "Database "},
		database.Name, allErrs)
}

func (v *DatabaseCustomValidator) validateDatabaseChanges(newDB, oldDB *apiv1.Database) field.ErrorList {
	var allErrs field.ErrorList

	// Prevent changing cluster.namespace after creation
	if oldDB.Spec.ClusterRef.Namespace != "" &&
		newDB.Spec.ClusterRef.Namespace != oldDB.Spec.ClusterRef.Namespace {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "cluster", "namespace"),
			newDB.Spec.ClusterRef.Namespace,
			"cluster.namespace is immutable once set",
		))
	}

	return allErrs
}

// validateCrossNamespaceCluster validates that if the Database references a Cluster
// in a different namespace, the Cluster must have EnableCrossNamespaceDatabases set to true.
func (v *DatabaseCustomValidator) validateCrossNamespaceCluster(
	ctx context.Context,
	db *apiv1.Database,
) field.ErrorList {
	// If not cross-namespace, no validation needed
	if !db.IsCrossNamespace() {
		return nil
	}

	// If no client is available (e.g., in unit tests), skip this validation
	if v.client == nil {
		return nil
	}

	// Fetch the referenced Cluster
	cluster := &apiv1.Cluster{}
	clusterKey := types.NamespacedName{
		Name:      db.Spec.ClusterRef.Name,
		Namespace: db.Spec.ClusterRef.Namespace,
	}

	if err := v.client.Get(ctx, clusterKey, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return field.ErrorList{
				field.NotFound(
					field.NewPath("spec", "cluster"),
					fmt.Sprintf("%s/%s", db.Spec.ClusterRef.Namespace, db.Spec.ClusterRef.Name),
				),
			}
		}
		// For other errors, log and allow (fail open) to avoid blocking all operations
		databaseLog.Error(err, "Failed to fetch cluster for cross-namespace validation",
			"cluster", clusterKey)
		return nil
	}

	// Check if the cluster has EnableCrossNamespaceDatabases set to true
	if !cluster.Spec.EnableCrossNamespaceDatabases {
		return field.ErrorList{
			field.Forbidden(
				field.NewPath("spec", "cluster", "namespace"),
				fmt.Sprintf(
					"cluster %q does not have enableCrossNamespaceDatabases set to true",
					db.Spec.ClusterRef.Name,
				),
			),
		}
	}

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
		v.validateFDWs,
		v.validateForeignServers,
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
	for i, schemaSpec := range d.Spec.Schemas {
		name := schemaSpec.Name
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

// validateFDWs validates the database Foreign Data Wrappers
// FDWs must be unique in .spec.fdws
func (v *DatabaseCustomValidator) validateFDWs(d *apiv1.Database) field.ErrorList {
	var result field.ErrorList
	nameSet := stringset.New()
	basePath := field.NewPath("spec", "fdws")
	for i, fdw := range d.Spec.FDWs {
		itemPath := basePath.Index(i)
		errs := validateNameOptionsUsages(itemPath, fdw.Name, fdw.Options, fdw.Usages, nameSet)
		result = append(result, errs...)
	}
	return result
}

// validateForeignServers validates foreign servers: uniqueness, options/usages duplicates,
// and that each referenced FDW exists.
func (v *DatabaseCustomValidator) validateForeignServers(d *apiv1.Database) field.ErrorList {
	basePath := field.NewPath("spec", "servers")

	fdwNames := stringset.New()
	for _, fdw := range d.Spec.FDWs {
		fdwNames.Put(fdw.Name)
	}

	nameSet := stringset.New()
	var allErrs field.ErrorList
	for i, server := range d.Spec.Servers {
		itemPath := basePath.Index(i)

		allErrs = append(allErrs, v.validateServerFDWReference(fdwNames, server, itemPath)...)

		allErrs = append(allErrs,
			validateNameOptionsUsages(itemPath, server.Name, server.Options, server.Usages, nameSet)...)
	}

	return allErrs
}

// validateServerFDWReference ensures the server references an existing FDW (and is non-empty).
func (v *DatabaseCustomValidator) validateServerFDWReference(
	fdwNames *stringset.Data,
	server apiv1.ServerSpec,
	itemPath *field.Path,
) field.ErrorList {
	if server.GetEnsure() == apiv1.EnsureAbsent || fdwNames.Has(server.FdwName) {
		return nil
	}

	return field.ErrorList{field.Invalid(
		itemPath.Child("fdw"),
		server.FdwName,
		"referenced fdw not defined in spec.fdws",
	)}
}

// validateNameOptionsUsages validates a single named object with options and usages, tracking duplicates.
func validateNameOptionsUsages(
	itemPath *field.Path,
	name string,
	options []apiv1.OptionSpec,
	usages []apiv1.UsageSpec,
	existingNames *stringset.Data,
) field.ErrorList {
	var errs field.ErrorList

	if existingNames.Has(name) {
		errs = append(errs, field.Duplicate(itemPath.Child("name"), name))
	}
	existingNames.Put(name)

	optionNames := stringset.New()
	for i, option := range options {
		if optionNames.Has(option.Name) {
			errs = append(errs, field.Duplicate(itemPath.Child("options").Index(i).Child("name"), option.Name))
		}
		optionNames.Put(option.Name)
	}

	usageNames := stringset.New()
	for i, usage := range usages {
		if usageNames.Has(usage.Name) {
			errs = append(errs, field.Duplicate(itemPath.Child("usages").Index(i).Child("name"), usage.Name))
		}
		usageNames.Put(usage.Name)
	}

	return errs
}
