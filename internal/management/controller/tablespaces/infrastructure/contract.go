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

package infrastructure

import "context"

// Tablespace represents the tablespace information read from / written to the Database
type Tablespace struct {
	Name      string `json:"name"`
	Temporary bool   `json:"temporary"`
}

// TablespaceManager abstracts the functionality of reconciling with PostgreSQL tablespaces
type TablespaceManager interface {
	// List the tablespace in the database
	List(ctx context.Context) ([]Tablespace, error)
	// Update the tablespace in the database
	Update(ctx context.Context, tablespace Tablespace) error
	// Create the tablespace in the database
	Create(ctx context.Context, tablespace Tablespace) error
}
