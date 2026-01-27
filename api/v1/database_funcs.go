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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// SetAsFailed sets the database as failed with the given error
func (db *Database) SetAsFailed(err error) {
	db.Status.Applied = ptr.To(false)
	db.Status.Message = err.Error()
}

// SetAsUnknown sets the database as unknown with the given error
func (db *Database) SetAsUnknown(err error) {
	db.Status.Applied = nil
	db.Status.Message = err.Error()
}

// SetAsReady sets the database as working correctly
func (db *Database) SetAsReady() {
	db.Status.Applied = ptr.To(true)
	db.Status.Message = ""
	db.Status.ObservedGeneration = db.Generation
}

// GetStatusMessage returns the status message of the database
func (db *Database) GetStatusMessage() string {
	return db.Status.Message
}

// GetClusterRef returns the cluster reference of the database
func (db *Database) GetClusterRef() corev1.LocalObjectReference {
	return db.Spec.ClusterRef
}

// GetManagedObjectName returns the name of the managed database object
func (db *Database) GetManagedObjectName() string {
	return db.Spec.Name
}

// GetName returns the database object name
func (db *Database) GetName() string {
	return db.Name
}

// HasReconciliations returns true if the database object has been reconciled at least once
func (db *Database) HasReconciliations() bool {
	return db.Status.ObservedGeneration > 0
}

// SetStatusObservedGeneration sets the observed generation of the database
func (db *Database) SetStatusObservedGeneration(obsGeneration int64) {
	db.Status.ObservedGeneration = obsGeneration
}

// MustHaveManagedResourceExclusivity detects conflicting databases
func (dbList *DatabaseList) MustHaveManagedResourceExclusivity(reference *Database) error {
	pointers := toSliceWithPointers(dbList.Items)
	return ensureManagedResourceExclusivity(reference, pointers)
}

// GetEnsure gets the ensure status of the resource
func (dbObject DatabaseObjectSpec) GetEnsure() EnsureOption {
	return dbObject.Ensure
}

// GetName gets the name of the resource
func (dbObject DatabaseObjectSpec) GetName() string {
	return dbObject.Name
}

// SetAdmissionError sets the admission error status on the Database resource
func (db *Database) SetAdmissionError(msg string) {
	db.Status.Message = msg
	db.Status.Applied = ptr.To(len(msg) == 0)
}
