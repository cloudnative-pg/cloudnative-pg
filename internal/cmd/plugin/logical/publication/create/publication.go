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

package create

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// PublicationCmdBuilder represent a command to create a publication
type PublicationCmdBuilder struct {
	// The name of the publication to be created
	PublicationName string

	// The target to be publication
	PublicationTarget PublicationTarget

	// The optional publication parameters
	PublicationParameters string
}

// ToSQL create the SQL Statement to create the publication
func (cmd PublicationCmdBuilder) ToSQL() string {
	result := fmt.Sprintf(
		"CREATE PUBLICATION %s %s",
		pgx.Identifier{cmd.PublicationName}.Sanitize(),
		cmd.PublicationTarget.ToPublicationTargetSQL(),
	)

	if len(cmd.PublicationParameters) > 0 {
		result = fmt.Sprintf("%s WITH (%s)", result, cmd.PublicationParameters)
	}

	return result
}

// PublicationTarget represent the publication target
type PublicationTarget interface {
	// Create the SQL statement to publication the tables
	ToPublicationTargetSQL() string
}

// PublicationTargetALLTables will publish all tables
type PublicationTargetALLTables struct{}

// ToPublicationTargetSQL implements the PublicationTarget interface
func (PublicationTargetALLTables) ToPublicationTargetSQL() string {
	return "FOR ALL TABLES"
}

// PublicationTargetPublicationObjects publishes multiple publication objects
type PublicationTargetPublicationObjects struct {
	PublicationObjects []PublicationObject
}

// ToPublicationTargetSQL implements the PublicationObject interface
func (objs *PublicationTargetPublicationObjects) ToPublicationTargetSQL() string {
	result := ""
	for _, object := range objs.PublicationObjects {
		if len(result) > 0 {
			result += ", "
		}
		result += object.ToPublicationObjectSQL()
	}

	if len(result) > 0 {
		result = fmt.Sprintf("FOR %s", result)
	}
	return result
}

// PublicationObject represent an object to publish
type PublicationObject interface {
	// ToPublicationObjectSQL creates the SQL statement to publish this object
	ToPublicationObjectSQL() string
}

// PublicationObjectSchema will publish all the tables in a certain schema
type PublicationObjectSchema struct {
	// The schema to publish
	SchemaName string
}

// ToPublicationObjectSQL implements the PublicationObject interface
func (obj PublicationObjectSchema) ToPublicationObjectSQL() string {
	return fmt.Sprintf("TABLES IN SCHEMA %s", pgx.Identifier{obj.SchemaName}.Sanitize())
}

// PublicationObjectTableExpression will publish the passed table expression
type PublicationObjectTableExpression struct {
	// The table expression to publish
	TableExpressions []string
}

// ToPublicationObjectSQL implements the PublicationObject interface
func (obj PublicationObjectTableExpression) ToPublicationObjectSQL() string {
	return fmt.Sprintf("TABLE %s", strings.Join(obj.TableExpressions, ", "))
}
