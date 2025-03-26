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

	"github.com/jackc/pgx/v5"
	"github.com/lib/pq"
)

// SubscriptionCmdBuilder represent a command to create a subscription
type SubscriptionCmdBuilder struct {
	// The name of the publication to be created
	SubscriptionName string

	// The connection to the source database
	ConnectionString string

	// The name of the publication to attach to
	PublicationName string

	// Custom subscription parameters
	Parameters string
}

// ToSQL create the SQL Statement to create the publication
func (cmd SubscriptionCmdBuilder) ToSQL() string {
	result := fmt.Sprintf(
		"CREATE SUBSCRIPTION %s CONNECTION %s PUBLICATION %s",
		pgx.Identifier{cmd.SubscriptionName}.Sanitize(),
		pq.QuoteLiteral(cmd.ConnectionString),
		cmd.PublicationName,
	)

	if len(cmd.Parameters) > 0 {
		result = fmt.Sprintf("%s WITH (%s)", result, cmd.Parameters)
	}

	return result
}
