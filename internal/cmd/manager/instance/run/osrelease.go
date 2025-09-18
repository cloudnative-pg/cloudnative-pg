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

package run

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"github.com/acobaugh/osrelease"
	"github.com/cloudnative-pg/machinery/pkg/log"
)

// checkCurrentOSRelease inspects the host's OS release information
// and logs warnings if the distribution is unknown, unsupported, or deprecated.
// If the OS release file is missing, the check is skipped silently.
func checkCurrentOSRelease(ctx context.Context) {
	contextLogger := log.FromContext(ctx)

	data, err := osrelease.Read()
	if errors.Is(err, fs.ErrNotExist) {
		// Some Linux distributions do not include an os-release file.
		// Skipping the OS distribution check in such cases.
		return
	}
	if err != nil {
		contextLogger.Warning(
			"Failed to read or parse the os-release file; skipping distribution check",
			"err", err)
		return
	}

	version := data["VERSION"]
	if version == "" {
		contextLogger.Warning(
			"os-release file is missing the VERSION field; skipping distribution check",
			"data", data)
		return
	}

	entry, ok := defaultOSDB.Get(version)
	if !ok {
		contextLogger.Warning(
			"Encountered unknown OS distribution version; skipping check",
			"version", version)
		return
	}

	now := time.Now()
	switch {
	case !entry.IsSupported(now):
		contextLogger.Warning(
			"OS distribution is not supported",
			"entry", entry)

	case entry.IsDeprecated(now):
		contextLogger.Warning(
			"OS distribution is deprecated; consider upgrading",
			"entry", entry)

	default:
		contextLogger.Info(
			"OS distribution is supported",
			"entry", entry)
	}
}
