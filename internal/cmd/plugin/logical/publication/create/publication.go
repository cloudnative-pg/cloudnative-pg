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

package create

import (
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PublicationCmdBuilder represent a command to create a publication
type PublicationCmdBuilder struct {
	// The name of the publication to be created
	PublicationName string

	// The target to be publication
	PublicationTarget PublicationTarget
}

// ToSQL create the SQL Statement to create the publication
func (cmd PublicationCmdBuilder) ToSQL() string {
	return fmt.Sprintf(
		"CREATE PUBLICATION %s %s",
		pgx.Identifier{cmd.PublicationName}.Sanitize(),
		cmd.PublicationTarget.ToSQL(),
	)
}

// PublicationTarget represent the publication target
type PublicationTarget interface {
	// Create the SQL statement to publication the tables
	ToSQL() string
}

// PublicationTargetALLTables will publicate all tables
type PublicationTargetALLTables struct{}

// ToSQL implements the PublicationTarget interface
func (PublicationTargetALLTables) ToSQL() string {
	return "FOR ALL TABLES"
}

// PublicationTargetPublicationObjects publicates multiple publication objects
type PublicationTargetPublicationObjects struct {
	PublicationObjects []PublicationObject
}

// ToSQL implements the PublicationObject interface
func (objs *PublicationTargetPublicationObjects) ToSQL() string {
	var result string
	for _, object := range objs.PublicationObjects {
		if len(result) > 0 {
			result += ", "
		}
		result += fmt.Sprintf("FOR %s", object.ToSQL())
	}
	return result
}

// PublicationObject represent an object to publicate
type PublicationObject interface {
	// Create the SQL statement to publicate this object
	ToSQL() string
}

// PublicationObjectSchema will publicate all the tables in a certain schema
type PublicationObjectSchema struct {
	// The schema to publicate
	SchemaName string
}

// ToSQL implements the PublicationObject interface
func (obj PublicationObjectSchema) ToSQL() string {
	return fmt.Sprintf("TABLES IN SCHEMA %s", pgx.Identifier{obj.SchemaName}.Sanitize())
}

// PublicationObjectTableExpression will publicate the passed table expression
type PublicationObjectTableExpression struct {
	// The table expression to publicate
	TableExpression string
}

// ToSQL implements the PublicationObject interface
func (obj PublicationObjectTableExpression) ToSQL() string {
	return obj.TableExpression
}
