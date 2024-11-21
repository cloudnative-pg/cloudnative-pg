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

package plugin

import (
	"fmt"

	"github.com/cloudnative-pg/cnpg-i/pkg/lifecycle"
)

// The OperationVerb corresponds to the Kubernetes API method
type OperationVerb string

// A Kubernetes operation verb
const (
	OperationVerbPatch  OperationVerb = "PATCH"
	OperationVerbUpdate OperationVerb = "UPDATE"
	OperationVerbCreate OperationVerb = "CREATE"
	OperationVerbDelete OperationVerb = "DELETE"
)

// ToOperationType_Type converts an OperationVerb into a lifecycle.OperationType_Type
// nolint: revive,stylecheck
func (o OperationVerb) ToOperationType_Type() (lifecycle.OperatorOperationType_Type, error) {
	switch o {
	case OperationVerbPatch:
		return lifecycle.OperatorOperationType_TYPE_PATCH, nil
	case OperationVerbDelete:
		return lifecycle.OperatorOperationType_TYPE_DELETE, nil
	case OperationVerbCreate:
		return lifecycle.OperatorOperationType_TYPE_CREATE, nil
	case OperationVerbUpdate:
		return lifecycle.OperatorOperationType_TYPE_UPDATE, nil
	}

	return lifecycle.OperatorOperationType_Type(0), fmt.Errorf("unknown operation type: '%s'", o)
}
