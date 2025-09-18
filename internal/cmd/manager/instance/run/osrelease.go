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

func checkCurrentOSRelease(ctx context.Context) {
	contextLogger := log.FromContext(ctx)

	data, err := osrelease.Read()
	if errors.Is(err, fs.ErrNotExist) {
		// There is no need to complain if we don't
		// find the os-release file. Some distributions
		// don't have it.
		return
	}
	if err != nil {
		contextLogger.Warning(
			"Can't parse the os-release file, distribution check skipped",
			"err", err)
		return
	}

	version := data["VERSION"]
	if version == "" {
		contextLogger.Warning(
			"The os-release file doesn't contain the VERSION field, distribution check skipped",
			"data", data)
		return
	}

	entry, ok := defaultOSDB.Get(version)
	if !ok {
		contextLogger.Warning(
			"Unknown distribution version, check skipped",
			"version", version)
		return
	}

	now := time.Now()
	switch {
	case !entry.IsSupported(now):
		contextLogger.Warning(
			"Base distribution is not supported",
			"entry", entry)

	case entry.IsDeprecated(now):
		contextLogger.Warning(
			"Base distribution is deprecated",
			"entry", entry)

	default:
		contextLogger.Warning(
			"Base distribution is supported",
			"entry", entry)
	}
}
