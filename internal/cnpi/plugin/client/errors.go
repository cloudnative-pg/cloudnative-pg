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

import (
	"errors"
	"fmt"
	"time"
)

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
	if IsRequeueError(err) {
		return err
	}

	return &pluginError{innerErr: err}
}

// RequeueError is returned when a plugin requests the reconciliation to be
// requeued without treating it as an error condition. This is useful when
// a plugin is waiting for a dependency (e.g., a custom resource) to be created.
type RequeueError struct {
	// After specifies how long to wait before requeuing.
	// If zero, the operator's default requeue interval is used.
	After time.Duration
}

func (e *RequeueError) Error() string {
	if e.After > 0 {
		return fmt.Sprintf("plugin requested requeue after %s", e.After)
	}
	return "plugin requested requeue"
}

// IsRequeueError checks if the provided error is a RequeueError.
func IsRequeueError(err error) bool {
	var requeueErr *RequeueError
	return errors.As(err, &requeueErr)
}

// GetRequeueAfter extracts the requeue duration from a RequeueError.
// Returns zero duration if the error is not a RequeueError.
func GetRequeueAfter(err error) time.Duration {
	var requeueErr *RequeueError
	if errors.As(err, &requeueErr) {
		return requeueErr.After
	}
	return 0
}
