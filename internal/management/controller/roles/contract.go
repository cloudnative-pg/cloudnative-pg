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

package roles

import (
	"context"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// RoleManager abstracts the functionality of reconciling with PostgreSQL roles
type RoleManager interface {
	// List the roles in the database
	List(ctx context.Context, config *apiv1.ManagedConfiguration) ([]apiv1.RoleConfiguration, error)
	// Update the role in the database
	Update(ctx context.Context, role apiv1.RoleConfiguration) error
	// Create the role in the database
	Create(ctx context.Context, role apiv1.RoleConfiguration) error
	// Delete the role in the database
	Delete(ctx context.Context, role apiv1.RoleConfiguration) error
}
