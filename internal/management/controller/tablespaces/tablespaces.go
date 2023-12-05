/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tablespaces

import (
	"context"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/tablespaces/infrastructure"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

type tablespaceStorageManager interface {
	getStorageLocation(tbsName string) string
	storageExists(tbsName string) (bool, error)
}

type instanceTablespaceStorageManager struct{}

func (ism instanceTablespaceStorageManager) getStorageLocation(tbsName string) string {
	return specs.LocationForTablespace(tbsName)
}

func (ism instanceTablespaceStorageManager) storageExists(tbsName string) (bool, error) {
	return fileutils.FileExists(ism.getStorageLocation(tbsName))
}

type (
	// TablespaceAction encodes the action necessary for a tablespaceAction
	TablespaceAction string
	// TablespaceByAction tablespaces group by action which is needed to take
	TablespaceByAction map[TablespaceAction][]apiv1.TablespaceConfiguration
	// TablespaceNameByStatus tablespace name group by status which will applied to cluster status
	TablespaceNameByStatus map[apiv1.TablespaceStatus][]string
)

// possible tablespace actions
const (
	// TbsIsReconciled means the tablespace already reconciled
	TbsIsReconciled TablespaceAction = "RECONCILED"
	// TbsToCreate means the tablespace needs to be created
	TbsToCreate TablespaceAction = "CREATE"
	// TbsToUpdate means the tablespace needs to be updated
	TbsToUpdate TablespaceAction = "UPDATE"
)

// TablespaceConfigurationAdapter the adapter class for tablespace configuration
type TablespaceConfigurationAdapter struct {
	// Name tablespace name
	Name string
	// TablespaceConfiguration tablespace with configuration settings
	apiv1.TablespaceConfiguration
}

// evaluateNextActions evaluates the next action going to take for tablespace
func evaluateNextActions(
	ctx context.Context,
	tablespaceInDBSlice []infrastructure.Tablespace,
	tablespaceInSpecSlice []apiv1.TablespaceConfiguration,
) TablespaceByAction {
	contextLog := log.FromContext(ctx).WithName("tbs_reconciler")
	contextLog.Debug("evaluating tablespace actions")

	tablespaceByAction := make(TablespaceByAction)

	tbsInDBNamed := make(map[string]infrastructure.Tablespace)
	for idx, tbs := range tablespaceInDBSlice {
		tbsInDBNamed[tbs.Name] = tablespaceInDBSlice[idx]
	}

	// we go through all the tablespaces in spec and create them if missing in DB
	// NOTE: we do not at the moment support update/Delete
	for _, tbsInSpec := range tablespaceInSpecSlice {
		tbsInSpec := tbsInSpec
		dbTablespace, isTbsInDB := tbsInDBNamed[tbsInSpec.Name]

		switch {
		case !isTbsInDB:
			tablespaceByAction[TbsToCreate] = append(tablespaceByAction[TbsToCreate],
				tbsInSpec)

		case dbTablespace.Owner != tbsInSpec.Owner:
			tablespaceByAction[TbsToUpdate] = append(tablespaceByAction[TbsToUpdate],
				tbsInSpec)

		default:
			tablespaceByAction[TbsIsReconciled] = append(tablespaceByAction[TbsIsReconciled],
				tbsInSpec)
		}
	}

	return tablespaceByAction
}

// convertToTablespaceNameByStatus convert the next action need to status, so we can patch it to cluster
func (r TablespaceByAction) convertToTablespaceNameByStatus() TablespaceNameByStatus {
	statusByAction := map[TablespaceAction]apiv1.TablespaceStatus{
		TbsIsReconciled: apiv1.TablespaceStatusReconciled,
		TbsToCreate:     apiv1.TablespaceStatusPendingReconciliation,
		TbsToUpdate:     apiv1.TablespaceStatusPendingReconciliation,
	}

	tablespaceByStatus := make(TablespaceNameByStatus)
	for action, tbsAdapterSlice := range r {
		for _, tbsAdapter := range tbsAdapterSlice {
			tablespaceByStatus[statusByAction[action]] = append(tablespaceByStatus[statusByAction[action]],
				tbsAdapter.Name)
		}
	}

	return tablespaceByStatus
}

// getTablespaceNames convert the TablespaceConfiguration slice to tablespaceName slice
func getTablespaceNames(tbsSlice []apiv1.TablespaceConfiguration) []string {
	names := make([]string, len(tbsSlice))
	for i, tbs := range tbsSlice {
		names[i] = tbs.Name
	}
	return names
}
