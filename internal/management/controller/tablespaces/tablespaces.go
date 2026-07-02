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

	"github.com/cloudnative-pg/machinery/pkg/log"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/tablespaces/infrastructure"
)

// evaluateNextSteps evaluates the next steps needed to reconcile tablespaces
func evaluateNextSteps(
	ctx context.Context,
	tablespaceInDBSlice []infrastructure.Tablespace,
	tablespaceInSpecSlice []apiv1.TablespaceConfiguration,
	pvcChecker func(tablespaceName string) bool,
) []tablespaceReconcilerStep {
	contextLog := log.FromContext(ctx).WithName("tbs_reconciler")
	contextLog.Debug("evaluating tablespace actions")

	result := make([]tablespaceReconcilerStep, len(tablespaceInSpecSlice))

	tbsInDBNamed := make(map[string]infrastructure.Tablespace)
	for idx, tbs := range tablespaceInDBSlice {
		tbsInDBNamed[tbs.Name] = tablespaceInDBSlice[idx]
	}

	// we go through all the tablespaces in spec and create them if missing in DB
	// NOTE: we do not at the moment support Dropping tablespaces
	for idx, tbsInSpec := range tablespaceInSpecSlice {
		dbTablespace, isTbsInDB := tbsInDBNamed[tbsInSpec.Name]

		switch {
		case !isTbsInDB:
			result[idx] = &createTablespaceAction{
				tablespace: tbsInSpec,
				pvcChecker: pvcChecker,
			}

		case dbTablespace.Owner != tbsInSpec.Owner.Name:
			result[idx] = &updateTablespaceAction{
				tablespace: tbsInSpec,
			}

		default:
			result[idx] = &noopTablespaceAction{
				tablespace: tbsInSpec,
			}
		}
	}

	return result
}
