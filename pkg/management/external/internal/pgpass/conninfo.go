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

package pgpass

import "fmt"

// ConnectionInfo contains the information identifying
// a PostgreSQL server whom credentials need to be included
// in a pgpass file
type ConnectionInfo struct {
	host     string
	port     string
	dbname   string
	user     string
	password string
}

// NewConnectionInfo builds a new NewConnectionInfo from a set of
// connection parameters and the corresponding password
func NewConnectionInfo(
	connectionParameters map[string]string,
	password string,
) (result ConnectionInfo) {
	getWithDefault := func(connectionParameters map[string]string, name string) string {
		if value, ok := connectionParameters[name]; ok {
			return value
		}

		return "*"
	}

	result.host = getWithDefault(connectionParameters, "host")
	result.port = getWithDefault(connectionParameters, "port")
	result.user = getWithDefault(connectionParameters, "user")
	result.password = password

	// dbname is fixed to "*"" as we do not want to discriminate
	// based on the target database, just the pair host:port
	result.dbname = "*"

	return result
}

// BuildLine builds a pgPass configuration file line
func (info ConnectionInfo) BuildLine() string {
	return fmt.Sprintf(
		"%v:%v:%v:%v:%v",
		info.host,
		info.port,
		info.dbname,
		info.user,
		info.password,
	)
}
