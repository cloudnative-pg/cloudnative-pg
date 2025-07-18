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

package client

import "errors"

var (
	// ErrPluginNotLoaded is raised when the plugin that should manage the backup
	// have not been loaded inside the cluster
	ErrPluginNotLoaded = newPluginError("plugin not loaded")

	// ErrPluginNotSupportBackup is raised when the plugin that should manage the backup
	// doesn't support the Backup service
	ErrPluginNotSupportBackup = newPluginError("plugin does not support Backup service")

	// ErrPluginNotSupportBackupEndpoint is raised when the plugin that should manage the backup
	// doesn't support the Backup RPC endpoint
	ErrPluginNotSupportBackupEndpoint = newPluginError("plugin does not support the Backup RPC call")
)

type pluginError struct {
	innerErr error
}

func (e *pluginError) Error() string {
	return e.innerErr.Error()
}

func (e *pluginError) Unwrap() error {
	return e.innerErr
}

func newPluginError(msg string) error {
	return &pluginError{innerErr: errors.New(msg)}
}

// ContainsPluginError checks if the provided error chain contains a plugin error.
func ContainsPluginError(err error) bool {
	if err == nil {
		return false
	}

	var pluginErr *pluginError
	return errors.As(err, &pluginErr)
}

func wrapAsPluginErrorIfNeeded(err error) error {
	if err == nil {
		return nil
	}
	if ContainsPluginError(err) {
		return err
	}

	return &pluginError{innerErr: err}
}
