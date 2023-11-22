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

package persistentvolumeclaim

import (
	"context"
	"fmt"

	volumesnapshot "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// ValidationStatus is the result of the validation of a cluster
// datasource
type ValidationStatus struct {
	// Errors is the list of blocking errors
	Errors []ValidationMessage `json:"errors"`

	// Warnings is the list of warnings that are not blocking
	Warnings []ValidationMessage `json:"warnings"`
}

// ValidationMessage is a message about a snapshot
type ValidationMessage struct {
	ObjectName string `json:"objectName"`
	Message    string `json:"message"`
}

func newValidationMessage(objectName string, message string) ValidationMessage {
	return ValidationMessage{ObjectName: objectName, Message: message}
}

// ContainsErrors returns true if the validation result
// has any blocking errors.
func (status *ValidationStatus) ContainsErrors() bool {
	return len(status.Errors) > 0
}

// ContainsWarnings returns true if there are any validation warnings.
func (status *ValidationStatus) ContainsWarnings() bool {
	return len(status.Warnings) > 0
}

// AddError adds an error message to the validation status
func (status *ValidationStatus) addErrorf(name string, format string, args ...interface{}) {
	status.Errors = append(status.Errors, newValidationMessage(name, fmt.Sprintf(format, args...)))
}

// AddWarning adds an error message to the validation status
func (status *ValidationStatus) addWarningf(name string, format string, args ...interface{}) {
	status.Warnings = append(status.Warnings, newValidationMessage(name, fmt.Sprintf(format, args...)))
}

// validateVolumeSnapshot validates the label of a volume snapshot,
// adding the result to the status
func (status *ValidationStatus) validateVolumeSnapshot(
	name string,
	snapshot *volumesnapshot.VolumeSnapshot,
	expectedRole PVCRole,
) {
	if snapshot == nil {
		status.addErrorf(name, "VolumeSnapshot doesn't exist")
		return
	}

	pvcRoleLabel := snapshot.GetAnnotations()[utils.PvcRoleLabelName]
	if len(pvcRoleLabel) == 0 {
		status.addWarningf(name, "Empty PVC role annotation")
	} else if pvcRoleLabel != expectedRole.GetRoleName() {
		status.addErrorf(
			name,
			"Expected role '%s', found '%s'",
			utils.PVCRoleValueData,
			pvcRoleLabel)
	}

	backupNameLabel := snapshot.GetLabels()[utils.BackupNameLabelName]
	if len(backupNameLabel) == 0 {
		status.addWarningf(
			name,
			"Empty backup name label",
		)
	}
}

// VerifyDataSourceCoherence verifies if th specified data source that we should
// use when creating a new cluster is coherent. We check for:
//
//   - role of the volume snapshot is coherent with the requested section
//     (being storage or walStorage)
//
//   - the specified snapshots all belong to the same cluster and backupName
func VerifyDataSourceCoherence(
	ctx context.Context,
	c client.Client,
	namespace string,
	source *apiv1.DataSource,
) (ValidationStatus, error) {
	var result ValidationStatus

	if source == nil {
		return result, nil
	}

	pgDataSnapshot, err := getVolumeShapshotOrNil(
		ctx,
		c,
		client.ObjectKey{Namespace: namespace, Name: source.Storage.Name})
	if err != nil {
		return result, err
	}
	result.validateVolumeSnapshot(source.Storage.Name, pgDataSnapshot, PgData{})

	var pgWalSnapshot *volumesnapshot.VolumeSnapshot
	if source.WalStorage != nil {
		pgWalSnapshot, err = getVolumeShapshotOrNil(
			ctx,
			c,
			client.ObjectKey{Namespace: namespace, Name: source.WalStorage.Name})
		if err != nil {
			return result, err
		}
		result.validateVolumeSnapshot(source.WalStorage.Name, pgWalSnapshot, PgWal{})
	}

	if pgDataSnapshot != nil && pgWalSnapshot != nil {
		pgDataBackupName := pgDataSnapshot.GetLabels()[utils.BackupNameLabelName]
		pgWalBackupName := pgWalSnapshot.GetLabels()[utils.BackupNameLabelName]

		if pgDataBackupName != pgWalBackupName {
			result.addErrorf(
				source.Storage.Name,
				"Non coherent backup names: '%s' and '%s'",
				pgDataBackupName,
				pgWalBackupName)
		}
	}

	return result, nil
}

// getVolumeShapshotOrNil gets a volume snapshot with a specified name.
// If the volume snapshot don't exist, returns nil
func getVolumeShapshotOrNil(
	ctx context.Context,
	c client.Client,
	name client.ObjectKey,
) (*volumesnapshot.VolumeSnapshot, error) {
	var result volumesnapshot.VolumeSnapshot
	if err := c.Get(ctx, name, &result); err != nil {
		if apierrs.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return &result, nil
}
