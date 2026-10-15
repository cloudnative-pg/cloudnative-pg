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

package config

import (
	"maps"
	"sync"
)

// templateVariables holds values computed while the tests run (snapshot
// names, backup names, ...) that are substituted for `${NAME}` placeholders
// when rendering .template fixtures. They are runtime state, not
// configuration, so they don't belong to the configuration file.
var (
	templateVariablesMu sync.RWMutex
	templateVariables   = map[string]string{}
)

// SetTemplateVariable registers a runtime value to be substituted for the
// `${name}` placeholder when rendering .template fixtures
func SetTemplateVariable(name, value string) {
	templateVariablesMu.Lock()
	defer templateVariablesMu.Unlock()
	templateVariables[name] = value
}

// UnsetTemplateVariable removes a previously registered template variable
func UnsetTemplateVariable(name string) {
	templateVariablesMu.Lock()
	defer templateVariablesMu.Unlock()
	delete(templateVariables, name)
}

// TemplateVariables returns a copy of the registered template variables
func TemplateVariables() map[string]string {
	templateVariablesMu.RLock()
	defer templateVariablesMu.RUnlock()
	return maps.Clone(templateVariables)
}
