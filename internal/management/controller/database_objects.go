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
	"database/sql"
	"fmt"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

type databaseObjectSpec interface {
	GetName() string
	GetEnsure() apiv1.EnsureOption
}

type databaseObjectManager[Spec databaseObjectSpec, Info any] struct {
	get    func(ctx context.Context, db *sql.DB, spec Spec) (*Info, error)
	create func(ctx context.Context, db *sql.DB, spec Spec) error
	update func(ctx context.Context, db *sql.DB, spec Spec, info *Info) error
	drop   func(ctx context.Context, db *sql.DB, spec Spec) error
}

func createFailedStatus(name, message string) apiv1.DatabaseObjectStatus {
	return apiv1.DatabaseObjectStatus{
		Name:    name,
		Applied: false,
		Message: message,
	}
}

func createSuccessStatus(name string) apiv1.DatabaseObjectStatus {
	return apiv1.DatabaseObjectStatus{
		Name:    name,
		Applied: true,
	}
}

func (r *databaseObjectManager[Spec, Info]) reconcileList(
	ctx context.Context,
	db *sql.DB,
	specs []Spec,
) []apiv1.DatabaseObjectStatus {
	result := make([]apiv1.DatabaseObjectStatus, len(specs))
	for i := range specs {
		spec := specs[i]
		result[i] = r.reconcile(ctx, db, spec)
	}
	return result
}

func (r *databaseObjectManager[Spec, Info]) reconcile(
	ctx context.Context,
	db *sql.DB,
	spec Spec,
) apiv1.DatabaseObjectStatus {
	info, err := r.get(ctx, db, spec)
	if err != nil {
		return createFailedStatus(
			spec.GetName(),
			fmt.Sprintf("while reading the object %#v: %v", spec, err),
		)
	}

	exists := info != nil
	ensureOption := spec.GetEnsure()

	switch {
	case !exists && ensureOption == apiv1.EnsurePresent:
		return r.reconcileCreate(ctx, db, spec)

	case !exists && ensureOption == apiv1.EnsureAbsent:
		return createSuccessStatus(spec.GetName())

	case exists && ensureOption == apiv1.EnsurePresent:
		return r.reconcileUpdate(ctx, db, spec, info)

	case exists && ensureOption == apiv1.EnsureAbsent:
		return r.reconcileDrop(ctx, db, spec)

	default:
		// If this happens, the CRD and/or the validating webhook
		// are not working properly. In this case, let's do nothing:
		// better to be safe than sorry.
		return createSuccessStatus(spec.GetName())
	}
}

func (r *databaseObjectManager[Spec, Info]) reconcileCreate(
	ctx context.Context,
	db *sql.DB,
	spec Spec,
) apiv1.DatabaseObjectStatus {
	if err := r.create(ctx, db, spec); err != nil {
		return createFailedStatus(
			spec.GetName(),
			err.Error(),
		)
	}

	return createSuccessStatus(spec.GetName())
}

func (r *databaseObjectManager[Spec, Info]) reconcileUpdate(
	ctx context.Context, db *sql.DB, spec Spec, info *Info,
) apiv1.DatabaseObjectStatus {
	if err := r.update(ctx, db, spec, info); err != nil {
		return createFailedStatus(
			spec.GetName(),
			err.Error(),
		)
	}

	return createSuccessStatus(spec.GetName())
}

func (r *databaseObjectManager[Spec, Info]) reconcileDrop(
	ctx context.Context,
	db *sql.DB,
	spec Spec,
) apiv1.DatabaseObjectStatus {
	if err := r.drop(ctx, db, spec); err != nil {
		return createFailedStatus(
			spec.GetName(),
			err.Error(),
		)
	}

	return createSuccessStatus(spec.GetName())
}
