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

package tablespaces

import (
	"context"
	"database/sql"

	"github.com/cloudnative-pg/machinery/pkg/log"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/tablespaces/infrastructure"
)

type tablespaceReconcilerStep interface {
	execute(ctx context.Context,
		db *sql.DB,
		tbsStorageManager tablespaceStorageManager,
		pvcChecker func(tablespaceName string) bool,
	) apiv1.TablespaceState
}

type createTablespaceAction struct {
	tablespace apiv1.TablespaceConfiguration
}

func (r *createTablespaceAction) execute(
	ctx context.Context,
	db *sql.DB,
	tbsStorageManager tablespaceStorageManager,
	pvcChecker func(tablespaceName string) bool,
) apiv1.TablespaceState {
	contextLog := log.FromContext(ctx).WithName("tbs_create_reconciler")

	contextLog.Trace("creating tablespace", "tablespace", r.tablespace.Name)
	if exists, err := tbsStorageManager.storageExists(r.tablespace.Name); err != nil || !exists {
		contextLog.Debug("deferring tablespace until creation of the mount point for the new volume",
			"tablespaceName", r.tablespace.Name,
			"tablespacePath", tbsStorageManager.getStorageLocation(r.tablespace.Name))
		return apiv1.TablespaceState{
			Name:  r.tablespace.Name,
			State: apiv1.TablespaceStatusPendingReconciliation,
			Owner: r.tablespace.Owner.Name,
			Error: "deferred until mount point is created",
		}
	}
	if !pvcChecker(r.tablespace.Name) {
		contextLog.Debug("deferring tablespace until required PVCs in all instances are healthy",
			"tablespaceName", r.tablespace.Name,
			"tablespacePath", tbsStorageManager.getStorageLocation(r.tablespace.Name))
		return apiv1.TablespaceState{
			Name:  r.tablespace.Name,
			State: apiv1.TablespaceStatusPendingReconciliation,
			Owner: r.tablespace.Owner.Name,
			Error: "deferred until all required PVCs are healthy",
		}
	}
	tablespace := infrastructure.Tablespace{
		Name:  r.tablespace.Name,
		Owner: r.tablespace.Owner.Name,
	}
	err := infrastructure.Create(ctx, db, tablespace)
	if err != nil {
		contextLog.Error(err, "while performing action", "tablespace", r.tablespace.Name)
		return apiv1.TablespaceState{
			Name:  r.tablespace.Name,
			Owner: r.tablespace.Owner.Name,
			State: apiv1.TablespaceStatusPendingReconciliation,
			Error: err.Error(),
		}
	}

	return apiv1.TablespaceState{
		Name:  r.tablespace.Name,
		Owner: r.tablespace.Owner.Name,
		State: apiv1.TablespaceStatusReconciled,
	}
}

type updateTablespaceAction struct {
	tablespace apiv1.TablespaceConfiguration
}

func (r *updateTablespaceAction) execute(
	ctx context.Context,
	db *sql.DB,
	_ tablespaceStorageManager,
	_ func(tablespaceName string) bool,
) apiv1.TablespaceState {
	contextLog := log.FromContext(ctx).WithName("tbs_update_reconciler")

	contextLog.Trace("updating tablespace ", "tablespace", r.tablespace.Name)
	tablespace := infrastructure.Tablespace{
		Name:  r.tablespace.Name,
		Owner: r.tablespace.Owner.Name,
	}
	err := infrastructure.Update(ctx, db, tablespace)
	if err != nil {
		contextLog.Error(
			err, "while performing action",
			"tablespace", r.tablespace.Name)
		return apiv1.TablespaceState{
			Name:  r.tablespace.Name,
			Owner: r.tablespace.Owner.Name,
			State: apiv1.TablespaceStatusPendingReconciliation,
			Error: err.Error(),
		}
	}

	return apiv1.TablespaceState{
		Name:  r.tablespace.Name,
		Owner: r.tablespace.Owner.Name,
		State: apiv1.TablespaceStatusReconciled,
	}
}

type noopTablespaceAction struct {
	tablespace apiv1.TablespaceConfiguration
}

func (r *noopTablespaceAction) execute(
	_ context.Context,
	_ *sql.DB,
	_ tablespaceStorageManager,
	_ func(tablespaceName string) bool,
) apiv1.TablespaceState {
	return apiv1.TablespaceState{
		Name:  r.tablespace.Name,
		Owner: r.tablespace.Owner.Name,
		State: apiv1.TablespaceStatusReconciled,
	}
}
