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

package repository

import "fmt"

// ErrUnknownPlugin is raised when requesting a connection to
// a plugin that is not known
type ErrUnknownPlugin struct {
	Name string
}

// Error implements the error interface
func (e *ErrUnknownPlugin) Error() string {
	return fmt.Sprintf("Unknown plugin: %s", e.Name)
}

// ErrPluginAlreadyRegistered is raised when a plugin
// is being registered but its configuration is
// already attached to the pool
type ErrPluginAlreadyRegistered struct {
	Name string
}

// Error implements the error interface
func (e *ErrPluginAlreadyRegistered) Error() string {
	return fmt.Sprintf("Plugin already registered: %s", e.Name)
}
