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

package postgres

import (
	"os/user"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// getCurrentUserOrDefaultToInsecureMapping retrieves the current system user's username.
// If the retrieval fails, it falls back to an insecure mapping using the root ("/") as the default username.
//
// Returns:
// - string: The current system user's username or the default insecure mapping if retrieval fails.
func getCurrentUserOrDefaultToInsecureMapping() string {
	currentUser, err := user.Current()
	if err != nil {
		log.Info("Unable to identify the current user. Falling back to insecure mapping.")
		return "/"
	}

	return currentUser.Username
}
