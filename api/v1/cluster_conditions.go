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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// A Condition that can be used to communicate the Backup progress
var (
	// BackupSucceededCondition is added to a backup
	// when it was completed correctly
	BackupSucceededCondition = metav1.Condition{
		Type:    string(ConditionBackup),
		Status:  metav1.ConditionTrue,
		Reason:  string(ConditionReasonLastBackupSucceeded),
		Message: "Backup was successful",
	}

	// BackupStartingCondition is added to a backup
	// when it started
	BackupStartingCondition = metav1.Condition{
		Type:    string(ConditionBackup),
		Status:  metav1.ConditionFalse,
		Reason:  string(ConditionBackupStarted),
		Message: "New Backup starting up",
	}

	// BuildClusterBackupFailedCondition builds
	// ConditionReasonLastBackupFailed condition
	BuildClusterBackupFailedCondition = func(err error) metav1.Condition {
		return metav1.Condition{
			Type:    string(ConditionBackup),
			Status:  metav1.ConditionFalse,
			Reason:  string(ConditionReasonLastBackupFailed),
			Message: err.Error(),
		}
	}
	// OutsideWatchScopeCondition is used to indicate that the Cluster resource is
	// in a namespace not watched by the operator.
	OutsideWatchScopeCondition = metav1.Condition{
		Type:    string(ConditionReconciled),
		Status:  metav1.ConditionFalse,
		Reason:  string(ConditionReasonOutsideWatchScope),
		Message: "This Cluster resource is in a namespace not watched by the operator",
	}
)
