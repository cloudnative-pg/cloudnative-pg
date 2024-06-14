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

	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// validateVolumeMetadata validates the label of a volume source,
// adding the result to the status
func (status *ValidationStatus) validateVolumeMetadata(
	name string,
	object *metav1.ObjectMeta,
	expectedMeta Meta,
) {
	if object == nil {
		status.addErrorf(name, "the volume doesn't exist")
		return
	}

	pvcRoleLabel, present := object.GetLabels()[utils.PvcRoleLabelName]
	if present {
		if pvcRoleLabel != expectedMeta.GetRoleName() {
			status.addErrorf(
				name,
				"Expected role '%s', found '%s'",
				utils.PVCRolePgData,
				pvcRoleLabel)
		}
		return
	}

	pvcRoleAnnotation := object.GetAnnotations()[utils.PvcRoleLabelName]
	if len(pvcRoleAnnotation) == 0 {
		status.addWarningf(name, "Empty PVC role annotation")
	} else if pvcRoleAnnotation != expectedMeta.GetRoleName() {
		status.addErrorf(
			name,
			"Expected role '%s', found '%s'",
			utils.PVCRolePgData,
			pvcRoleAnnotation)
	}

	backupNameLabel := object.GetLabels()[utils.BackupNameLabelName]
	if len(backupNameLabel) == 0 {
		status.addWarningf(
			name,
			"Empty backup name label",
		)
	}
}

// VerifyDataSourceCoherence verifies if the specified data source that we should
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

	pgData, err := GetSourceMetadataOrNil(
		ctx,
		c,
		namespace,
		source.Storage)
	if err != nil {
		return result, err
	}
	result.validateVolumeMetadata(source.Storage.Name, pgData, NewPgDataCalculator())

	var pgWal *metav1.ObjectMeta
	if source.WalStorage != nil {
		pgWal, err = GetSourceMetadataOrNil(
			ctx,
			c,
			namespace,
			*source.WalStorage)
		if err != nil {
			return result, err
		}
		result.validateVolumeMetadata(source.WalStorage.Name, pgWal, NewPgWalCalculator())
	}

	if pgData != nil && pgWal != nil {
		pgDataBackupName := pgData.GetLabels()[utils.BackupNameLabelName]
		pgWalBackupName := pgWal.GetLabels()[utils.BackupNameLabelName]

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

// GetSourceMetadataOrNil gets snapshot metadata from a specified source.
// If the source doesn't exist, returns nil
func GetSourceMetadataOrNil(
	ctx context.Context,
	c client.Client,
	namespace string,
	source corev1.TypedLocalObjectReference,
) (*metav1.ObjectMeta, error) {
	apiGroup := ""
	if source.APIGroup != nil {
		apiGroup = *source.APIGroup
	}

	switch {
	case apiGroup == "" && source.Kind == "":
		fallthrough
	case apiGroup == storagesnapshotv1.GroupName && source.Kind == "VolumeSnapshot":
		var result storagesnapshotv1.VolumeSnapshot
		if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: source.Name}, &result); err != nil {
			if apierrs.IsNotFound(err) {
				return nil, nil
			}

			return nil, err
		}
		return &result.ObjectMeta, nil
	case apiGroup == corev1.GroupName && source.Kind == "PersistentVolumeClaim":
		var result corev1.PersistentVolumeClaim
		if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: source.Name}, &result); err != nil {
			if apierrs.IsNotFound(err) {
				return nil, nil
			}

			return nil, err
		}
		return &result.ObjectMeta, nil
	}

	return nil, fmt.Errorf("only VolumeSnapshots and PersistentVolumeClaims are supported")
}
