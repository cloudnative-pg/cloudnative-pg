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

// GetStatusMessage returns the applied status of the database
func (db *Database) GetStatusMessage() string {
	return db.Status.Message
}

// GetClusterRef returns the cluster reference of the database
func (db *Database) GetClusterRef() corev1.LocalObjectReference {
	return db.Spec.ClusterRef
}
