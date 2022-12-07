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

package resources

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PatchType is the patch type to be used for patch requests
type PatchType string

const (
	// PatchTypeStrategicMerge means use the strategic merge patch type, passing
	// just the diff to the API server
	PatchTypeStrategicMerge PatchType = "StrategicMerge"

	// PatchTypeMerge means use the merge strategy
	PatchTypeMerge PatchType = "Merge"
)

// BuildPatch creates a client.patch for the passed object
func (p PatchType) BuildPatch(current client.Object) client.Patch {
	switch p {
	case PatchTypeMerge:
		return client.Merge
	case PatchTypeStrategicMerge:
		return client.StrategicMergeFrom(current)
	default:
		// this is a programmatic error and should cause a panic
		panic("unknown patch type in BuildPatch")
	}
}
