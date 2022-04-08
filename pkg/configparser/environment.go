/*
Copyright 2019-2022 The CloudNativePG Contributors

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

package configparser

import (
	"os"
)

// EnvironmentSource is an interface to identify an environment values source.
type EnvironmentSource interface {
	Getenv(key string) string
}

// OsEnvironment is an EnvironmentSource that fetch data from the OS environment.
type OsEnvironment struct{}

// Getenv retrieves the value of the environment variable named by the key.
func (OsEnvironment) Getenv(key string) string {
	return os.Getenv(key)
}
